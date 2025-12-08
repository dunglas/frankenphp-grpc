package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/dunglas/frankenphp"
	caddyFrankenPHP "github.com/dunglas/frankenphp/caddy"
	"google.golang.org/grpc"
)

var w frankenphp.Workers

func init() {
	caddy.RegisterModule(Grpc{})
	httpcaddyfile.RegisterGlobalOption("grpc", parseGlobalOption)
}

var grpcServerFactory func() *grpc.Server

func RegisterGrpcServerFactory(f func() *grpc.Server) {
	grpcServerFactory = f
}

func HandleRequest(ctx context.Context, request any) (any, error) {
	return w.SendMessage(ctx, request, nil)
}

type Grpc struct {
	Address        string `json:"address,omitempty"`
	NumThreads     int    `json:"num_threads,omitempty"`
	WorkerFileName string `json:"worker_file_name,omitempty"`

	ctx    caddy.Context
	logger *slog.Logger
	srv    *grpc.Server
}

// CaddyModule returns the Caddy module information.
func (Grpc) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "grpc",
		New: func() caddy.Module { return new(Grpc) },
	}
}

func (g *Grpc) Provision(ctx caddy.Context) error {
	g.logger = ctx.Slogger()
	g.ctx = ctx

	if g.Address == "" {
		g.Address = ":50051"
	}

	if g.NumThreads <= 0 {
		g.NumThreads = runtime.NumCPU()
	}

	if g.WorkerFileName == "" {
		g.WorkerFileName = "grpc-worker.php"
	}

	w = caddyFrankenPHP.RegisterWorkers("m#Grpc", g.WorkerFileName, g.NumThreads)

	return nil
}

func (g Grpc) Start() error {
	address, err := caddy.ParseNetworkAddress(g.Address)
	if err != nil {
		return err
	}

	lnAny, err := address.Listen(g.ctx, 0, net.ListenConfig{})
	if err != nil {
		return err
	}

	ln := lnAny.(net.Listener)

	if grpcServerFactory == nil {
		return fmt.Errorf("no gRPC server factory registered")
	}

	g.srv = grpcServerFactory()
	go func() {
		if err := g.srv.Serve(ln); err != nil {
			g.logger.LogAttrs(g.ctx, slog.LevelError, "failed to start gRPC server", slog.Any("error", err))
		}
	}()

	g.logger.LogAttrs(g.ctx, slog.LevelInfo, "gRPC server started", slog.String("address", g.Address))

	return nil
}

func (g Grpc) Stop() error {
	if g.srv != nil {
		g.srv.GracefulStop()
		g.srv = nil
	}

	return nil
}

func (g *Grpc) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			// when adding a new directive, also update the allowedDirectives error message
			switch d.Val() {
			case "address":
				if !d.NextArg() {
					return d.ArgErr()
				}

				g.Address = d.Val()
			case "worker":
				if !d.NextArg() {
					return d.ArgErr()
				}

				g.WorkerFileName = d.Val()
			case "min_threads":
				if !d.NextArg() {
					return d.ArgErr()
				}

				t, err := strconv.Atoi(d.Val())
				if err != nil {
					return nil
				}
				g.NumThreads = t
			default:
				return d.Errf(`unrecognized subdirective "%s"`, d.Val())
			}
		}
	}

	return nil
}

func parseGlobalOption(d *caddyfile.Dispenser, _ any) (any, error) {
	app := &Grpc{}
	if err := app.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}

	// tell Caddyfile adapter that this is the JSON for an app
	return httpcaddyfile.App{
		Name:  "grpc",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

// Interface guards
var (
	_ caddy.Module = (*Grpc)(nil)
	_ caddy.App    = (*Grpc)(nil)
)
