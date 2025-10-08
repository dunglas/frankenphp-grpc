package grpc

import (
	"net/http"
	"net/url"

	"github.com/dunglas/frankenphp"
)

var u = &url.URL{Host: "grpc.alt", Path: "/grpc"}

func HandleRequest(request any) any {
	responseChan := make(chan any)

	w.InjectRequest(&frankenphp.WorkerRequest{
		Request:            &http.Request{URL: u},
		CallbackParameters: request,
		AfterFunc: func(callbackReturn any) {
			responseChan <- callbackReturn
		},
	})

	return <-responseChan
}
