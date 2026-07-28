package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DharmilG/go-commerce/internal/order"
)

// Notifier is the outbound side effect of an order event. Real email and push
// providers can replace the logging implementation without changing the pool.
type Notifier interface {
	NotifyOrderCreated(context.Context, order.Order) error
}

// Pool is a bounded worker pool fed by a buffered channel.
type Pool struct {
	jobs     chan order.Order
	notifier Notifier
	logger   *slog.Logger

	wg        sync.WaitGroup
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	done      chan struct{}
}

func NewPool(workers, queueSize int, notifier Notifier, logger *slog.Logger) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}

	pool := &Pool{
		jobs:     make(chan order.Order, queueSize),
		notifier: notifier,
		logger:   logger,
		done:     make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}
	return pool
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for job := range p.jobs {
		p.process(id, job)
	}
}

func (p *Pool) process(workerID int, orderData order.Order) {
	defer func() {
		if recovered := recover(); recovered != nil {
			p.logger.Error("order worker panic recovered",
				"worker", workerID,
				"order_id", orderData.ID,
				"panic", recovered,
			)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	backoff := 100 * time.Millisecond
	for attempt := 1; attempt <= 3; attempt++ {
		if err := p.notifier.NotifyOrderCreated(ctx, orderData); err == nil {
			p.logger.Info("order notification processed",
				"worker", workerID,
				"order_id", orderData.ID,
				"attempt", attempt,
			)
			return
		} else {
			p.logger.Warn("order notification attempt failed",
				"worker", workerID,
				"order_id", orderData.ID,
				"attempt", attempt,
				"error", err,
			)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// PublishOrderCreated never blocks the HTTP response. A full queue is logged
// and the order remains committed; durable delivery is future work.
func (p *Pool) PublishOrderCreated(ctx context.Context, orderData order.Order) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.logger.WarnContext(ctx, "order event dropped because the worker pool is closed", "order_id", orderData.ID)
		return
	}

	select {
	case p.jobs <- orderData:
	default:
		p.logger.WarnContext(ctx, "order event dropped because the worker queue is full", "order_id", orderData.ID)
	}
}

// Shutdown stops accepting jobs, drains queued work, and waits for workers or
// the supplied context deadline.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.jobs)
		p.mu.Unlock()

		go func() {
			p.wg.Wait()
			close(p.done)
		}()
	})

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for order workers: %w", ctx.Err())
	}
}
