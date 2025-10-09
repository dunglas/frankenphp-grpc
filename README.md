# FrankenPHP gRPC Server

A [FrankenPHP](https://frankenphp.dev) extension that allows you to run a [gRPC](https://grpc.io/) server triggering code written either in PHP or Go.
Under the hood, this extension uses the [gRPC for Go](https://grpc.io/docs/languages/go/) library and FrankenPHP's [Go extension support](https://frankenphp.dev/docs/extensions/).

> [!WARNING]
>
> This extension is highly experimental and not recommended for production use.
> The public API may change at any time without notice.

## Features

* Run a high performance gRPC server with FrankenPHP (the PHP part is executed in a worker loop)
* Write gRPC service handlers in PHP
* Write gRPC service handlers in Go
* Write gRPC service handlers in a mix of PHP and Go 🤯
* All features supported by the [gRPC for Go](https://grpc.io/docs/languages/go/) library
* Entirely written in Go, no C code!
* [API Platform](https://api-platform.com) compatibility!

## Prerequisites

* FrankenPHP extensions prerequisites: https://frankenphp.dev/docs/extensions/#prerequisites
* gRPC for Go prerequisites: https://grpc.io/docs/languages/go/quickstart/#prerequisites

## Usage

### Create a Go module

```console
go mod init example.com/mygrpcserver 
```

### Create a Protobuf Definition:

Create a `.proto` file describing your gRPC service and messages.

Example (in a `helloworld/helloworld.proto` file):

```protobuf
syntax = "proto3";

option go_package = "example.com/mygrpcserver/helloworld";

package helloworld;

service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply) {}
}

message HelloRequest {
  string name = 1;
}

message HelloReply {
  string message = 1;
}
```

Generate the Go code:

```console
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative helloworld/helloworld.proto
```

### Implement the gRPC Server in Go

```go
package mygrpcserver

import (
	"context"
	"fmt"

	pb "example.com/mygrpcserver/helloworld"
	"github.com/dunglas/frankenphp"
	phpGrpc "github.com/dunglas/frankenphp-grpc"
	"github.com/go-viper/mapstructure/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func init() {
	phpGrpc.RegisterGrpcServerFactory(func() *grpc.Server {
		s := grpc.NewServer()
		pb.RegisterGreeterServer(s, &server{})
		reflection.Register(s)

		return s
	})
}

type server struct {
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(_ context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("the Name field is required")
	}

    // Convert the request to a map[string]any
	var phpRequest map[string]any
	if err := mapstructure.Decode(in, &phpRequest); err != nil {
		return nil, err
	}

    // Call the PHP code, pass the map as a PHP associative array
	phpResponse := phpGrpc.HandleRequest(phpRequest)

    // Convert the PHP response (a map) back to a HelloReply struct
	var response pb.HelloReply
	if err := mapstructure.Decode(phpResponse.(frankenphp.AssociativeArray).Map, &response); err != nil {
		return nil, err
	}

	return &response, nil
}
```

Refer to the [gRPC for Go documentation](https://grpc.io/docs/languages/go/) for more details on how to implement your gRPC service in Go.
Rfer to the [FrankenPHP extensions documentation](https://frankenphp.dev/docs/extensions/) for more details on how to pass data from Go to PHP and vice versa.

### Implement the gRPC Service Handler in PHP

Create a file named `grpc-worker.php` in the same directory as the FrankenPHP binary we'll build later:

```php
<?php

// Require the Composer autoloader here if needed (API Platform, Symfony, etc.)
//require __DIR__ . '/vendor/autoload.php';

// Handler outside the loop for better performance (doing less work)
$handler = static function (array $request): array  {
	// Do something with the gRPC request

    return ['message' => "Hello, {$request['Name']}"];
};

$maxRequests = (int)($_SERVER['MAX_REQUESTS'] ?? 0);
for ($nbRequests = 0; !$maxRequests || $nbRequests < $maxRequests; ++$nbRequests) {
    $keepRunning = \frankenphp_handle_request($handler);

    // Call the garbage collector to reduce the chances of it being triggered in the middle of the handling of a request
    gc_collect_cycles();

    if (!$keepRunning) {
      break;
    }
}
```

### Create the `Caddyfile`

Create a `Caddyfile` in the same directory as the FrankenPHP binary we'll build later:

```caddyfile
{
	frankenphp
	grpc {
		address :50051 # Optional
		worker grpc-worker.php # Optional
		min_threads 50 # Optional, defaults to runtime.NumCPU()
	}
}
```

### Build and Run the FrankenPHP Binary with the gRPC Extension

Run the server:

```console
XCADDY_DEBUG=1
    CGO_ENABLED=1 \
	CGO_CFLAGS="$(php-config --includes) -I/opt/homebrew/include/" \
	CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs) -L/opt/homebrew/lib/ -L/usr/lib" \
	xcaddy build

./caddy run
```

Your gRPC server should now be running on `localhost:50051`.

We recommend using [gRPC UI](https://github.com/fullstorydev/grpcui) (a Postman-like GUI for gRPC) to test your server.

## Credits

Created by [Kévin Dunglas](https://dunglas.dev) and sponsored by [Les-Tilleuls.coop](https://les-tilleuls.coop).
