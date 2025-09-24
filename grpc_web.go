package grpc

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dunglas/frankenphp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("grpc", parseHandlerDirective)
}

// Handler is an HTTP handler that translates gRPC-Web requests to gRPC.
type Handler struct {
	MinThreads int    `json:"min_threads,omitempty"`
	Worker     string `json:"worker,omitempty"`

	logger *zap.Logger
	srv    *grpc.Server
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.grpc",
		New: func() caddy.Module { return new(Handler) },
	}
}

func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()

	if h.MinThreads <= 0 {
		h.MinThreads = runtime.NumCPU()
	}

	if h.Worker == "" {
		h.Worker = "grpc-worker.php"
	}

	// This part seems specific to a frankenphp setup.
	// Assuming `w` and `grpcServerFactory` are defined elsewhere in your project.
	w.minThread = h.MinThreads
	w.filename = h.Worker
	frankenphp.RegisterExternalWorker(w)
	if grpcServerFactory == nil {
		return fmt.Errorf("no gRPC server factory registered")
	}
	h.srv = grpcServerFactory()

	return nil
}

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "worker":
				if !d.NextArg() {
					return d.ArgErr()
				}

				h.Worker = d.Val()
			case "min_threads":
				if !d.NextArg() {
					return d.ArgErr()
				}

				t, err := strconv.Atoi(d.Val())
				if err != nil {
					return err
				}
				h.MinThreads = t
			default:
				return fmt.Errorf(`unrecognized subdirective "%s"`, d.Val())
			}
		}
	}

	return nil
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	contentType := r.Header.Get("Content-Type")
	isGrpcWebRequest := strings.HasPrefix(contentType, "application/grpc-web")

	if !isGrpcWebRequest {
		return next.ServeHTTP(w, r)
	}

	h.logger.Debug("handling gRPC-Web request",
		zap.String("uri", r.RequestURI),
		zap.String("content-type", contentType),
	)

	grpcRequest, err := h.newGrpcRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to transform grpc-web request: %v", err), http.StatusBadRequest)
		return nil
	}

	wrappedWriter := &grpcWebResponseWriter{
		w:       w,
		body:    &bytes.Buffer{},
		headers: make(http.Header),
	}
	h.srv.ServeHTTP(wrappedWriter, grpcRequest)

	h.logger.Debug("gRPC server finished handling request",
		zap.Int("status_code", wrappedWriter.statusCode),
		zap.Any("headers", wrappedWriter.headers),
		zap.Int("body_size", wrappedWriter.body.Len()),
	)

	isTextResponse := strings.Contains(r.Header.Get("Accept"), "application/grpc-web-text") || strings.HasSuffix(contentType, "-text")
	finalContentType := "application/grpc-web+proto"
	if isTextResponse {
		finalContentType = "application/grpc-web-text+proto"
	}

	// Prepare the final response body by translating the gRPC response to gRPC-Web frames.
	var responseBody bytes.Buffer
	if err := h.transformGrpcToGrpcWebBody(&responseBody, wrappedWriter.body); err != nil {
		h.logger.Error("failed to transform gRPC response body", zap.Error(err))
		// We can't write an http error now as the connection state is uncertain.
		return nil
	}

	// Append the trailer frame to the response body.
	if err := h.writeGrpcWebTrailers(&responseBody, wrappedWriter.headers); err != nil {
		h.logger.Error("failed to write gRPC-Web trailers", zap.Error(err))
		return nil
	}

	// Get the final bytes that will be sent on the wire.
	finalBodyBytes := responseBody.Bytes()
	if isTextResponse {
		h.logger.Debug("encoding response to base64 grpc-web-text")
		encoded := base64.StdEncoding.EncodeToString(finalBodyBytes)
		finalBodyBytes = []byte(encoded)
	}

	// Now that we have the final body, set the headers.
	finalHeaders := w.Header()
	finalHeaders.Set("Content-Type", finalContentType)
	finalHeaders.Set("Content-Length", strconv.Itoa(len(finalBodyBytes)))

	// Copy over non-gRPC headers from the response.
	for key, values := range wrappedWriter.headers {
		lowerKey := strings.ToLower(key)
		if lowerKey == "content-type" || lowerKey == "content-length" {
			continue // We've already set these.
		}
		finalHeaders[key] = values
	}

	// grpc-web requires this header to be exposed for the browser client to read it.
	finalHeaders.Add("Access-Control-Expose-Headers", "grpc-status, grpc-message")

	// Write the status header and the final body.
	w.WriteHeader(http.StatusOK)
	w.Write(finalBodyBytes)

	return nil
}

