package main

import (
	"archive/tar"
	"bytes"
	"context"
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
	"github.com/jferrl/go-githubauth"
)

type Config struct {
	cli               *client.Client
	ctx               context.Context
	imageName         string
	githubAccessToken string
	tokenExpire       *time.Time
	ownerName         string
	repoName          string
	limit             int
}

func main() {
	githubAccessToken, expire, err := getGitHubToken()
	ownerName := os.Getenv("GITHUB_REPOSITORY_OWNER")
	repoName := os.Getenv("GITHUB_REPOSITORY_NAME")
	runnerLimit := os.Getenv("RUNNER_LIMIT")
	if err != nil {
		log.Println(err)
		return
	}
	if ownerName == "" {
		log.Println("GITHUB_REPOSITORY_OWNER is not registered")
		return
	}
	if repoName == "" {
		log.Println("GITHUB_REPOSITORY_NAME is not registered")
		return
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Println("Error creating Docker client: ", err)
	}

	var limit int
	limit, err = strconv.Atoi(runnerLimit)
	if err != nil {
		limit = 2
	}

	config := &Config{
		cli:               cli,
		ctx:               context.Background(),
		imageName:         "local-runner:latest",
		githubAccessToken: githubAccessToken,
		tokenExpire:       expire,
		ownerName:         ownerName,
		repoName:          repoName,
		limit:             limit,
	}

	build, e := config.haveToBuild()
	if e != nil {
		log.Println("Can not find image: ", err)
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

	eventsChan, errorsChan := cli.Events(config.ctx, events.ListOptions{})

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
			containers, _ := config.cli.ContainerList(config.ctx, container.ListOptions{})
			for _, v := range containers {
				if v.Image == config.imageName {
					res, err := config.cli.ContainerExecCreate(config.ctx, v.ID, container.ExecOptions{
						Cmd: []string{"/bin/bash", "-c", "export GITHUB_ACCESS_TOKEN=" + config.githubAccessToken + " && /actions-runner/stop.sh"},
					})
					if err != nil {
						log.Println("Can not delete container ", err)
						continue
					}
					_, err = cli.ContainerExecAttach(context.Background(), res.ID, container.ExecStartOptions{})
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

func getGitHubToken() (string, *time.Time, error) {
	githubAccessToken := os.Getenv("GITHUB_ACCESS_TOKEN")
	if githubAccessToken != "" {
		return githubAccessToken, nil, nil
	}
	keyPath := os.Getenv("KEY_PATH")
	appId, err := strconv.Atoi(os.Getenv("GITHUB_APP_ID"))
	if err != nil {
		return "", nil, fmt.Errorf("GITHUB_APP_ID is invalid %s", err)
	}
	installationId, err := strconv.Atoi(os.Getenv("GITHUB_APP_INSTALL_ID"))
	if err != nil {
		return "", nil, fmt.Errorf("GITHUB_APP_INSTALL_ID is invalid %s", err)
	}

	if keyPath != "" && appId != 0 && installationId != 0 {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return "", nil, fmt.Errorf("Can not get app private key %s", err)
		}
		appTokenSource, err := githubauth.NewApplicationTokenSource(int64(appId), key)
		if err != nil {
			return "", nil, fmt.Errorf("Can not get access_token %s", err)
		}
		installationTokenSource := githubauth.NewInstallationTokenSource(int64(installationId), appTokenSource)
		token, err := installationTokenSource.Token()
		if err != nil {
			return "", nil, fmt.Errorf("Can not get token %s", err)
		}
		return token.AccessToken, &token.Expiry, nil
	}
	return "", nil, fmt.Errorf("Can not get token")
}

// コンテナ終了時のコールバック処理
func (config *Config) handleContainer() *error {
	// 必要な処理をここに実装
	containers, _ := config.cli.ContainerList(config.ctx, container.ListOptions{})
	var count = 0
	for _, v := range containers {
		if v.Image == config.imageName {
			count++
		}
	}
	if config.limit > count {
		// コンテナの設定
		config.refreshToken()
		containerConfig := &container.Config{
			Image: config.imageName,
			Env:   []string{"GITHUB_API_DOMAIN=api.github.com", "GITHUB_DOMAIN=github.com", "RUNNER_ALLOW_RUNASROOT=abc", "GITHUB_ACCESS_TOKEN=" + config.githubAccessToken, "GITHUB_REPOSITORY_OWNER=" + config.ownerName, "GITHUB_REPOSITORY_NAME=" + config.repoName},
		}

		// ホスト設定（自動削除など）
		hostConfig := &container.HostConfig{
			AutoRemove: true, // コンテナ終了後に自動で削除
		}

		j := config.limit - count
		for i := 0; i < j; i++ {
			seed := time.Now().UnixNano()
			rand.New(rand.NewSource(seed))
			val := rand.Intn(100000)

			// コンテナの作成
			resp, err := config.cli.ContainerCreate(
				config.ctx,
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
			if err := config.cli.ContainerStart(config.ctx, resp.ID, container.StartOptions{}); err != nil {
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
		Tags:       []string{config.imageName},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}

	buildContext, err := createBuildContext("./")
	if err != nil {
		log.Println("Error creating build context: ", err)
		return err
	}

	res, er := config.cli.ImageBuild(config.ctx, buildContext, options)
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
	list, e := config.cli.ImageList(config.ctx, image.ListOptions{})
	if e != nil {
		log.Println(e)
		return true, fmt.Errorf("does not find %s can not get image list", config.imageName)
	}
	for _, v := range list {
		for _, t := range v.RepoTags {
			if t == config.imageName {
				return false, nil
			}
		}
	}
	return true, nil
}

func (config *Config) refreshToken() error {
	if config.tokenExpire != nil && config.tokenExpire.Before(time.Now()) {
		newToken, newExpire, e := getGitHubToken()
		if e != nil {
			return fmt.Errorf("Can not get GitHub token %s", e)
		}
		config.githubAccessToken = newToken
		config.tokenExpire = newExpire
	}
	return nil
}
