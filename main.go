package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/google/go-github/v64/github"
	"github.com/jferrl/go-githubauth"
)

type GitHubAuth struct {
	IsApp       bool   `json:"is_app"`
	AccessToken string `json:"access_token"`
	App         struct {
		Id             int64  `json:"id"`
		InstallationId int64  `json:"installation_id"`
		KeyPath        string `json:"key_path"`
	}
}

type Repository struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type Env struct {
	Github struct {
		OrgName string `json:"org_name"`
		Repository Repository `json:"repository"`
		Auth       GitHubAuth `json:"auth"`
	} `json:"github"`
	RunnerLimit int `json:"runner_limit"`
}

type Config struct {
	Cli               *client.Client
	Ctx               context.Context
	ImageName         string
	GithubAuth        GitHubAuth
	GithubAccessToken string
	TokenExpire       *time.Time
	Repository        *Repository
	OrgName *string
	Limit             int
}

func main() {
	config, err := makeConfig()
	if err != nil {
		log.Println("Invalid enviroment variables: ", err)
		return
	}
	build, e := config.haveToBuild()
	if e != nil {
		log.Println("Can not find image: ", e)
		return
	}

	if build {
		er := config.buildRunnerImage()
		if er != nil {
			log.Println("Can not build: ", er)
			return
		}
	}
	ee := config.handleContainer()
	if ee != nil {
		log.Println(ee)
		return
	}

	// プログラム終了を制御するチャンネル
	done := make(chan bool)

	// シグナルをキャッチするチャンネルを作成
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 別のゴルーチンでシグナルを監視
	go func() {
		<-sigChan    // シグナルが来るまで待機
		done <- true // シグナルを受け取ったらdoneチャンネルに通知
	}()

	eventsChan, errorsChan := config.Cli.Events(config.Ctx, events.ListOptions{})

	// イベントストリームの監視
	for {
		select {
		case event := <-eventsChan:
			if event.Type == events.ContainerEventType && event.Action == "die" {
				log.Println("Container", event.Actor.ID, " has exited", event.Actor.Attributes)
				ee := config.handleContainer()
				if ee != nil {
					log.Println(ee)
					return
				}
			}
		case err := <-errorsChan:
			log.Println("Error while listening to Docker events: ", err)
		case <-done:
			config.refreshToken()
			containers, _ := config.Cli.ContainerList(config.Ctx, container.ListOptions{})
			for _, v := range containers {
				if v.Image == config.ImageName {
					res, err := config.Cli.ContainerExecCreate(config.Ctx, v.ID, container.ExecOptions{
						Cmd: []string{"/bin/bash", "-c", "export GITHUB_ACCESS_TOKEN=" + config.GithubAccessToken + " && /actions-runner/stop.sh"},
					})
					if err != nil {
						log.Println("Can not delete container ", err)
						continue
					}
					_, err = config.Cli.ContainerExecAttach(context.Background(), res.ID, container.ExecStartOptions{})
					if err != nil {
						log.Println("Can not delete container ", err)
						continue
					}
					log.Println("Delete container id: ", v.ID)
				}
			}
			return
		}
	}
}

func makeConfig() (*Config, error) {
	bytes, err := os.ReadFile("config.json")
	if err != nil {
		return nil, fmt.Errorf("Config file (config.json) is not present.")
	}
	var env Env
	err = json.Unmarshal(bytes, &env)
	if err != nil {
		return nil, fmt.Errorf("Config file (config.json) is not invalid.")
	}
	if env.Github.Repository.Name == "" {
		return nil, fmt.Errorf("github.repository.name is not registered in config.json")
	}
	if env.Github.Repository.Owner == "" {
		return nil, fmt.Errorf("github.repository.owner is not registered in config.json")
	}

	var token string
	var expiresAt *time.Time
	if !env.Github.Auth.IsApp {
		if env.Github.Auth.AccessToken == "" {
			return nil, fmt.Errorf("github.auth.access_token is not registered in config.json")
		}
		token = env.Github.Auth.AccessToken
		expiresAt = nil
	} else {
		if env.Github.Auth.App.KeyPath == "" {
			return nil, fmt.Errorf("github.auth.app.key_path is not registered in config.json")
		}
		if env.Github.Auth.App.Id == 0 {
			return nil, fmt.Errorf("github.auth.app.id is not registered in config.json")
		}
		if env.Github.Auth.App.InstallationId == 0 {
			return nil, fmt.Errorf("github.auth.app.installation_id is not registered in config.json")
		}
		token, expiresAt, err = env.Github.Auth.getGitHubToken(env.Github.Repository.Name)
		if err != nil {
			return nil, fmt.Errorf("Can not get GitHub Token %s", err)
		}
	}
	var orgName *string
	var repository *Repository
	if env.Github.OrgName != "" {
		repository = nil
		orgName = &env.Github.OrgName
	} else {
		orgName = nil
				if env.Github.Repository.Owner == "" {
			return nil, fmt.Errorf("github.repository.onwer is not registered in config.json")
		}
						if env.Github.Repository.Name == "" {
			return nil, fmt.Errorf("github.repository.Name is not registered in config.json")
		}
		repository = &env.Github.Repository
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Error creating Docker client: %s", err)
	}

	var limit = env.RunnerLimit
	if limit == 0 {
		limit = 2
	}

	config := &Config{
		Cli:               cli,
		Ctx:               context.Background(),
		ImageName:         "local-runner:latest",
		GithubAccessToken: token,
		TokenExpire:       expiresAt,
		OrgName: orgName,
		Repository:        repository,
		Limit:             limit,
	}

	return config, nil
}

