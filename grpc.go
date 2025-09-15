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

func HandleRequest(request map[string]any) map[string]any {
	responseChan := make(chan map[string]any)

	w.handles <- cgo.NewHandle(message{
		request:      request,
		responseChan: responseChan,
	})

	return <-responseChan
}

type message struct {
	runtime.Pinner

	request      map[string]any
	responseChan chan map[string]any
}

//export go_get_request
func go_get_request(handle unsafe.Pointer) unsafe.Pointer {
	hStr := frankenphp.GoString(handle)
	hUint, _ := strconv.ParseUint(hStr, 10, 64)

	m := cgo.Handle(hUint).Value().(message)

	mp := frankenphp.PHPMap(m.request)
	m.Pin(mp)

	return mp
}

//export go_send_response
func go_send_response(handle unsafe.Pointer, response unsafe.Pointer) {
	hStr := frankenphp.GoString(handle)
	hUint, _ := strconv.ParseUint(hStr, 10, 64)

	m := cgo.Handle(hUint).Value().(message)
	m.Unpin()

	m.responseChan <- frankenphp.GoMap(response)
}
