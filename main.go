package main

import (
	"bufio"
	"context"
	"encoding/json"
	"html/template"
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
	nat "github.com/docker/go-connections/nat"
)

var (
	// ConfigDir is the config directory of opencopilot on the host
	ConfigDir = os.Getenv("CONFIG_DIR")
)

func startService() {
	log.Println("starting HAProxy")
	dockerCli, err := dockerClient.NewClientWithOpts()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	containerConfig := &container.Config{
		Image: "haproxy:latest",
		Labels: map[string]string{
			"com.opencopilot.service": "LB",
		},
		ExposedPorts: nat.PortSet{
			"80/tcp": struct{}{},
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
		AutoRemove: true,
		Binds: []string{
			filepath.Join(ConfigDir, "/services/LB") + ":/usr/local/etc/haproxy",
		},
		PublishAllPorts: true,
	}
	res, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "com.opencopilot.service.LB")
	if err != nil {
		log.Println(err)
	}

	log.Printf("HAProxy container started with ID: %s\n", res.ID[:10])

	startErr := dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if startErr != nil {
		log.Fatal(startErr)
	}

	statusCh, errCh := dockerCli.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	case status := <-statusCh:
		log.Printf("status: %v", status.StatusCode)
	}
}

func stopService() {
	log.Println("stopping HAProxy")
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.service=LB"),
		filters.Arg("name", "com.opencopilot.service.LB"),
	)
	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)

	}
	for _, container := range containers {
		dockerCli.ContainerStop(ctx, container.ID, nil)
		log.Printf("stopping container with ID: %s\n", container.ID[:10])
	}
}

func ensureConfigDirectory() {
	if ConfigDir == "" {
		ConfigDir = "/etc/opencopilot"
	}
	confPath := filepath.Join(ConfigDir, "/services/LB")
	log.Printf("ensuring the configuration path exists: %s", confPath)
	err := os.MkdirAll(confPath, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}
}

func ensureService(quit chan struct{}) {
	for {
		select {
		case <-quit:
			return
		default:
			startService()
		}
	}
}

func configureService(configString string) {
	log.Println(configString)

	config := make(map[string]interface{})
	err := json.Unmarshal([]byte(configString), &config)
	if err != nil {
		log.Println(err)
	}

	t, err := template.ParseFiles("./haproxy.template.cfg")
	if err != nil {
		log.Print(err)
	}

	f, err := os.Create(filepath.Join(ConfigDir, "/services/LB/haproxy.cfg"))
	if err != nil {
		log.Fatal(err)
	}

	w := bufio.NewWriter(f)
	errT := t.Execute(w, config)
	if errT != nil {
		log.Println(errT)
	}
	w.Flush()
	f.Close()

	log.Println(config)

	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.service=LB"),
		filters.Arg("name", "com.opencopilot.service.LB"),
	)
	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)

	}
	for _, container := range containers {
		dockerCli.ContainerKill(ctx, container.ID, "SIGHUP")
	}
}

func main() {
	log.Println("ensuring config directory")
	ensureConfigDirectory()

	log.Println("starting HAProxy Manager gRPC server")
	go startServer()

	sigs := make(chan os.Signal, 1)
	stopEnsuringService := make(chan struct{}, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("received shutdown signal")
		stopEnsuringService <- struct{}{}
		stopService()
	}()

	log.Println("ensuring the HAProxy is running...")
	ensureService(stopEnsuringService)
}
