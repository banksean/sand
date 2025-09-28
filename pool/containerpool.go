package pool

import (
	"context"
	"fmt"
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

// NewConnectionPool creates a new container pool
func NewConnectionPool(ctx context.Context, maxSize int, newFunc func(ctx context.Context) (*PooledContainer, error), stopFunc func(ctx context.Context, pc *PooledContainer)) (*ContainerPool, error) {
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

// Acquire gets a container from the pool
func (cp *ContainerPool) Acquire(ctx context.Context) (*PooledContainer, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closing {
		return nil, fmt.Errorf("cannot acquire pooled container because the pool is shutting down")
	}
	select {
	case conn := <-cp.pool:
		fmt.Printf("Acquired existing container %s\n", conn.ID)
		return conn, nil
	default:
		if len(cp.pool) < cp.maxSize {
			cp.currentSize++
			newConn, err := cp.New(ctx)
			if err != nil {
				return nil, err
			}
			fmt.Printf("Created and acquired new container %s\n", newConn.ID)
			return newConn, nil
		}
		// Block until a container is available.
		conn := <-cp.pool
		fmt.Printf("Acquired existing container %s after waiting\n", conn.ID)
		return conn, nil
	}
}

// Release returns a container to the pool
func (cp *ContainerPool) Release(conn *PooledContainer) {
	// Optional: perform container validation/health checks before releasing
	cp.pool <- conn
	fmt.Printf("Released container %s\n", conn.ID)
}

// Remove removes a container from the pool and returns the new size of the pool.
func (cp *ContainerPool) Remove(conn *PooledContainer) int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.currentSize--
	fmt.Printf("Removed container %s, current pool size: %d\n", conn.ID, cp.currentSize)
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
			if cp.Remove(conn) == 0 {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