func (auth *GitHubAuth) getGitHubToken(repo string) (string, *time.Time, error) {
	if !auth.IsApp {
		return auth.AccessToken, nil, nil
	}

	key, err := os.ReadFile(auth.App.KeyPath)
	if err != nil {
		return "", nil, fmt.Errorf("Can not get app private key %s", err)
	}
	appTokenSource, err := githubauth.NewApplicationTokenSource(auth.App.Id, key)
	if err != nil {
		return "", nil, fmt.Errorf("Can not get access_token %s", err)
	}
	installationTokenSource := githubauth.NewInstallationTokenSource(auth.App.InstallationId, appTokenSource, githubauth.WithInstallationTokenOptions(&github.InstallationTokenOptions{
		Repositories: []string{repo},
	}))
	token, err := installationTokenSource.Token()
	if err != nil {
		return "", nil, fmt.Errorf("Can not get token %s", err)
	}
	return token.AccessToken, &token.Expiry, nil
}

// コンテナ終了時のコールバック処理
func (config *Config) handleContainer() *error {
	// 必要な処理をここに実装
	containers, _ := config.Cli.ContainerList(config.Ctx, container.ListOptions{})
	var count = 0
	for _, v := range containers {
		if v.Image == config.ImageName {
			count++
		}
	}
	if config.Limit > count {
		// コンテナの設定
		config.refreshToken()
		var env []string
		if config.OrgName != nil {
			env = []string{"GITHUB_API_DOMAIN=api.github.com", "GITHUB_DOMAIN=github.com", "RUNNER_ALLOW_RUNASROOT=abc", "GITHUB_ACCESS_TOKEN=" + config.GithubAccessToken, "GITHUB_REPOSITORY_OWNER=" + *config.OrgName}
		} else {
			env = []string{"GITHUB_API_DOMAIN=api.github.com", "GITHUB_DOMAIN=github.com", "RUNNER_ALLOW_RUNASROOT=abc", "GITHUB_ACCESS_TOKEN=" + config.GithubAccessToken, "GITHUB_REPOSITORY_OWNER=" + config.Repository.Owner, "GITHUB_REPOSITORY_NAME=" + config.Repository.Name}
		}
		containerConfig := &container.Config{
			Image: config.ImageName,
			Env:   env,
		}

		// ホスト設定（自動削除など）
		hostConfig := &container.HostConfig{
			AutoRemove: true, // コンテナ終了後に自動で削除
		}

		j := config.Limit - count
		for i := 0; i < j; i++ {
			seed := time.Now().UnixNano()
			rand.New(rand.NewSource(seed))
			val := rand.Intn(100000)

			// コンテナの作成
			resp, err := config.Cli.ContainerCreate(
				config.Ctx,
				containerConfig,
				hostConfig,
				nil,
				nil,
				"local-runner-"+strconv.Itoa(val),
			)

			if err != nil {
				log.Println("Error creating container: ", err)
				continue
			}

			// コンテナのIDを表示
			log.Println("Container created with ID: ", resp.ID)

			// コンテナを起動
			if err := config.Cli.ContainerStart(config.Ctx, resp.ID, container.StartOptions{}); err != nil {
				log.Println("Error starting container: ", err)
				continue
			}
		}
	}
	return nil
}

func (config *Config) buildRunnerImage() error {
	// イメージビルドオプションの設定
	options := types.ImageBuildOptions{
		Tags:       []string{config.ImageName},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}

	buildContext, err := createBuildContext("./")
	if err != nil {
		log.Println("Error creating build context: ", err)
		return err
	}

	res, er := config.Cli.ImageBuild(config.Ctx, buildContext, options)
	if er != nil {
		log.Println(er)
		return er
	}
	defer res.Body.Close()

	// ビルドの出力を表示
	_, err = io.Copy(os.Stdout, res.Body)
	if err != nil {
		log.Println("Error reading build output: ", err)
	}

	log.Println("Docker image built successfully!")
	return nil
}

func createBuildContext(dir string) (io.ReadCloser, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// 指定されたディレクトリ内の全ファイルをtarアーカイブに追加
	err := filepath.Walk(dir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// ディレクトリの場合はスキップ
		if fi.IsDir() {
			return nil
		}

		// ファイルをtarアーカイブに追加
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		header.Name = file // ファイルパスを保持

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}

		if _, err := tw.Write(data); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to tar directory: %w", err)
	}

	// tarアーカイブをクローズ
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	// tarアーカイブをio.Readerとして返す
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func (config *Config) haveToBuild() (bool, error) {
	list, e := config.Cli.ImageList(config.Ctx, image.ListOptions{})
	if e != nil {
		log.Println(e)
		return true, fmt.Errorf("does not find %s can not get image list", config.ImageName)
	}
	for _, v := range list {
		for _, t := range v.RepoTags {
			if t == config.ImageName {
				return false, nil
			}
		}
	}
	return true, nil
}

func (config *Config) refreshToken() error {
	if config.TokenExpire != nil && config.TokenExpire.Before(time.Now()) {
		newToken, newExpire, e := config.GithubAuth.getGitHubToken(config.Repository.Name)
		if e != nil {
			return fmt.Errorf("Can not get GitHub token %s", e)
		}
		config.GithubAccessToken = newToken
		config.TokenExpire = newExpire
	}
	return nil
}
