package grpc

import (
	"fmt"
	"net"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(Grpc{})
	httpcaddyfile.RegisterGlobalOption("grpc", parseGlobalOption)
}

// Grpc is a Caddy app that runs a standalone, native gRPC server.
// It embeds grpcApp to share worker and server configuration.
type Grpc struct {
	grpcApp

	// The address to listen on for native gRPC connections.
	Address string `json:"address,omitempty"`

	ctx caddy.Context
}

// CaddyModule returns the Caddy module information.
func (Grpc) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "grpc",
		New: func() caddy.Module { return new(Grpc) },
	}
}

// Provision sets up the gRPC app. It calls the embedded grpcApp's
// Provision method to handle the common setup.
func (g *Grpc) Provision(ctx caddy.Context) error {
	g.ctx = ctx
	if g.Address == "" {
		g.Address = ":50051" // Default gRPC port
	}

	// Provision the shared gRPC server and worker
	return g.grpcApp.Provision(ctx)
}

// Start runs the native gRPC server in a goroutine.
func (g *Grpc) Start() error {
	address, err := caddy.ParseNetworkAddress(g.Address)
	if err != nil {
		return fmt.Errorf("parsing gRPC address '%s': %w", g.Address, err)
	}

	lnAny, err := address.Listen(g.ctx, 0, net.ListenConfig{})
	if err != nil {
		return fmt.Errorf("listening on gRPC address '%s': %w", g.Address, err)
	}
	ln := lnAny.(net.Listener)

	go func() {
		g.logger.Info("starting native gRPC server", zap.String("address", g.Address))
		if err := g.srv.Serve(ln); err != nil {
			g.logger.Error("native gRPC server failed", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully stops the native gRPC server.
func (g *Grpc) Stop() error {
	if g.srv != nil {
		g.logger.Info("stopping native gRPC server")
		g.srv.GracefulStop()
		g.srv = nil
	}
	return nil
}

// UnmarshalCaddyfile parses the `grpc` global option from the Caddyfile.
func (g *Grpc) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		// First, parse app-specific directives
		if d.Val() == "grpc" { // expecting `grpc { ... }`
			for d.NextBlock(0) {
				switch d.Val() {
				case "address":
					if !d.NextArg() {
						return d.ArgErr()
					}
					g.Address = d.Val()
				// Handle common directives directly
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
						return err
					}
					g.MinThreads = t
				default:
					return d.Errf("unrecognized gRPC app directive '%s'", d.Val())
				}
			}
		}
	}
	return nil
}

// parseGlobalOption configures the gRPC app from a Caddyfile.
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
	_ caddy.Module      = (*Grpc)(nil)
	_ caddy.App         = (*Grpc)(nil)
	_ caddy.Provisioner = (*Grpc)(nil)
)
