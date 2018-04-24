package main

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"path/filepath"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
)

var (
	// ConfigDir is the config directory of opencopilot on the host
	ConfigDir = os.Getenv("CONFIG_DIR")
)

func stop() {
	log.Println("Should stop service...")
	stopService()
}

func startService() {
	log.Println("Starting HAProxy")
	dockerCli, err := dockerClient.NewClientWithOpts()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	containerConfig := &container.Config{
		Image: "haproxy:latest",
		Labels: map[string]string{
			"com.opencopilot.service": "haproxy",
		},
	}

	reader, err := dockerCli.ImagePull(ctx, containerConfig.Image, dockerTypes.ImagePullOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()
	if _, err := ioutil.ReadAll(reader); err != nil {
		log.Panic(err)
	}

	hostConfig := &container.HostConfig{
		Binds: []string{
			filepath.Join(ConfigDir, "/services/LB") + ":/usr/local/etc/haproxy",
		},
	}
	res, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "")
	if err != nil {
		log.Fatal(err)
	}

	log.Println(res.ID)

	startErr := dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if startErr != nil {
		log.Fatal(startErr)
	}
}

func stopService() {
	log.Println("Stopping HAProxy")
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.service=haproxy"),
	)
	containers, err := dockerCli.ContainerList(context.Background(), dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, container := range containers {
		log.Printf(container.ID)
		dockerCli.ContainerStop(ctx, container.ID, nil)
	}
}

func ensureConfigDirectory() {
	if ConfigDir == "" {
		ConfigDir = "/etc/opencopilot"
	}
	confPath := filepath.Join(ConfigDir, "/services/LB")
	log.Println(confPath)
	err := os.MkdirAll(confPath, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	log.Println("Ensuring config directory")
	ensureConfigDirectory()

	log.Println("Starting HAProxy Manager gRPC server")
	go startServer()

	startService()

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		stop()
		done <- true
	}()

	<-done
}
