package main

import (
	"context"
	"fmt"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"github.com/dunglas/frankenphp"
	phpGrpc "github.com/dunglas/frankenphp-grpc"
	_ "github.com/dunglas/frankenphp/caddy"
	"github.com/go-viper/mapstructure/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func init() {
	phpGrpc.RegisterGrpcServerFactory(func() *grpc.Server {
		s := grpc.NewServer()
		RegisterGreeterServer(s, &server{})
		reflection.Register(s)

		return s
	})
}

func main() {
	caddycmd.Main()
}

type server struct {
	UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(_ context.Context, in *HelloRequest) (*HelloReply, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("the Name field is required")
	}

	var phpRequest map[string]any
	if err := mapstructure.Decode(in, &phpRequest); err != nil {
		return nil, err
	}

	phpResponse := phpGrpc.HandleRequest(phpRequest)

	var response HelloReply
	if err := mapstructure.Decode(phpResponse.(frankenphp.AssociativeArray).Map, &response); err != nil {
		return nil, err
	}

	return &response, nil
}
