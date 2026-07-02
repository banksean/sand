package xpc

type ProgressUpdate struct {
	Description    *string
	SubDescription *string
	ItemsName      *string
	AddTasks       *int64
	SetTasks       *int64
	AddTotalTasks  *int64
	SetTotalTasks  *int64
	AddItems       *int64
	SetItems       *int64
	AddTotalItems  *int64
	SetTotalItems  *int64
	AddSize        *int64
	SetSize        *int64
	AddTotalSize   *int64
	SetTotalSize   *int64
}

type ProgressHandler func(ProgressUpdate)

type progressEndpoint interface {
	Handle() uintptr
	Close()
}