func (h Handler) newGrpcRequest(r *http.Request) (*http.Request, error) {
	var bodyReader io.Reader = r.Body

	if strings.HasSuffix(r.Header.Get("Content-Type"), "-text") {
		h.logger.Debug("decoding base64 grpc-web-text request body")
		bodyReader = base64.NewDecoder(base64.StdEncoding, bodyReader)
	}

	requestBody, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}
	r.Body.Close()

	if len(requestBody) == 0 {
		// This is a request with no body, like an empty proto.
		grpcRequest := r.Clone(r.Context())
		grpcRequest.Body = http.NoBody
		grpcRequest.ContentLength = 0
		grpcRequest.Header.Set("Content-Type", "application/grpc")
		return grpcRequest, nil
	}

	// gRPC-Web frames the body. We need to un-frame it for native gRPC.
	if len(requestBody) < 5 {
		return nil, fmt.Errorf("invalid grpc-web request body: too short")
	}

	var nativeBody bytes.Buffer
	frameReader := bytes.NewReader(requestBody)

	for frameReader.Len() > 0 {
		frameHeader := make([]byte, 5)
		if _, err := io.ReadFull(frameReader, frameHeader); err != nil {
			return nil, fmt.Errorf("reading frame header: %w", err)
		}

		// First byte is frame type, 0x00 for data. We ignore other frame types.
		if frameHeader[0] == 0x00 {
			length := binary.BigEndian.Uint32(frameHeader[1:])

			// Re-construct the gRPC header (compression + length)
			grpcHeader := make([]byte, 5)
			grpcHeader[0] = 0 // No compression
			binary.BigEndian.PutUint32(grpcHeader[1:], length)
			nativeBody.Write(grpcHeader)

			if _, err := io.CopyN(&nativeBody, frameReader, int64(length)); err != nil {
				return nil, fmt.Errorf("copying frame data: %w", err)
			}
		}
	}

	grpcRequest := r.Clone(r.Context())
	grpcRequest.Body = io.NopCloser(&nativeBody)
	grpcRequest.ContentLength = int64(nativeBody.Len())
	grpcRequest.Header.Set("Content-Type", "application/grpc")
	grpcRequest.Header.Del("Content-Length")

	return grpcRequest, nil
}

func (h Handler) transformGrpcToGrpcWebBody(w io.Writer, grpcBody io.Reader) error {
	bodyBytes, err := io.ReadAll(grpcBody)
	if err != nil {
		return fmt.Errorf("reading grpc response body: %w", err)
	}

	if len(bodyBytes) == 0 {
		return nil
	}

	if len(bodyBytes) < 5 {
		return fmt.Errorf("invalid grpc response body: too short")
	}
	// The gRPC message is prefixed with 1 byte for compression and 4 for length.
	length := binary.BigEndian.Uint32(bodyBytes[1:5])
	messageBytes := bodyBytes[5:]

	if uint32(len(messageBytes)) != length {
		return fmt.Errorf("grpc message length mismatch: header says %d, but actual is %d", length, len(messageBytes))
	}

	// Write the gRPC-Web data frame header (0x00, 4-byte length).
	webHeader := make([]byte, 5)
	webHeader[0] = 0x00 // Data frame
	binary.BigEndian.PutUint32(webHeader[1:], length)
	if _, err := w.Write(webHeader); err != nil {
		return fmt.Errorf("writing grpc-web header: %w", err)
	}

	if _, err := w.Write(messageBytes); err != nil {
		return fmt.Errorf("writing grpc-web payload: %w", err)
	}

	return nil
}

func (h Handler) writeGrpcWebTrailers(w io.Writer, headers http.Header) error {
	var trailers bytes.Buffer

	statusVal := headers.Get("Grpc-Status")
	if statusVal == "" {
		// If the gRPC server returns no error, it doesn't set a status header.
		// For gRPC-Web, a trailer frame is required, so we explicitly set it to 0 (OK).
		statusVal = "0"
	}
	trailers.WriteString(fmt.Sprintf("grpc-status:%s\r\n", statusVal))

	if msg := headers.Get("Grpc-Message"); msg != "" {
		trailers.WriteString(fmt.Sprintf("grpc-message:%s\r\n", msg))
	}

	// Write the gRPC-Web trailer frame header (0x80, 4-byte length).
	webHeader := make([]byte, 5)
	webHeader[0] = 0x80 // Trailer frame (MSB set)
	binary.BigEndian.PutUint32(webHeader[1:], uint32(trailers.Len()))
	if _, err := w.Write(webHeader); err != nil {
		return fmt.Errorf("writing grpc-web trailer header: %w", err)
	}

	if _, err := w.Write(trailers.Bytes()); err != nil {
		return fmt.Errorf("writing grpc-web trailer payload: %w", err)
	}

	return nil
}

// grpcWebResponseWriter is a wrapper to buffer the response from the gRPC server.
type grpcWebResponseWriter struct {
	w          http.ResponseWriter
	body       *bytes.Buffer
	headers    http.Header
	statusCode int
}

func (w *grpcWebResponseWriter) Header() http.Header {
	return w.headers
}

func (w *grpcWebResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

func (w *grpcWebResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	// We buffer the response, so we don't write headers to the underlying writer yet.
}

func (w *grpcWebResponseWriter) Flush() {
	// This is a no-op because we buffer the entire response before writing.
}

func parseHandlerDirective(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	handler := &Handler{}
	if err := handler.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return handler, nil
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ http.ResponseWriter         = (*grpcWebResponseWriter)(nil)
)

