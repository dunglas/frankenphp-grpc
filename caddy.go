package grpc

import (
	"fmt"
	"net"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func init() {
	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("grpc", parseGlobalOption)
}

// App is a Caddy app that runs a standalone, native gRPC server.
type App struct {
	Address string `json:"address,omitempty"`

	ctx    caddy.Context
	logger *zap.Logger
	srv    *grpc.Server
}

// CaddyModule returns the Caddy module information.
func (a App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "grpc",
		New: func() caddy.Module { return new(App) },
	}
}

func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()
	a.ctx = ctx

	if a.Address == "" {
		a.Address = ":50051"
	}

	if grpcServerFactory == nil {
		return fmt.Errorf("no gRPC server factory registered")
	}
	a.srv = grpcServerFactory()

	return nil
}

func (a App) Start() error {
	address, err := caddy.ParseNetworkAddress(a.Address)
	if err != nil {
		return err
	}

	lnAny, err := address.Listen(a.ctx, 0, net.ListenConfig{})
	if err != nil {
		return err
	}

	ln := lnAny.(net.Listener)

	go func() {
		if err := a.srv.Serve(ln); err != nil {
			a.logger.Error("failed to start gRPC server", zap.Error(err))
		}
	}()

	a.logger.Info("gRPC server started", zap.String("address", a.Address))

	return nil
}

func (a App) Stop() error {
	if a.srv != nil {
		a.srv.GracefulStop()
		a.srv = nil
	}
	return nil
}

func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		// a worker and min_threads could be defined for the gRPC app
		// but it would require to change the worker system to support multiple workers
		// which is not the case for now
		for d.NextBlock(0) {
			switch d.Val() {
			case "address":
				if !d.NextArg() {
					return d.ArgErr()
				}
				a.Address = d.Val()

			default:
				return fmt.Errorf(`unrecognized subdirective "%s"`, d.Val())
			}
		}
	}
	return nil
}

func parseGlobalOption(d *caddyfile.Dispenser, _ any) (any, error) {
	app := &App{}
	if err := app.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}

	return httpcaddyfile.App{
		Name:  "grpc",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

// Interface guards
var (
	_ caddy.Module          = (*App)(nil)
	_ caddy.App             = (*App)(nil)
	_ caddyfile.Unmarshaler = (*App)(nil)
)


