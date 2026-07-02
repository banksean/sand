//go:build darwin && cgo

package hostops

func newAppleContainerOps() ContainerOps {
	ops, err := NewXPCContainerOps()
	if err != nil {
		return &appleContainerOps{}
	}
	return ops
}
