package grpc

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
)

const (
	// gRPC-Web Content-Type prefixes
	grpcWebContentType     = "application/grpc-web"
	grpcWebTextContentType = "application/grpc-web-text"

	// gRPC-Web Content-Type with proto subtype
	grpcWebProtoContentType     = "application/grpc-web+proto"
	grpcWebTextProtoContentType = "application/grpc-web-text+proto"

	// Native gRPC Content-Type
	grpcContentType = "application/grpc"

	// Frame type constants as per the gRPC-Web protocol
	grpcDataFrame    byte = 0x00
	grpcTrailerFrame byte = 0x80 // The MSB is set to 1
)

// grpcWebFrameReader translates a gRPC-Web request body into a standard gRPC request body.
// It reads gRPC-Web frames and outputs gRPC-style length-prefixed messages.
type grpcWebFrameReader struct {
	source io.Reader
	buffer bytes.Buffer
}

func newGrpcWebFrameReader(r io.Reader, isTextEncoded bool) io.Reader {
	var bodyReader io.Reader = r
	if isTextEncoded {
		bodyReader = base64.NewDecoder(base64.StdEncoding, bodyReader)
	}

	return &grpcWebFrameReader{
		source: bodyReader,
	}
}

func (fr *grpcWebFrameReader) Read(p []byte) (n int, err error) {
	// If we have data in the buffer from a previous read, serve it first.
	if fr.buffer.Len() > 0 {
		return fr.buffer.Read(p)
	}

	// Read the gRPC-Web 5-byte frame header.
	frameHeader := make([]byte, 5)
	if _, err := io.ReadFull(fr.source, frameHeader); err != nil {
		return 0, err
	}

	// The first byte of the header is the frame type.
	// As per the spec, the client should only send DATA frames (0x00).
	// Trailer frames (0x80) are not sent from the client.
	if frameHeader[0] != grpcDataFrame {
		// We've likely hit the end of the data frames.
		return 0, io.EOF
	}

	// The next 4 bytes are the length of the message.
	length := binary.BigEndian.Uint32(frameHeader[1:])
	if length == 0 {
		return 0, nil
	}

	// Translate to gRPC's 5-byte message header: 1 byte for compression flag (0) + 4 bytes for length.
	grpcHeader := make([]byte, 5)
	grpcHeader[0] = 0 // No compression (for now ?)
	binary.BigEndian.PutUint32(grpcHeader[1:], length)
	fr.buffer.Write(grpcHeader)

	// Copy the message payload from the source reader into our buffer.
	if _, err := io.CopyN(&fr.buffer, fr.source, int64(length)); err != nil {
		return 0, fmt.Errorf("copying frame data: %w", err)
	}

	// Now that the buffer is filled with the gRPC-framed message, read it into p.
	return fr.buffer.Read(p)
}

// streamingResponseWriter translates a gRPC response stream into a gRPC-Web response stream.
// It implements http.ResponseWriter and captures the gRPC response, re-framing it for gRPC-Web.
type streamingResponseWriter struct {
	w                  http.ResponseWriter
	logger             *zap.Logger
	isTextResponse     bool
	headers            http.Header
	trailers           http.Header
	capturedStatusCode int
	headersWritten     bool
	bodyWriter         io.Writer
	flusher            http.Flusher
	frameBuffer        *bytes.Buffer
}

func newStreamingResponseWriter(w http.ResponseWriter, isText bool, logger *zap.Logger) *streamingResponseWriter {
	srw := &streamingResponseWriter{
		w:                  w,
		logger:             logger,
		isTextResponse:     isText,
		headers:            make(http.Header),
		trailers:           make(http.Header),
		capturedStatusCode: http.StatusOK,
		frameBuffer:        &bytes.Buffer{},
	}

	var writer io.Writer = w
	if isText {
		writer = newFlushingBase64Writer(w)
	}
	srw.bodyWriter = writer

	if flusher, ok := w.(http.Flusher); ok {
		srw.flusher = flusher
	}

	return srw
}

func (w *streamingResponseWriter) Header() http.Header {
	return w.headers
}

func (w *streamingResponseWriter) WriteHeader(statusCode int) {
	if w.headersWritten {
		return
	}
	w.capturedStatusCode = statusCode
}

// writeHeaders prepares and writes the HTTP response headers.
// In gRPC, headers are sent before the first message.
func (w *streamingResponseWriter) writeHeaders() {
	if w.headersWritten {
		return
	}
	w.headersWritten = true

	// The gRPC server might announce trailers using the "Trailer" header.
	// We need to move these announced headers from the header map to our trailer map.
	if trailers, ok := w.headers["Trailer"]; ok {
		for _, trailer := range trailers {
			for _, key := range strings.Split(trailer, ",") {
				canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
				// Move the value from headers to trailers.
				if val, ok := w.headers[canonicalKey]; ok {
					w.trailers[canonicalKey] = val
					delete(w.headers, canonicalKey)
				}
			}
		}
	}
	delete(w.headers, "Trailer")

	// Copy remaining headers to the underlying ResponseWriter.
	for k, v := range w.headers {
		// These are handled by the gRPC-Web protocol framing, not by HTTP headers.
		if k != "Content-Type" && k != "Content-Length" {
			w.w.Header()[k] = v
		}
	}

	// Set gRPC-Web specific headers.
	finalContentType := grpcWebProtoContentType
	if w.isTextResponse {
		finalContentType = grpcWebTextProtoContentType
	}
	w.w.Header().Set("Content-Type", finalContentType)
	w.w.Header().Add("Access-Control-Expose-Headers", "grpc-status, grpc-message")

	// gRPC-Web always returns 200 OK for the HTTP status.
	// The real gRPC status is sent in the trailer frame.
	w.w.WriteHeader(http.StatusOK)
}

