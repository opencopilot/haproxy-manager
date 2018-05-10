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
	"github.com/docker/docker/api/types/strslice"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/fsnotify/fsnotify"
)

var (
	// ConfigDir is the config directory of opencopilot on the host
	ConfigDir = os.Getenv("CONFIG_DIR")
	// InstanceID is the instance id of this device
	InstanceID = os.Getenv("INSTANCE_ID")
)

func startService(dockerCli *dockerClient.Client) {
	log.Println("starting HAProxy")

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

func stopService(dockerCli *dockerClient.Client) {
	log.Println("stopping HAProxy")

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

func configureService(dockerCli *dockerClient.Client, configString string) {
	log.Println("configuring LB")
	// Go find the docker container running the service and send a SIGHUP to have it reload the config
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

func ensureService(dockerCli *dockerClient.Client, quit chan struct{}) {
	for {
		select {
		case <-quit:
			return
		default:
			startService(dockerCli)
		}
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

func startConsulTemplate(dockerCli *dockerClient.Client) {
	ctx := context.Background()

	LBConfDir := filepath.Join(ConfigDir, "/services/LB")

	containerConfig := &container.Config{
		Image: "hashicorp/consul-template",
		Labels: map[string]string{
			"com.opencopilot.consul-template": "LB",
		},
		Cmd: strslice.StrSlice{
			"-template",
			filepath.Join(LBConfDir, "haproxy.ctmpl") + ":" + filepath.Join(LBConfDir, "haproxy.cfg"),
			"-consul-addr",
			"host.docker.internal:8500",
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
			LBConfDir + ":" + LBConfDir,
		},
		NetworkMode: "host",
	}
	res, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "com.opencopilot.consul-template.LB")
	if err != nil {
		log.Println(err)
	}

	startErr := dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if startErr != nil {
		log.Fatal(startErr)
	}

	log.Printf("consul-template container started with ID: %s\n", res.ID[:10])

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

func ensureConsulTemplate(dockerCli *dockerClient.Client, quit chan struct{}) {
	for {
		select {
		case <-quit:
			return
		default:
			startConsulTemplate(dockerCli)
		}
	}
}

func stopConsulTemplate(dockerCli *dockerClient.Client) {
	log.Println("stopping consul-template")

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.consul-template=LB"),
		filters.Arg("name", "com.opencopilot.consul-template.LB"),
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

func watchConfigFile(dockerClient *dockerClient.Client) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)
				}
				if event.Op&fsnotify.Chmod == fsnotify.Chmod {
					configureService(dockerClient, "")
				}
			case err := <-watcher.Errors:
				log.Println("error:", err)
			}
		}
	}()
	err = watcher.Add(filepath.Join(ConfigDir, "/services/LB/haproxy.cfg"))
	if err != nil {
		log.Fatal(err)
	}
	<-done
}

func main() {
	log.Println("ensuring config directory")
	ensureConfigDirectory()

	dockerCli, err := dockerClient.NewClientWithOpts()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("starting HAProxy Manager gRPC server")
	go startServer(dockerCli)

	sigs := make(chan os.Signal, 1)
	stopEnsuringService := make(chan struct{}, 1)
	stopEnsuringConsulTemplate := make(chan struct{}, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("received shutdown signal")
		stopEnsuringConsulTemplate <- struct{}{}
		stopConsulTemplate(dockerCli)
		stopEnsuringService <- struct{}{}
		stopService(dockerCli)
	}()

	go watchConfigFile(dockerCli)

	log.Println("starting consul-template")
	go ensureConsulTemplate(dockerCli, stopEnsuringConsulTemplate)

	log.Println("ensuring that HAProxy is running...")
	ensureService(dockerCli, stopEnsuringService)
}
