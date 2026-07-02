//go:build !darwin || !cgo

package xpc

func newProgressEndpoint(ProgressHandler) (progressEndpoint, error) {
	return nil, nil
}
