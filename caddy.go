package grpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	srv     *grpc.Server
	httpSrv *http.Server
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

func (g *Grpc) Start() error {
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
	// The grpcweb handler wraps the gRPC server. It proxies gRPC-Web requests
	// and falls back to the underlying gRPC server for native gRPC requests.
	webHandler := &grpcweb.Handler{GRPCServer: g.srv}

	g.httpSrv = &http.Server{Handler: webHandler}

	go func() {
		g.logger.Info("starting gRPC/gRPC-Web server", zap.String("address", g.Address))
		if err := g.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			g.logger.Error("gRPC/gRPC-Web server failed", zap.Error(err))
		}
	}()

	return nil
}

func (g *Grpc) Stop() error {
	if g.srv != nil {
		g.srv.GracefulStop()
		g.srv = nil
	}

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

type Handler struct {
	proxy *httputil.ReverseProxy
	app   *Grpc
}

func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.grpc",
		New: func() caddy.Module { return new(Handler) },
	}
}

func (h *Handler) Provision(ctx caddy.Context) error {
	grpcAppIface, err := ctx.App("grpc")
	if err != nil {
		return fmt.Errorf("getting grpc app: %v. make sure 'grpc' is configured in global options", err)
	}
	h.app = grpcAppIface.(*Grpc)

	addr, err := net.ResolveTCPAddr("tcp", h.app.Address)
	if err != nil {
		return fmt.Errorf("could not resolve grpc app address '%s': %w", h.app.Address, err)
	}

	host := "127.0.0.1"
	if addr.IP != nil && !addr.IP.IsUnspecified() {
		host = addr.IP.String()
	}

	target, err := url.Parse("http://" + net.JoinHostPort(host, strconv.Itoa(addr.Port)))
	if err != nil {
		return fmt.Errorf("invalid grpc upstream URL: %w", err)
	}

	h.proxy = httputil.NewSingleHostReverseProxy(target)

	return nil
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	contentType := r.Header.Get("Content-Type")

	// This handler is only for gRPC requests.
	// We check for both gRPC-Web and native gRPC content types.
	// If it's not a gRPC request, we pass it to the next handler in the chain.
	isGrpcRequest := r.Method == http.MethodPost &&
		(strings.HasPrefix(contentType, "application/grpc-web") || strings.HasPrefix(contentType, "application/grpc"))

	if isGrpcRequest {
		h.proxy.ServeHTTP(w, r)
		return nil
	}

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


