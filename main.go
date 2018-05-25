package main

import (
	"context"
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
	// InstanceID is the instance id of this device
	InstanceID = os.Getenv("INSTANCE_ID")
	// ConsulAddr is where consul-template can reach consul
	ConsulAddr = os.Getenv("CONSUL_ADDR")
)

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

	configFilePath := filepath.Join(ConfigDir, "/services/LB/haproxy.cfg")
	configTemplateFilePath := filepath.Join(ConfigDir, "/services/LB/haproxy.ctmpl")

	if _, err := os.Stat(configFilePath); os.IsNotExist(err) { // if config doesn't exist, add the default
		err = os.Link("./haproxy.cfg", configFilePath)
		if err != nil {
			log.Fatal(err)
		}
	}

	if _, err := os.Stat(configTemplateFilePath); err == nil { // if config template exists, remove it
		err = os.Remove(configTemplateFilePath)
		if err != nil {
			log.Fatal(err)
		}
	}

	err = os.Link("./haproxy.ctmpl", configTemplateFilePath)
	if err != nil {
		log.Fatal(err)
	}

}

func isContainerRunning(dockerCli *dockerClient.Client, containerName string) (bool, *string, error) {
	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("name", containerName),
	)
	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		return false, nil, err
	}
	for _, container := range containers {
		return true, &container.ID, nil
	}
	return false, nil, nil
}

func waitForContainerStop(dockerCli *dockerClient.Client, containerID string) {
	statusCh, errCh := dockerCli.ContainerWait(context.Background(), containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	case status := <-statusCh:
		log.Printf("status: %v", status.StatusCode)
	}
}

func main() {
	log.Println("ensuring config directory")
	ensureConfigDirectory()

	dockerCli, err := dockerClient.NewClientWithOpts()
	if err != nil {
		log.Fatal(err)
	}

	if ConsulAddr == "" {
		ConsulAddr = "localhost:8500"
	}

	sigs := make(chan os.Signal, 1)
	stopEnsuringService := make(chan struct{}, 1)
	stopEnsuringConsulTemplate := make(chan struct{}, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	log.Println("starting consul-template")
	go ensureConsulTemplate(dockerCli, stopEnsuringConsulTemplate)

	log.Println("ensuring that HAProxy is running...")
	go ensureService(dockerCli, stopEnsuringService)

	log.Println("starting HAProxy Manager gRPC server")
	go startServer(dockerCli)

	go watchConfigFile(dockerCli)

	func() {
		<-sigs
		log.Println("received shutdown signal")

		stopEnsuringConsulTemplate <- struct{}{}
		stopConsulTemplate(dockerCli)

		stopEnsuringService <- struct{}{}
		stopService(dockerCli)
	}()
}
