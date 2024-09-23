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
	"path/filepath"
	"strconv"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func main() {
	sock := os.Getenv("DOCKER_SOCK")
	if sock == "" {
		log.Println("DOCKER_SOCK is not registered")
		return
	}
	cli, err := client.NewClientWithOpts(client.WithHost(sock), client.WithAPIVersionNegotiation())
	if err != nil {
		log.Println("Error creating Docker client: ", err)
	}

	imageName := "local-runner:latest"

	ctx := context.Background()

	list, e := cli.ImageList(ctx, image.ListOptions{})
	if e != nil {
		log.Println(e)
	}

	var build bool = true
	for _, v := range list {
		tags := v.RepoTags
		for _, t := range tags {
			if t == imageName {
				build = false
			}
		}
	}

	if build {
		er := buildRunnerImage(imageName, ctx, cli)
		if er != nil {
			log.Println("Can not build: ", er)
			return
		}
	}
	ee := handleContainer(cli, ctx, imageName)
	if ee != nil {
		log.Println(ee)
		return
	}

	eventsChan, errorsChan := cli.Events(ctx, events.ListOptions{})

	// イベントストリームの監視
	for {
		select {
		case event := <-eventsChan:
			if event.Type == events.ContainerEventType && event.Action == "die" {
				log.Println("Container", event.Actor.ID, " has exited", event.Actor.Attributes)
				ee := handleContainer(cli, ctx, imageName)
				if ee != nil {
					log.Println(ee)
					return
				}
			}
		case err := <-errorsChan:
			log.Println("Error while listening to Docker events: ", err)
		}
	}
}

// コンテナ終了時のコールバック処理
func handleContainer(cli *client.Client, ctx context.Context, imageName string) *error {
	// 必要な処理をここに実装
	limit := 2
	containers, _ := cli.ContainerList(ctx, container.ListOptions{})
	var count = 0
	for _, v := range containers {
		if v.Image == imageName {
			count++
		}
	}
	if limit > count {
		githubAccessToken := os.Getenv("GITHUB_ACCESS_TOKEN")
		ownerName := os.Getenv("GITHUB_REPOSITORY_OWNER")
		repoName := os.Getenv("GITHUB_REPOSITORY_NAME")
		if githubAccessToken == "" {
			e := fmt.Errorf("GITHUB_ACCESS_TOKEN is not registered")
			return &e
		}
		if ownerName == "" {
			e := fmt.Errorf("GITHUB_REPOSITORY_OWNER is not registered")
			return &e
		}
		if repoName == "" {
			e := fmt.Errorf("GITHUB_REPOSITORY_NAME is not registered")
			return &e
		}
		// コンテナの設定
		containerConfig := &container.Config{
			Image: imageName,
			Env:   []string{"GITHUB_API_DOMAIN=api.github.com", "GITHUB_DOMAIN=github.com", "RUNNER_ALLOW_RUNASROOT=abc", "GITHUB_ACCESS_TOKEN=" + githubAccessToken, "GITHUB_REPOSITORY_OWNER=" + ownerName, "GITHUB_REPOSITORY_NAME=" + repoName, "RUNNER_ALLOW_RUNASROOT=abc"},
		}

		// ホスト設定（自動削除など）
		hostConfig := &container.HostConfig{
			AutoRemove: true, // コンテナ終了後に自動で削除
		}

		j := limit - count
		for i := 0; i < j; i++ {
			go func() {
				seed := time.Now().UnixNano()
				rand.New(rand.NewSource(seed))
				val := rand.Intn(100000)

				// コンテナの作成
				resp, err := cli.ContainerCreate(
					ctx,
					containerConfig,
					hostConfig,
					nil,
					nil,
					"local-runner-"+strconv.Itoa(val),
				)

				if err != nil {
					log.Println("Error creating container: ", err)
					return
				}

				// コンテナのIDを表示
				log.Println("Container created with ID: ", resp.ID)

				// コンテナを起動
				if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
					log.Println("Error starting container: ", err)
					return
				}
			}()
		}
	}
	return nil
}

func buildRunnerImage(imageName string, ctx context.Context, cli *client.Client) error {
	// イメージビルドオプションの設定
	options := types.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}

	buildContext, err := createBuildContext("./")
	if err != nil {
		log.Println("Error creating build context: ", err)
		return err
	}

	res, er := cli.ImageBuild(ctx, buildContext, options)
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
