package main

import (
	"context"
	"log"
	"net"

	pb "github.com/opencopilot/haproxy-manager/manager"
	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	dockerClient "github.com/docker/docker/client"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

type server struct {
	dockerCli *dockerClient.Client
}

func (s *server) GetStatus(ctx context.Context, in *pb.ManagerStatusRequest) (*pb.ManagerStatus, error) {
	// return the status of the manager - what should this contain?
	return &pb.ManagerStatus{}, nil
}

func (s *server) Configure(ctx context.Context, in *pb.ConfigureRequest) (*pb.ManagerStatus, error) {
	// execute the configuration change on the service (HAProxy)
	configureService(s.dockerCli, in.Config)
	return &pb.ManagerStatus{}, nil
}

func startServer(dockerCli *dockerClient.Client) {
	lis, err := net.Listen("tcp", ":50052")
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

	pb.RegisterManagerServer(s, &server{
		dockerCli: dockerCli,
	})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
