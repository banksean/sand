//go:build darwin && cgo

package hostops

func newAppleContainerOps() ContainerOps {
	ops, err := NewXPCContainerOps()
	if err != nil {
		return &appleContainerOps{}
	}
	return ops
}

func newAppleImageOps() ImageOps {
	ops, err := NewXPCImageOps()
	if err != nil {
		return &appleImageOps{}
	}
	return ops
}
