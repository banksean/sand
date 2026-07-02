//go:build !darwin || !cgo

package hostops

func newAppleContainerOps() ContainerOps {
	return &appleContainerOps{}
}
