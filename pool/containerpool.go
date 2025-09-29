package pool

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type PooledContainer struct {
	ID string
}

// ContainerPool manages a pool of containers
type ContainerPool struct {
	pool        chan *PooledContainer
	maxSize     int
	currentSize int
	mu          sync.Mutex
	closing     bool
	New         func(ctx context.Context) (*PooledContainer, error)
	Stop        func(ctx context.Context, pc *PooledContainer)
}

// NewContainerPool creates a new container pool
func NewContainerPool(ctx context.Context, maxSize int, newFunc func(ctx context.Context) (*PooledContainer, error), stopFunc func(ctx context.Context, pc *PooledContainer)) (*ContainerPool, error) {
	pool := make(chan *PooledContainer, maxSize)
	for i := 0; i < maxSize/2; i++ { // Initialize with some containers
		pc, err := newFunc(ctx)
		if err != nil {
			return nil, err
		}
		pool <- pc
	}
	return &ContainerPool{
		pool:        pool,
		maxSize:     maxSize,
		currentSize: maxSize / 2,
		New:         newFunc,
		Stop:        stopFunc,
		closing:     false,
	}, nil
}

var ErrPoolIsClosing = errors.New("pool is shutting down")

// Acquire gets a container from the pool
func (cp *ContainerPool) Acquire(ctx context.Context) (*PooledContainer, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closing {
		return nil, ErrPoolIsClosing
	}
	select {
	case conn := <-cp.pool:
		slog.InfoContext(ctx, "ContainerPool.Acquire returning existing container", "container_id", conn.ID)
		return conn, nil
	default:
		if len(cp.pool) < cp.maxSize {
			cp.currentSize++
			newConn, err := cp.New(ctx)
			if err != nil {
				return nil, err
			}
			slog.InfoContext(ctx, "ContainerPool.Acquire created and acquired new container", "container_id", newConn.ID)
			return newConn, nil
		}
		// Block until a container is available.
		conn := <-cp.pool
		slog.InfoContext(ctx, "ContainerPool.Acquire returning existing container after waiting", "container_id", conn.ID)
		return conn, nil
	}
}

// Release returns a container to the pool
func (cp *ContainerPool) Release(ctx context.Context, conn *PooledContainer) {
	// Optional: perform container validation/health checks before releasing
	cp.pool <- conn
	slog.InfoContext(ctx, "ContainerPool.Release", "container_id", conn.ID)
}

// Remove removes a container from the pool and returns the new size of the pool.
func (cp *ContainerPool) Remove(ctx context.Context, conn *PooledContainer) int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.currentSize--
	slog.InfoContext(ctx, "ContainerPool.Remove", "container_id", conn.ID, "new_pool_size", cp.currentSize)
	return cp.currentSize
}

// Shutdown forces any subsequent calls to .Acquire to return an error, and cycles through
// all pooled containers to call #Stop on each. This function should be called with a TimeoutContext,
// because it may block on
func (cp *ContainerPool) Shutdown(ctx context.Context) error {
	cp.mu.Lock()
	// set .closing to true so that any subsequent callers of .Acquire will
	// get an error instead of acquring a pooled container.
	cp.closing = true
	cp.mu.Unlock()
	for {
		select {
		case conn := <-cp.pool:
			cp.Stop(ctx, conn)
			if cp.Remove(ctx, conn) == 0 {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