// Write accepts a gRPC message frame and translates it into a gRPC-Web message frame.
func (w *streamingResponseWriter) Write(p []byte) (int, error) {
	if !w.headersWritten {
		w.writeHeaders()
	}

	w.frameBuffer.Write(p)

	// Process all complete gRPC frames in the buffer. A gRPC frame is 5 bytes header + N bytes payload.
	for w.frameBuffer.Len() >= 5 {
		grpcHeader := w.frameBuffer.Bytes()[:5]
		length := binary.BigEndian.Uint32(grpcHeader[1:5])

		// Break if the full message payload isn't in the buffer yet.
		if uint32(w.frameBuffer.Len()) < 5+length {
			break
		}

		// Consume the 5-byte gRPC header.
		w.frameBuffer.Next(5)

		// Construct the 5-byte gRPC-Web DATA frame header (0x00).
		grpcWebFrameHeader := make([]byte, 5)
		grpcWebFrameHeader[0] = grpcDataFrame
		binary.BigEndian.PutUint32(grpcWebFrameHeader[1:5], length)

		if _, err := w.bodyWriter.Write(grpcWebFrameHeader); err != nil {
			return 0, err
		}

		// Write the message payload.
		if _, err := io.CopyN(w.bodyWriter, w.frameBuffer, int64(length)); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func (w *streamingResponseWriter) Flush() {
	if !w.headersWritten {
		w.writeHeaders()
	}
	if w.flusher != nil {
		w.flusher.Flush()
	}
}

// finish constructs and sends the final trailer frame. This must be called to complete the response.
func (w *streamingResponseWriter) finish() error {
	if !w.headersWritten {
		w.writeHeaders()
	}

	// If the gRPC server didn't provide a gRPC status, derive one from the HTTP status code.
	if w.trailers.Get("Grpc-Status") == "" {
		grpcCode := httpStatusToGrpcCode(w.capturedStatusCode)
		w.trailers.Set("Grpc-Status", strconv.Itoa(int(grpcCode)))
	}
	if w.trailers.Get("Grpc-Message") == "" && w.capturedStatusCode != http.StatusOK {
		w.trailers.Set("Grpc-Message", http.StatusText(w.capturedStatusCode))
	}

	// Serialize trailers into a buffer. The format is a standard HTTP/1 header block.
	var trailerBuf bytes.Buffer
	if err := w.trailers.Write(&trailerBuf); err != nil {
		return fmt.Errorf("failed to serialize trailers: %w", err)
	}

	// Construct the 5-byte gRPC-Web TRAILER frame header.
	// As per the spec, the MSB of the first byte is set to 1.
	frameHeader := make([]byte, 5)
	frameHeader[0] = grpcTrailerFrame
	binary.BigEndian.PutUint32(frameHeader[1:], uint32(trailerBuf.Len()))

	// Write the trailer frame header and payload.
	if _, err := w.bodyWriter.Write(frameHeader); err != nil {
		return fmt.Errorf("failed to write trailer frame header: %w", err)
	}
	if _, err := w.bodyWriter.Write(trailerBuf.Bytes()); err != nil {
		return fmt.Errorf("failed to write trailer frame payload: %w", err)
	}

	// If the body writer (e.g., the base64 encoder) needs to be closed, do it now.
	if closer, ok := w.bodyWriter.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("failed closing body writer: %w", err)
		}
	}

	w.Flush()
	return nil
}

// httpStatusToGrpcCode maps HTTP status codes to gRPC codes. This is a fallback mechanism.
func httpStatusToGrpcCode(httpStatusCode int) codes.Code {
	switch httpStatusCode {
	case http.StatusOK:
		return codes.OK
	case http.StatusBadRequest:
		return codes.Internal
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.Unimplemented
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return codes.Unavailable
	default:
		return codes.Unknown
	}
}

// flushingBase64Writer is a base64 encoder that supports http.Flusher.
type flushingBase64Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	encoder io.WriteCloser
}

func newFlushingBase64Writer(w http.ResponseWriter) *flushingBase64Writer {
	flusher, _ := w.(http.Flusher)
	return &flushingBase64Writer{
		w:       w,
		flusher: flusher,
		encoder: base64.NewEncoder(base64.StdEncoding, w),
	}
}

func (fbw *flushingBase64Writer) Write(p []byte) (int, error) {
	return fbw.encoder.Write(p)
}

func (fbw *flushingBase64Writer) Flush() {
	if fbw.flusher != nil {
		fbw.flusher.Flush()
	}
}

func (fbw *flushingBase64Writer) Close() error {
	return fbw.encoder.Close()
}

// Interface guards
var (
	_ http.ResponseWriter = (*streamingResponseWriter)(nil)
	_ http.Flusher        = (*streamingResponseWriter)(nil)
	_ io.Reader           = (*grpcWebFrameReader)(nil)
	_ io.WriteCloser      = (*flushingBase64Writer)(nil)
	_ http.Flusher        = (*flushingBase64Writer)(nil)
)
