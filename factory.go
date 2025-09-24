package grpc

import "google.golang.org/grpc"

var grpcServerFactory func() *grpc.Server

func RegisterGrpcServerFactory(f func() *grpc.Server) {
	grpcServerFactory = f
}

