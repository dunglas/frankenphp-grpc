package grpc

import (
	"github.com/dunglas/frankenphp"
)

func HandleRequest(request any) any {
	responseChan := make(chan any)

	w.InjectRequest(&frankenphp.WorkerRequest{
		CallbackParameters: request,
		AfterFunc: func(callbackReturn any) {
			responseChan <- callbackReturn
		},
	})

	return <-responseChan
}
