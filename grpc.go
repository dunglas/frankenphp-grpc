package grpc

func HandleRequest(request any) any {
	responseChan := make(chan any)

	w.messages <- message{
		request:      request,
		responseChan: responseChan,
	}

	return <-responseChan
}

type message struct {
	request      any
	responseChan chan any
}
