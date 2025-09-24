package grpc

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("grpc", parseHandlerDirective)
}

// Handler is an HTTP handler that translates gRPC-Web requests to gRPC.
// It embeds grpcApp to share worker and server configuration.
type Handler struct {
	grpcApp
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.grpc",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision sets up the gRPC handler. It simply calls the embedded
// Provision method.
func (h *Handler) Provision(ctx caddy.Context) error {
	return h.grpcApp.Provision(ctx)
}

// UnmarshalCaddyfile parses the `grpc` directive from a site block.
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	// Let the embedded grpcApp handle parsing its own directives.
	return h.grpcApp.UnmarshalCaddyfile(d)
}

// ServeHTTP handles the gRPC-Web request, translating it for the gRPC server.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, grpcWebContentType) {
		return next.ServeHTTP(w, r)
	}

	h.logger.Debug("handling gRPC-Web request",
		zap.String("uri", r.RequestURI),
		zap.String("content-type", contentType),
	)

	grpcRequest, err := h.newGrpcRequest(r, contentType)
	if err != nil {
		h.writeErrorResponse(w, r, status.New(codes.InvalidArgument, err.Error()))
		return nil // We've handled the response.
	}

	isTextResponse := isGrpcWebTextRequest(r)

	// This response writer will translate the gRPC stream to a gRPC-Web stream on the fly.
	streamingWriter := newStreamingResponseWriter(w, isTextResponse, h.logger)
	defer func() {
		if err := streamingWriter.finish(); err != nil {
			h.logger.Error("failed to finish gRPC-Web stream", zap.Error(err))
		}
	}()

	// Let the gRPC server handle the translated request.
	h.srv.ServeHTTP(streamingWriter, grpcRequest)

	return nil // We have handled the request and response.
}

// writeErrorResponse writes a gRPC-Web formatted error.
func (h Handler) writeErrorResponse(w http.ResponseWriter, r *http.Request, st *status.Status) {
	isTextResponse := isGrpcWebTextRequest(r)
	sw := newStreamingResponseWriter(w, isTextResponse, h.logger)
	sw.trailers.Set("grpc-status", strconv.Itoa(int(st.Code())))
	sw.trailers.Set("grpc-message", st.Message())
	if err := sw.finish(); err != nil {
		h.logger.Error("failed to write gRPC-Web error response", zap.Error(err))
	}
}

// newGrpcRequest transforms an incoming gRPC-Web HTTP request into a native gRPC request.
func (h Handler) newGrpcRequest(r *http.Request, contentType string) (*http.Request, error) {
	isTextEncoded := strings.HasSuffix(contentType, "-text")
	bodyReader := newGrpcWebFrameReader(r.Body, isTextEncoded)

	grpcRequest, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	// Copy relevant headers from the original request.
	for key, values := range r.Header {
		lowerKey := strings.ToLower(key)
		if lowerKey == "user-agent" || lowerKey == "authorization" || strings.HasPrefix(lowerKey, "x-") {
			grpcRequest.Header[key] = values
		}
	}

	// Set headers to make it look like a standard gRPC request.
	grpcRequest.Header.Set("Content-Type", grpcContentType)
	grpcRequest.Header.Set("TE", "trailers")
	grpcRequest.Proto = "HTTP/2.0"
	grpcRequest.ProtoMajor = 2
	grpcRequest.ProtoMinor = 0
	grpcRequest.ContentLength = -1 // Indeterminate length for streaming.

	return grpcRequest, nil
}

// isGrpcWebTextRequest checks if the client expects a base64-encoded text response.
func isGrpcWebTextRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	accept := r.Header.Get("Accept")
	return strings.HasSuffix(contentType, "-text") || strings.Contains(accept, grpcWebTextContentType)
}

// parseHandlerDirective creates the gRPC handler from a Caddyfile.
func parseHandlerDirective(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var handler Handler
	if err := handler.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &handler, nil
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
