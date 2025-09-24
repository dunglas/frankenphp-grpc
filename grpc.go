package grpc

import (
	"fmt"
	"runtime"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/dunglas/frankenphp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// HandleRequest sends a request to the PHP worker pool and waits for a response.
// This is the primary entrypoint for the PHP C extension.
func HandleRequest(request any) any {
	responseChan := make(chan any)

	w.messages <- message{
		request:      request,
		responseChan: responseChan,
	}

	// Block until the response is received from the PHP worker.
	return <-responseChan
}

// message represents a single request/response cycle with the PHP worker.
type message struct {
	request      any
	responseChan chan any
}

// grpcApp holds the common configuration and logic for both the
// standalone gRPC app and the gRPC-Web HTTP handler.
type grpcApp struct {
	// The path to the PHP worker script that will handle the gRPC services.
	Worker string `json:"worker,omitempty"`

	// The minimum number of worker threads to spawn.
	MinThreads int `json:"min_threads,omitempty"`

	logger *zap.Logger
	srv    *grpc.Server
}

// Provision sets up the common gRPC server and worker configuration.
func (a *grpcApp) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()

	// Set defaults
	if a.MinThreads <= 0 {
		a.MinThreads = runtime.NumCPU()
	}
	if a.Worker == "" {
		a.Worker = "grpc-worker.php"
	}

	// Configure and register the FrankensPHP worker
	w.minThread = a.MinThreads
	w.filename = a.Worker
	frankenphp.RegisterExternalWorker(w)

	// Ensure a gRPC server factory has been provided by the user's application
	if grpcServerFactory == nil {
		return fmt.Errorf("no gRPC server factory has been registered. Call grpc.RegisterGrpcServerFactory in your main package's init() function")
	}

	a.srv = grpcServerFactory()
	a.logger.Info("provisioned gRPC server", zap.String("worker", a.Worker), zap.Int("min_threads", a.MinThreads))

	return nil
}

// UnmarshalCaddyfile parses the common gRPC directives from the Caddyfile.
func (a *grpcApp) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "worker":
				if !d.NextArg() {
					return d.ArgErr()
				}
				a.Worker = d.Val()

			case "min_threads":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t, err := strconv.Atoi(d.Val())
				if err != nil {
					return err
				}
				a.MinThreads = t

			default:
				// This allows parent structs to handle their own directives
				return nil
			}
		}
	}
	return nil
}
