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
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type Runner struct {
	ApiDomain  string `json:"api_domain"`
	Domain     string `json:"domain"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Auth       Auth   `json:"auth"`
}
type Auth struct {
	IsApp       bool   `json:"is_app"`
	AccessToken string `json:"access_token"`
	App         struct {
		Id             int    `json:"id"`
		InstallationId int    `json:"installation_id"`
		KeyPath        string `json:"key_path"`
	}
}

type Env struct {
	Runner    Runner   `json:"runner"`
	BaseImage string   `json:"base_image"`
	Limit     int      `json:"limit"`
	Labels    []string `json:"labels"`
}

type Config struct {
	Cli       *client.Client
	Ctx       context.Context
	ImageName string
	Runner    *Runner
	Limit     int
	Labels    []string
	BaseImage string
}

func (config *Config) imageName() string {
	return config.ImageName + ":" + config.BaseImage + "-" + "2.320.0"
}

const patPath = "./pat.txt"

func main() {
	config, err := makeConfig()
	if err != nil {
		log.Println("Invalid enviroment variables: ", err)
		return
	}
	build, e := config.hasToBuild()
	if e != nil {
		log.Println("Can not find image: ", e)
		return
	}

	if build {
		if er := config.buildRunnerImage(); er != nil {
			log.Println("Can not build: ", er)
			return
		}
	}
	if ee := config.handleContainer(); ee != nil {
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
				if event.Actor.Attributes["image"] == config.imageName() {
					log.Println("Container", event.Actor.ID, " has exited", event.Actor.Attributes)
					if ee := config.handleContainer(); ee != nil {
						log.Println(ee)
						return
					}
				}
			}
		case err := <-errorsChan:
			log.Println("Error while listening to Docker events: ", err)
		case <-done:
			log.Println("Removing containers and ", patPath)
			containers, err := config.Cli.ContainerList(config.Ctx, container.ListOptions{Filters: filters.NewArgs(filters.KeyValuePair{Key: "ancestor", Value: config.imageName()})})
			if err != nil {
				log.Println("Can not remove containers", err)
				return
			}
			for _, v := range containers {
				res, err := config.Cli.ContainerExecCreate(config.Ctx, v.ID, container.ExecOptions{
					Cmd: []string{"/bin/bash", "-c", "/actions-runner/stop.sh"},
				})
				if err != nil {
					log.Println("Can not delete container ", err)
					continue
				}
				var attachResp types.HijackedResponse
				attachResp, err = config.Cli.ContainerExecAttach(context.Background(), res.ID, container.ExecStartOptions{})
				if err != nil {
					log.Println("Can not delete container ", err)
					continue
				}
				defer attachResp.Close()
				// 標準出力を読み取り
				go func() {
					if _, err := io.Copy(log.Writer(), attachResp.Reader); err != nil {
						log.Printf("Error reading attached exec output: %s", err)
					}
				}()

				// `exec`プロセスが終了するのを待つ
				for {
					// `ContainerExecInspect`で`exec`プロセスの状態を確認
					execInspect, err := config.Cli.ContainerExecInspect(context.Background(), res.ID)
					if err != nil {
						log.Fatalf("Error inspecting exec instance: %s", err)
					}

					// 終了したかを確認
					if execInspect.Running == false {
						fmt.Printf("Exec process finished with exit code: %d\n", execInspect.ExitCode)
						break
					}

					// 少し待ってから再度確認
					time.Sleep(500 * time.Millisecond)
				}
				log.Println("Delete container id: ", v.ID)
			}
			if _, err := os.Stat(patPath); !os.IsExist(err) {
				log.Println("Remove", patPath)
				if e := os.Remove(patPath); e != nil {
					log.Println("Can not remove ", patPath, " ", e)
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
	if err = json.Unmarshal(bytes, &env); err != nil {
		return nil, fmt.Errorf("Config file (config.json) is not invalid.")
	}

	if gitHubError := env.Runner.validate(); gitHubError != nil {
		return nil, fmt.Errorf("Runner is not valid in config.json %s", gitHubError)
	}

	env.Runner.setDefaultValue()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Error creating Docker client: %s", err)
	}

	var limit = env.Limit
	if limit == 0 {
		limit = 2
	}

	var baseImage = "Jammy"
	if env.BaseImage != "" {
		if _, err := os.Stat("./dockerfiles/Dockerfile" + env.BaseImage); os.IsNotExist(err) {
			return nil, fmt.Errorf("Can not find ./dockerfiles/Dockerfile%s", env.BaseImage)
		}
		baseImage = env.BaseImage
	}

	config := &Config{
		Cli:       cli,
		Ctx:       context.Background(),
		ImageName: "local-runner",
		Runner:    &env.Runner,
		Limit:     limit,
		Labels:    env.Labels,
		BaseImage: baseImage,
	}

	return config, nil
}

func (runner *Runner) validate() error {
	if runner.Owner == "" {
		return fmt.Errorf("onwer is empty")
	}

	if authError := runner.Auth.validate(); authError != nil {
		return fmt.Errorf("auth is invalid %s", authError)
	}
	return nil
}

func (runner *Runner) setDefaultValue() {
	if runner.ApiDomain == "" {
		runner.ApiDomain = "api.github.com"
	}
	if runner.Domain == "" {
		runner.Domain = "github.com"
	}
}

func (auth *Auth) validate() error {
	if !auth.IsApp {
		if auth.AccessToken == "" {
			return fmt.Errorf("access_token is empty")
		}
	} else {
		if auth.App.KeyPath == "" {
			return fmt.Errorf("key_path is empty")
		}
		if auth.App.Id == 0 {
			return fmt.Errorf("app.id is empty")
		}
		if auth.App.InstallationId == 0 {
			return fmt.Errorf("app.installation_id is empty")
		}
	}
	return nil
}

// コンテナ終了時のコールバック処理
func (config *Config) handleContainer() *error {
	// 必要な処理をここに実装
	containers, err := config.Cli.ContainerList(config.Ctx, container.ListOptions{Filters: filters.NewArgs(filters.KeyValuePair{Key: "ancestor", Value: config.imageName()})})
	if err != nil {
		log.Println("Can not get containers list")
		res := fmt.Errorf("Can not get containers list %s", err)
		return &res
	}
	if len(containers) >= config.Limit {
		return nil
	}
	j := config.Limit - len(containers)
	// コンテナの設定
	var env = []string{"GITHUB_API_DOMAIN=" + config.Runner.ApiDomain, "GITHUB_DOMAIN=" + config.Runner.Domain, "RUNNER_ALLOW_RUNASROOT=abc"}
	labels := map[string]string{}
	if config.Runner.Repository == "" {
		labels["owner"] = config.Runner.Owner
		env = append(env, "GITHUB_REPOSITORY_OWNER="+config.Runner.Owner, "LABELS="+strings.Join(config.Labels, ","))
	} else {
		labels["owner"] = config.Runner.Owner
		labels["repository"] = config.Runner.Repository
		env = append(env, "GITHUB_REPOSITORY_OWNER="+config.Runner.Owner, "GITHUB_REPOSITORY_NAME="+config.Runner.Repository, "LABELS="+strings.Join(config.Labels, ","))
	}

	var binds []string
	if config.Runner.Auth.IsApp {
		env = append(env, "APP_ID="+strconv.Itoa(config.Runner.Auth.App.Id), "INSTALL_ID="+strconv.Itoa(config.Runner.Auth.App.InstallationId), "KEY_FILE_PATH=/mnt/private-key.pem")
		binds = []string{
			fmt.Sprintf("%s:%s:ro", config.Runner.Auth.App.KeyPath, "/mnt/private-key.pem"), // roはリードオンリー
		}
	} else {
		patFile, err := os.Create(patPath)
		if err != nil {
			log.Println("Can not create file", patPath)
			res := fmt.Errorf("Can not create file %s %s", patPath, err)
			return &res
		}

		patFile.Write([]byte(config.Runner.Auth.AccessToken))
		abspath, err := filepath.Abs(patFile.Name())
		if err != nil {
			log.Println("Can not get file path", patPath)
			res := fmt.Errorf("Can not get file path %s %s", patPath, err)
			return &res
		}
		binds = []string{
			fmt.Sprintf("%s:%s:ro", abspath, "/mnt/pat.txt"), // roはリードオンリー
		}
	}

	containerConfig := &container.Config{
		Image:  config.imageName(),
		Env:    env,
		Labels: labels,
	}

	// ホスト設定（自動削除など）
	hostConfig := &container.HostConfig{
		AutoRemove: true, // コンテナ終了後に自動で削除
		Binds:      binds,
	}

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
	return nil
}

func (config *Config) buildRunnerImage() error {
	// イメージビルドオプションの設定
	options := types.ImageBuildOptions{
		Tags:       []string{config.imageName()},
		Dockerfile: "Dockerfile" + config.BaseImage,
		Remove:     true,
	}

	buildContext, err := createBuildContext("./dockerfiles")
	if err != nil {
		return fmt.Errorf("Error creating build context: %s", err)
	}

	res, er := config.Cli.ImageBuild(config.Ctx, buildContext, options)
	if er != nil {
		return fmt.Errorf("build failed: %s", er)
	}
	defer res.Body.Close()

	// ビルドの出力を表示
	if _, err = io.Copy(os.Stdout, res.Body); err != nil {
		return fmt.Errorf("Error reading build output: %s", err)
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
		// ヘッダーの名前を相対パスに変更
		relPath, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		header.Name = relPath

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

func (config *Config) hasToBuild() (bool, error) {
	list, e := config.Cli.ImageList(config.Ctx, image.ListOptions{Filters: filters.NewArgs(filters.KeyValuePair{Key: "reference", Value: config.imageName()})})
	if e != nil {
		log.Println(e)
		return true, fmt.Errorf("does not find %s can not get image list", config.imageName())
	}
	if len(list) > 0 {
		return false, nil
	}
	return true, nil
}
