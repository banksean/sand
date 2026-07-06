//go:build darwin && cgo

package hostops

func newAppleContainerOps() (ContainerOps, error) {
	return NewXPCContainerOps()
}

func newAppleImageOps() (ImageOps, error) {
	return NewXPCImageOps()
}
