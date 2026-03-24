//go:build !darwin

package hostops

func NewDefaultFileOps() FileOps {
	panic("only call NewDefaultFileOps from darwin")
}
