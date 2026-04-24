package pipeline

import (
	"context"
	"sync"
)

// Worker is a generic bounded-queue goroutine that calls handler for each dequeued item.
// The goroutine exits cleanly when ctx is cancelled.
type Worker[T any] struct {
	Queue chan T
	ctx   context.Context
	wg    *sync.WaitGroup
}

// NewWorker creates a Worker with a buffered queue of queueSize and starts its goroutine.
func NewWorker[T any](ctx context.Context, wg *sync.WaitGroup, queueSize int, handler func(T)) *Worker[T] {
	w := &Worker[T]{
		Queue: make(chan T, queueSize),
		ctx:   ctx,
		wg:    wg,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				// Drain any items already in the queue before exiting so that
				// the writer's buffer receives everything enqueued before shutdown.
				for {
					select {
					case item := <-w.Queue:
						handler(item)
					default:
						return
					}
				}
			case item := <-w.Queue:
				handler(item)
			}
		}
	}()

	return w
}

// Enqueue adds item to the worker's queue without blocking.
// Returns false if the queue is full.
func (w *Worker[T]) Enqueue(item T) bool {
	select {
	case w.Queue <- item:
		return true
	default:
		return false
	}
}
