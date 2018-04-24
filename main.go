package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/opencopilot/haproxy-manager/manager"
	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

type server struct{}

func (s *server) GetStatus(ctx context.Context, in *pb.ManagerStatusRequest) (*pb.ManagerStatus, error) {
	// return the status of the manager - what should this contain?
	return &pb.ManagerStatus{}, nil
}

func (s *server) Configure(ctx context.Context, in *pb.ConfigureRequest) (*pb.ManagerStatus, error) {
	// execute the configuration change on the service (HAProxy)
	return &pb.ManagerStatus{}, nil
}

func startServer() {
	lis, err := net.Listen("tcp", "127.0.0.1:50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	logger, err := zap.NewProduction()
	defer logger.Sync()
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}

	s := grpc.NewServer(
		// grpc.Creds(creds),
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.StreamServerInterceptor(logger),
			grpc_recovery.StreamServerInterceptor(),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.UnaryServerInterceptor(logger),
			grpc_recovery.UnaryServerInterceptor(),
		)),
	)

	pb.RegisterManagerServer(s, &server{})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func stop() {
	log.Println("Should stop service...")
}

func startService() {
	log.Println("Starting HAProxy")
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	containerConfig := &container.Config{
		Image: "haproxy",
		Labels: map[string]string{
			"com.opencopilot.service": "haproxy",
		},
	}

	res, err := dockerCli.ContainerCreate(ctx, containerConfig, nil, nil, "")
	if err != nil {
		log.Fatal(err)
	}

	reader, err := dockerCli.ImagePull(ctx, containerConfig.Image, dockerTypes.ImagePullOptions{})
	if err != nil {
		log.Fatal(err)
	}
	reader.Close()

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
		dockerCli.ContainerStop(ctx, container.ID, nil)
	}
}

func main() {
	log.Println("Starting HAProxy Manager gRPC server")
	go startServer()

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
