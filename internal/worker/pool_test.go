package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/order"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

type fakeNotifier struct {
	calls atomic.Int64
	done  chan struct{}
}

func (n *fakeNotifier) NotifyOrderCreated(context.Context, order.Order) error {
	n.calls.Add(1)
	n.done <- struct{}{}
	return nil
}

func TestPoolProcessesQueuedOrders(t *testing.T) {
	notifier := &fakeNotifier{done: make(chan struct{}, 8)}
	pool := NewPool(2, 8, notifier, testLogger())

	for i := 0; i < 4; i++ {
		pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	}
	for i := 0; i < 4; i++ {
		select {
		case <-notifier.done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for a notification")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := notifier.calls.Load(); got != 4 {
		t.Fatalf("notification count = %d, want 4", got)
	}
}

type flakyNotifier struct {
	calls   atomic.Int64
	success chan struct{}
}

func (n *flakyNotifier) NotifyOrderCreated(context.Context, order.Order) error {
	if n.calls.Add(1) < 3 {
		return errors.New("temporary notification failure")
	}
	n.success <- struct{}{}
	return nil
}

func TestPoolRetriesFailedNotifications(t *testing.T) {
	notifier := &flakyNotifier{success: make(chan struct{}, 1)}
	pool := NewPool(1, 2, notifier, testLogger())
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})

	select {
	case <-notifier.success:
	case <-time.After(3 * time.Second):
		t.Fatal("notification was not retried to success")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := notifier.calls.Load(); got != 3 {
		t.Fatalf("notification attempts = %d, want 3", got)
	}
}

type panicNotifier struct {
	calls   atomic.Int64
	success chan struct{}
}

func (n *panicNotifier) NotifyOrderCreated(context.Context, order.Order) error {
	if n.calls.Add(1) == 1 {
		panic("simulated notifier panic")
	}
	n.success <- struct{}{}
	return nil
}

func TestPoolRecoversPanicAndKeepsProcessing(t *testing.T) {
	notifier := &panicNotifier{success: make(chan struct{}, 1)}
	pool := NewPool(1, 4, notifier, testLogger())
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})

	select {
	case <-notifier.success:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not recover after a panic")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := notifier.calls.Load(); got != 2 {
		t.Fatalf("notification calls = %d, want 2", got)
	}
}

type blockingNotifier struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (n *blockingNotifier) NotifyOrderCreated(context.Context, order.Order) error {
	n.calls.Add(1)
	n.started <- struct{}{}
	<-n.release
	return nil
}

func TestShutdownDrainsInFlightAndQueuedJobs(t *testing.T) {
	notifier := &blockingNotifier{
		started: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	pool := NewPool(1, 4, notifier, testLogger())
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	<-notifier.started
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdownDone <- pool.Shutdown(ctx)
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown() returned before in-flight work finished: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(notifier.release)
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown() did not drain queued work")
	}

	if got := notifier.calls.Load(); got != 2 {
		t.Fatalf("notification calls = %d, want 2", got)
	}
}

func TestPublishAfterShutdownDoesNotPanicOrEnqueue(t *testing.T) {
	notifier := &fakeNotifier{done: make(chan struct{}, 1)}
	pool := NewPool(1, 1, notifier, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	select {
	case <-notifier.done:
		t.Fatal("notification was delivered after shutdown")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFullQueueDropsWithoutBlocking(t *testing.T) {
	notifier := &blockingNotifier{
		started: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	pool := NewPool(1, 1, notifier, testLogger())

	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	<-notifier.started
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})
	pool.PublishOrderCreated(context.Background(), order.Order{ID: uuid.New()})

	close(notifier.release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := notifier.calls.Load(); got != 2 {
		t.Fatalf("notification calls = %d, want 2 (one dropped)", got)
	}
}
