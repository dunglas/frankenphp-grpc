package grpc

//#include "grpc.h"
import "C"
import (
	"runtime"
	"runtime/cgo"
	"strconv"
	"unsafe"

	"github.com/dunglas/frankenphp"
)

func init() {
	frankenphp.RegisterExtension(unsafe.Pointer(&C.ext_module_entry))
}

func HandleRequest(request any) any {
	responseChan := make(chan any)

	w.handles <- cgo.NewHandle(message{
		request:      request,
		responseChan: responseChan,
		pinner:       &runtime.Pinner{},
	})

	return <-responseChan
}

type message struct {
	pinner *runtime.Pinner

	request      any
	responseChan chan any
}

//export go_get_request
func go_get_request(handle unsafe.Pointer) unsafe.Pointer {
	hStr := frankenphp.GoString(handle)
	hUint, _ := strconv.ParseUint(hStr, 10, 64)

	m := cgo.Handle(hUint).Value().(message)

	mp := frankenphp.PHPValue(m.request)
	m.pinner.Pin(mp)

	return mp
}

//export go_send_response
func go_send_response(handle unsafe.Pointer, response unsafe.Pointer) {
	hStr := frankenphp.GoString(handle)
	hUint, _ := strconv.ParseUint(hStr, 10, 64)

	m := cgo.Handle(hUint).Value().(message)
	m.pinner.Unpin()

	m.responseChan <- frankenphp.GoValue(response)
}
