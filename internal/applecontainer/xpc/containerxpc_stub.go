//go:build !darwin || !cgo

package xpc

import "fmt"

func Demo() {
	fmt.Println("Apple Container XPC demo is only available on macOS with cgo enabled.")
}

func ListContainers() ([]ContainerSnapshot, error) {
	return nil, fmt.Errorf("Apple Container XPC is only available on macOS with cgo enabled")
}
