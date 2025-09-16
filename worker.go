package grpc

import (
	"net/http"
	"net/url"

	"github.com/dunglas/frankenphp"
)

var w = &worker{
	messages: make(chan message),
}

func init() {
}

type worker struct {
	messages chan message
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

func (w *worker) ProvideRequest() *frankenphp.WorkerRequest[any, any] {
	m := <-w.messages

	return &frankenphp.WorkerRequest[any, any]{
		Request:            &http.Request{URL: u},
		CallbackParameters: m.request,
		AfterFunc: func(callbackReturn any) {
			m.responseChan <- callbackReturn
		},
	}
}
