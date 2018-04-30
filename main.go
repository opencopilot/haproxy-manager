package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
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
	config := make(map[string]interface{})
	err := json.Unmarshal([]byte(configString), &config)
	if err != nil {
		log.Println(err)
	}

	t, err := template.ParseFiles("./haproxy.template.cfg")
	if err != nil {
		log.Print(err)
	}

	configPath := filepath.Join(ConfigDir, "/services/LB/haproxy.cfg")

	prevConfig, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Fatal(err)
	}
	oldHash := md5.Sum(prevConfig)

	var newConfig bytes.Buffer
	errT := t.Execute(&newConfig, config)
	if errT != nil {
		log.Println(errT)
	}
	newHash := md5.Sum([]byte(newConfig.String()))

	// Compare md5 hashes of old and new config, if they match just return
	if bytes.Compare(oldHash[:], newHash[:]) == 0 {
		return
	}

	// Execute the new template config and write to file
	f, err := os.Create(configPath)
	if err != nil {
		log.Fatal(err)
	}

	w := bufio.NewWriter(f)
	err = t.Execute(w, config)
	if err != nil {
		log.Println(err)
	}
	w.Flush()
	f.Close()

	// Go find the docker container running the service and send a SIGHUB to have it reload the config
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

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		log.Println("received shutdown signal")
		stopEnsuringService <- struct{}{}
		stopService(dockerCli)
	}()

	log.Println("ensuring that HAProxy is running...")
	ensureService(dockerCli, stopEnsuringService)
}
