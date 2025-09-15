package grpc

import "C"
import (
	"net/http"
	"net/url"
	"runtime/cgo"
	"strconv"

	"github.com/dunglas/frankenphp"
)

var w = &worker{
	handles: make(chan cgo.Handle),
}

func init() {
	frankenphp.RegisterExternalWorker(w)
}

type worker struct {
	handles chan cgo.Handle
}

func (w *worker) Name() string {
	return "m#Grpc"
}

func (w *worker) FileName() string {
	return "grpc-worker.php"
}

func (w *worker) GetMinThreads() int {
	return 1
}

func (w *worker) ThreadActivatedNotification(int)   {}
func (w *worker) ThreadDrainNotification(int)       {}
func (w *worker) ThreadDeactivatedNotification(int) {}
func (w *worker) Env() frankenphp.PreparedEnv {
	return frankenphp.PreparedEnv{}
}

var u = &url.URL{Host: "grpc.alt", Path: "/grpc"}

func (w *worker) ProvideRequest() *frankenphp.WorkerRequest {
	h := <-w.handles
	id := strconv.FormatUint(uint64(h), 10)

	return &frankenphp.WorkerRequest{
		Request: &http.Request{
			Method: http.MethodPost,
			URL:    u,
			Header: http.Header{"ID": []string{id}},
		},
	}
}
