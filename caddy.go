package grpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dunglas/frankenphp"
	"github.com/soyuka/grpcweb"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func init() {
	caddy.RegisterModule(Grpc{})
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterGlobalOption("grpc", parseGlobalOption)
	httpcaddyfile.RegisterHandlerDirective("grpc", parseGRPCHandler)
}

var grpcServerFactory func() *grpc.Server

func RegisterGrpcServerFactory(f func() *grpc.Server) {
	grpcServerFactory = f
}

type Grpc struct {
	Address    string `json:"address,omitempty"`
	MinThreads int    `json:"min_threads,omitempty"`
	Worker     string `json:"worker,omitempty"`

	ctx     caddy.Context
	logger  *zap.Logger
	httpSrv *http.Server

	// webHandler holds the wrapper that handles both gRPC and gRPC-Web requests.
	webHandler http.Handler
}

// CaddyModule returns the Caddy module information.
func (Grpc) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "grpc",
		New: func() caddy.Module { return new(Grpc) },
	}
}

// Provision sets up the gRPC app module.
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

// Start creates the gRPC server instance and starts it listening on the configured address.
func (g *Grpc) Start() error {
	if grpcServerFactory == nil {
		return fmt.Errorf("no gRPC server factory registered")
	}

	// Create the raw gRPC server.
	grpcServer := grpcServerFactory()

	// Wrap the gRPC server with the gRPC-Web handler and store it.
	g.webHandler = &grpcweb.Handler{GRPCServer: grpcServer}

	address, err := caddy.ParseNetworkAddress(g.Address)
	if err != nil {
		return err
	}

	lnAny, err := address.Listen(g.ctx, 0, net.ListenConfig{})
	if err != nil {
		return err
	}

	ln := lnAny.(net.Listener)

	// The background server uses the same web handler.
	g.httpSrv = &http.Server{Handler: g.webHandler}

	go func() {
		g.logger.Info("starting gRPC/gRPC-Web server", zap.String("address", g.Address))
		if err := g.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			g.logger.Error("gRPC/gRPC-Web server failed", zap.Error(err))
		}
	}()

	return nil
}

func (g *Grpc) Stop() error {
	if g.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := g.httpSrv.Shutdown(ctx); err != nil {
			g.logger.Error("error shutting down gRPC/gRPC-Web server", zap.Error(err))
		}
		g.httpSrv = nil
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
					return d.Errf("invalid value for min_threads: %v", err)
				}
				g.MinThreads = t
			default:
				return d.Errf(`unrecognized subdirective "%s"`, d.Val())
			}
		}
	}
	return nil
}

// Handler is the Caddy HTTP middleware that handles gRPC requests in-process.
type Handler struct {
	app *Grpc
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.grpc",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision gets a reference to the gRPC app.
func (h *Handler) Provision(ctx caddy.Context) error {
	grpcAppIface, err := ctx.App("grpc")
	if err != nil {
		return fmt.Errorf(`unable to get the "grpc" app: %v, make sure "grpc" is configured in global options`, err)
	}
	h.app = grpcAppIface.(*Grpc)
	return nil
}

// ServeHTTP delegates gRPC and gRPC-Web requests to the in-process web handler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	contentType := r.Header.Get("Content-Type")

	// Check if the request is potentially for our gRPC server.
	if r.Method == http.MethodPost && strings.HasPrefix(contentType, "application/grpc") {
		// Delegate to the webHandler, which correctly handles both
		// native gRPC and gRPC-Web requests.
		h.app.webHandler.ServeHTTP(w, r)
		return nil
	}

	// Pass non-gRPC requests to the next handler.
	return next.ServeHTTP(w, r)
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

func parseGRPCHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	h.Dispenser.Next() // consume "grpc"

	// This directive does not take any arguments
	if h.Dispenser.NextArg() {
		return nil, h.Dispenser.ArgErr()
	}

	return new(Handler), nil
}

// Interface guards
var (
	_ caddy.Module                = (*Grpc)(nil)
	_ caddy.App                   = (*Grpc)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
)

