//go:build !darwin || !cgo

package xpc

import "fmt"

func newDefaultSender(service string) (Sender, error) {
	return nil, fmt.Errorf("Apple Container XPC is only available on macOS with cgo enabled")
}
