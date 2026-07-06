//go:build !darwin || !cgo

package hostops

import "fmt"

func newAppleContainerOps() (ContainerOps, error) {
	return nil, fmt.Errorf("apple container XPC operations require darwin and cgo")
}

func newAppleImageOps() (ImageOps, error) {
	return nil, fmt.Errorf("apple container XPC image operations require darwin and cgo")
}
