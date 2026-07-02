//go:build darwin && cgo

package xpc

/*
#cgo LDFLAGS: -framework Foundation
#include <xpc/xpc.h>
#include <stdlib.h>
#include <stdbool.h>

void set_empty_xpc_handler(xpc_connection_t conn);

static const char* xpc_error_description_key(void) {
	return XPC_ERROR_KEY_DESCRIPTION;
}
*/
import "C"
import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

type xpcSender struct {
	conn C.xpc_connection_t
}

func newDefaultSender(service string) (Sender, error) {
	serviceName := C.CString(service)
	defer C.free(unsafe.Pointer(serviceName))

	conn := C.xpc_connection_create_mach_service(serviceName, nil, 0)
	if conn == nil {
		return nil, fmt.Errorf("create XPC connection for %s", service)
	}
	C.set_empty_xpc_handler(conn)
	C.xpc_connection_resume(conn)
	return &xpcSender{conn: conn}, nil
}

func (s *xpcSender) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	C.xpc_connection_cancel(s.conn)
	s.conn = nil
	return nil
}

func (s *xpcSender) Send(ctx context.Context, message *Message) (*Message, error) {
	if s == nil || s.conn == nil {
		return nil, fmt.Errorf("XPC connection is closed")
	}
	type result struct {
		message *Message
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		request, err := messageToXPC(message)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer C.xpc_release(request)

		reply := C.xpc_connection_send_message_with_reply_sync(s.conn, request)
		if reply == nil {
			ch <- result{err: fmt.Errorf("nil XPC reply")}
			return
		}
		defer C.xpc_release(reply)

		parsed, err := xpcToMessage(reply)
		ch <- result{message: parsed, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		return result.message, result.err
	}
}

func messageToXPC(message *Message) (C.xpc_object_t, error) {
	obj := C.xpc_dictionary_create(nil, nil, 0)
	if obj == nil {
		return nil, fmt.Errorf("create XPC dictionary")
	}
	for key, value := range message.values {
		cKey := C.CString(key)
		switch value.kind {
		case messageKindString:
			cValue := C.CString(value.s)
			C.xpc_dictionary_set_string(obj, cKey, cValue)
			C.free(unsafe.Pointer(cValue))
		case messageKindBool:
			C.xpc_dictionary_set_bool(obj, cKey, C.bool(value.b))
		case messageKindInt64:
			C.xpc_dictionary_set_int64(obj, cKey, C.int64_t(value.i))
		case messageKindUint64:
			C.xpc_dictionary_set_uint64(obj, cKey, C.uint64_t(value.u))
		case messageKindData:
			if len(value.data) > 0 {
				ptr := C.CBytes(value.data)
				C.xpc_dictionary_set_data(obj, cKey, ptr, C.size_t(len(value.data)))
				C.free(ptr)
			} else {
				C.xpc_dictionary_set_data(obj, cKey, nil, 0)
			}
		case messageKindDate:
			ns := value.t.UnixNano()
			C.xpc_dictionary_set_date(obj, cKey, C.int64_t(ns))
		case messageKindFD:
			C.xpc_dictionary_set_fd(obj, cKey, C.int(value.fd))
		case messageKindEndpoint:
			C.xpc_dictionary_set_value(obj, cKey, C.xpc_object_t(unsafe.Pointer(value.ptr)))
		default:
			C.free(unsafe.Pointer(cKey))
			C.xpc_release(obj)
			return nil, fmt.Errorf("unsupported XPC message value kind %d", value.kind)
		}
		C.free(unsafe.Pointer(cKey))
	}
	return obj, nil
}

func xpcToMessage(obj C.xpc_object_t) (*Message, error) {
	if C.xpc_get_type(obj) == C.XPC_TYPE_ERROR {
		description := "unknown"
		if cDescription := C.xpc_dictionary_get_string(obj, C.xpc_error_description_key()); cDescription != nil {
			description = C.GoString(cDescription)
		}
		return nil, fmt.Errorf("XPC connection error: %s", description)
	}
	if C.xpc_get_type(obj) != C.XPC_TYPE_DICTIONARY {
		return nil, fmt.Errorf("unexpected XPC reply type")
	}

	message := newEmptyMessage()
	for _, key := range knownXPCDictionaryKeys() {
		cKey := C.CString(key)
		value := C.xpc_dictionary_get_value(obj, cKey)
		if value != nil {
			readXPCValue(message, key, value)
		}
		C.free(unsafe.Pointer(cKey))
	}
	return message, nil
}

func readXPCValue(message *Message, key string, value C.xpc_object_t) {
	switch C.xpc_get_type(value) {
	case C.XPC_TYPE_STRING:
		if raw := C.xpc_string_get_string_ptr(value); raw != nil {
			message.SetStringRaw(key, C.GoString(raw))
		}
	case C.XPC_TYPE_BOOL:
		message.set(key, messageValue{kind: messageKindBool, b: bool(C.xpc_bool_get_value(value))})
	case C.XPC_TYPE_INT64:
		message.set(key, messageValue{kind: messageKindInt64, i: int64(C.xpc_int64_get_value(value))})
	case C.XPC_TYPE_UINT64:
		message.set(key, messageValue{kind: messageKindUint64, u: uint64(C.xpc_uint64_get_value(value))})
	case C.XPC_TYPE_DATE:
		message.set(key, messageValue{kind: messageKindDate, t: unixNanoTime(int64(C.xpc_date_get_value(value)))})
	case C.XPC_TYPE_DATA:
		length := int(C.xpc_data_get_length(value))
		if length == 0 {
			message.SetDataRaw(key, nil)
			return
		}
		message.SetDataRaw(key, C.GoBytes(C.xpc_data_get_bytes_ptr(value), C.int(length)))
	case C.XPC_TYPE_FD:
		fd := C.xpc_fd_dup(value)
		if fd >= 0 {
			message.SetFDRaw(key, int(fd))
		}
	}
}

func unixNanoTime(ns int64) time.Time {
	return time.Unix(0, ns).UTC()
}
