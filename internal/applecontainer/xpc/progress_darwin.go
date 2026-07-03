//go:build darwin && cgo

package xpc

/*
#cgo LDFLAGS: -framework Foundation
#include <xpc/xpc.h>
#include <stdint.h>

xpc_connection_t create_progress_endpoint(uint64_t id, xpc_endpoint_t *endpoint_out);
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

var (
	progressID       atomic.Uint64
	progressHandlers sync.Map
)

type darwinProgressEndpoint struct {
	id       uint64
	listener C.xpc_connection_t
	endpoint C.xpc_endpoint_t
}

func newProgressEndpoint(handler ProgressHandler) (progressEndpoint, error) {
	if handler == nil {
		return nil, nil
	}
	id := progressID.Add(1)
	progressHandlers.Store(id, handler)
	var endpoint C.xpc_endpoint_t
	listener := C.create_progress_endpoint(C.uint64_t(id), &endpoint)
	if listener == nil || endpoint == nil {
		progressHandlers.Delete(id)
		if endpoint != nil {
			C.xpc_release(C.xpc_object_t(endpoint))
		}
		if listener != nil {
			C.xpc_connection_cancel(listener)
			C.xpc_release(C.xpc_object_t(listener))
		}
		return nil, nil
	}
	return &darwinProgressEndpoint{id: id, listener: listener, endpoint: endpoint}, nil
}

func (p *darwinProgressEndpoint) Handle() uintptr {
	if p == nil || p.endpoint == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(p.endpoint))
}

func (p *darwinProgressEndpoint) Close() {
	if p == nil {
		return
	}
	progressHandlers.Delete(p.id)
	if p.listener != nil {
		C.xpc_connection_cancel(p.listener)
		C.xpc_release(C.xpc_object_t(p.listener))
		p.listener = nil
	}
	if p.endpoint != nil {
		C.xpc_release(C.xpc_object_t(p.endpoint))
		p.endpoint = nil
	}
}

//export goHandleProgressUpdate
func goHandleProgressUpdate(id C.uint64_t, object C.xpc_object_t) {
	handlerValue, ok := progressHandlers.Load(uint64(id))
	if !ok {
		return
	}
	handler, ok := handlerValue.(ProgressHandler)
	if !ok || handler == nil {
		return
	}
	message, err := xpcToMessage(object)
	if err != nil {
		return
	}
	handler(progressUpdateFromMessage(message))
}

func progressUpdateFromMessage(message *Message) ProgressUpdate {
	return ProgressUpdate{
		Description:    stringPtrFromMessage(message, XPCKeyProgressUpdateSetDescription),
		SubDescription: stringPtrFromMessage(message, XPCKeyProgressUpdateSetSubDescription),
		ItemsName:      stringPtrFromMessage(message, XPCKeyProgressUpdateSetItemsName),
		AddTasks:       int64PtrFromMessage(message, XPCKeyProgressUpdateAddTasks),
		SetTasks:       int64PtrFromMessage(message, XPCKeyProgressUpdateSetTasks),
		AddTotalTasks:  int64PtrFromMessage(message, XPCKeyProgressUpdateAddTotalTasks),
		SetTotalTasks:  int64PtrFromMessage(message, XPCKeyProgressUpdateSetTotalTasks),
		AddItems:       int64PtrFromMessage(message, XPCKeyProgressUpdateAddItems),
		SetItems:       int64PtrFromMessage(message, XPCKeyProgressUpdateSetItems),
		AddTotalItems:  int64PtrFromMessage(message, XPCKeyProgressUpdateAddTotalItems),
		SetTotalItems:  int64PtrFromMessage(message, XPCKeyProgressUpdateSetTotalItems),
		AddSize:        int64PtrFromMessage(message, XPCKeyProgressUpdateAddSize),
		SetSize:        int64PtrFromMessage(message, XPCKeyProgressUpdateSetSize),
		AddTotalSize:   int64PtrFromMessage(message, XPCKeyProgressUpdateAddTotalSize),
		SetTotalSize:   int64PtrFromMessage(message, XPCKeyProgressUpdateSetTotalSize),
	}
}

func stringPtrFromMessage(message *Message, key XPCKey) *string {
	value, ok := message.String(key)
	if !ok {
		return nil
	}
	return &value
}

func int64PtrFromMessage(message *Message, key XPCKey) *int64 {
	value, ok := message.values[string(key)]
	if !ok || value.kind != messageKindInt64 {
		return nil
	}
	return &value.i
}
