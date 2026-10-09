// Package usage batches request records into the database off the request path.
package usage

import (
	"context"
	"log/slog"
	"time"

	"github.com/sobakintech/maitake/internal/store"
)

type Recorder struct {
	store *store.Store
	ch    chan store.UsageEvent
	done  chan struct{}
}

func NewRecorder(st *store.Store) *Recorder {
	return &Recorder{store: st, ch: make(chan store.UsageEvent, 4096), done: make(chan struct{})}
}

// Record queues an event; if the queue is full the event is dropped rather than blocking a request.
func (r *Recorder) Record(e store.UsageEvent) {
	select {
	case r.ch <- e:
	default:
		slog.Warn("usage queue full; dropping event")
	}
}

// Run flushes queued events until ctx ends, then drains what's left.
func (r *Recorder) Run(ctx context.Context) {
	defer close(r.done)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var batch []store.UsageEvent
	flush := func() {
		if len(batch) == 0 {
			return
		}
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := r.store.InsertUsage(fctx, batch); err != nil {
			slog.Error("write usage", "err", err, "events", len(batch))
		}
		cancel()
		batch = batch[:0]
	}
	for {
		select {
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= 200 {
				flush()
			}
		case <-tick.C:
			flush()
		case <-ctx.Done():
			for {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Wait blocks until Run has drained the queue after shutdown.
func (r *Recorder) Wait() { <-r.done }
