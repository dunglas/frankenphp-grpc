package grpc

import (
	"fmt"
	"net"
	"runtime"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/dunglas/frankenphp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func init() {
	caddy.RegisterModule(Grpc{})
	httpcaddyfile.RegisterGlobalOption("grpc", parseGlobalOption)
}

var grpcServerFactory func() *grpc.Server

func RegisterGrpcServerFactory(f func() *grpc.Server) {
	grpcServerFactory = f
}

type Grpc struct {
	Address    string `json:"address,omitempty"`
	MinThreads int    `json:"min_threads,omitempty"`
	Worker     string `json:"worker,omitempty"`

	ctx    caddy.Context
	logger *zap.Logger
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
	g.logger = ctx.Logger()
	g.ctx = ctx

	if g.Address == "" {
		g.Address = ":50051"
	}

	if g.MinThreads <= 0 {
		g.MinThreads = runtime.NumCPU()
	}

	if g.Worker == "" {
		g.Worker = "grpc-worker.php"
	}

	w.minThread = g.MinThreads
	w.filename = g.Worker

	frankenphp.RegisterExternalWorker(w)

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
			g.logger.Panic("failed to start gRPC server", zap.Error(err))
		}
	}()

	g.logger.Info("gRPC server started", zap.String("address", g.Address))

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

				g.Worker = d.Val()
			case "min_threads":
				if !d.NextArg() {
					return d.ArgErr()
				}

				t, err := strconv.Atoi(d.Val())
				if err != nil {
					return nil
				}
				g.MinThreads = t
			default:
				return fmt.Errorf(`unrecognized subdirective "%s"`, d.Val())
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
