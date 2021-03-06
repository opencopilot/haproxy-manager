package main

import (
	"context"
	"io/ioutil"
	"log"

	"path/filepath"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

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

func startService(dockerCli *dockerClient.Client) {
	alreadyRunning, _, err := isContainerRunning(dockerCli, "com.opencopilot.service."+ServiceName)
	if err != nil {
		log.Fatal(err)
	}
	if alreadyRunning {
		log.Println("HAProxy already running, stopping")
		stopService(dockerCli)
	}
	log.Println("starting HAProxy")

	ctx := context.Background()

	containerConfig := &container.Config{
		Image: "haproxy:1.8.9",
		Labels: map[string]string{
			"com.opencopilot.service." + ServiceName: "haproxy",
		},
		ExposedPorts: nat.PortSet{
			"80/tcp":   struct{}{},
			"8080/tcp": struct{}{},
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
		// RestartPolicy: container.RestartPolicy{Name: "always"},
		AutoRemove: true,
		Binds: []string{
			filepath.Join(ConfigDir, "/services/", ServiceName) + ":/usr/local/etc/haproxy",
		},
		PortBindings: nat.PortMap{
			"80/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "80"},
			},
			"8080/tcp": []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: "8080"},
			},
		},
	}
	res, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "com.opencopilot.service."+ServiceName)
	if err != nil {
		log.Println(err)
	}

	startErr := dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if startErr != nil {
		log.Fatal(startErr)
	}

	log.Printf("HAProxy container started with ID: %s\n", res.ID[:10])

	waitForContainerStop(dockerCli, res.ID)
}

func stopService(dockerCli *dockerClient.Client) {
	log.Println("stopping HAProxy")

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("name", "com.opencopilot.service."+ServiceName),
	)
	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)

	}
	for _, container := range containers {
		dockerCli.ContainerKill(ctx, container.ID, "SIGTERM")
		// dockerCli.ContainerStop(ctx, container.ID, nil)
		log.Printf("removing container with ID: %s\n", container.ID[:10])
	}
}

func configureService(dockerCli *dockerClient.Client) {
	log.Println("configuring " + ServiceName)
	// Go find the docker container running the service and send a SIGHUP to have it reload the config
	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.service="+ServiceName),
		filters.Arg("name", "com.opencopilot.service."+ServiceName),
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
