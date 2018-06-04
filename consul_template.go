package main

import (
	"context"
	"io/ioutil"
	"log"

	"path/filepath"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/strslice"
	dockerClient "github.com/docker/docker/client"
)

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

func startConsulTemplate(dockerCli *dockerClient.Client) {
	alreadyRunning, _, err := isContainerRunning(dockerCli, "com.opencopilot.consul-template."+ServiceName)
	if err != nil {
		log.Fatal(err)
	}
	if alreadyRunning {
		log.Println("consul-template already running, stopping")
		stopConsulTemplate(dockerCli)
	}

	ctx := context.Background()

	ConfDir := filepath.Join(ConfigDir, "/services/", ServiceName)

	containerConfig := &container.Config{
		Image: "hashicorp/consul-template:0.19.4-alpine",
		Labels: map[string]string{
			"com.opencopilot.service." + ServiceName: "consul-template",
		},
		Env: []string{
			"CONFIG_DIR=" + ConfigDir,
			"INSTANCE_ID=" + InstanceID,
			"CONSUL_ADDR=" + ConsulAddr,
		},
		Cmd: strslice.StrSlice{
			"-template",
			filepath.Join(ConfDir, "haproxy.ctmpl") + ":" + filepath.Join(ConfDir, "haproxy.cfg"),
			"-consul-addr",
			ConsulAddr,
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
			ConfDir + ":" + ConfDir,
		},
		NetworkMode: "host",
	}
	res, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "com.opencopilot.consul-template."+ServiceName)
	if err != nil {
		log.Println(err)
	}

	err = dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("consul-template container started with ID: %s\n", res.ID[:10])

	waitForContainerStop(dockerCli, res.ID)
}

func stopConsulTemplate(dockerCli *dockerClient.Client) {
	log.Println("stopping consul-template")

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("name", "com.opencopilot.consul-template."+ServiceName),
	)
	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)

	}
	for _, container := range containers {
		dockerCli.ContainerKill(ctx, container.ID, "SIGTINT")
		dockerCli.ContainerStop(ctx, container.ID, nil)
		log.Printf("stopping container with ID: %s\n", container.ID[:10])
	}
}
