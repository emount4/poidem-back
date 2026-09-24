package events

import (
	"context"
	"log/slog"
	"time"
)

type CompletionStore interface {
	CompleteDue(context.Context, time.Time, int64) (int64, error)
}

type CompletionWorker struct {
	store    CompletionStore
	log      *slog.Logger
	interval time.Duration
	batch    int64
	now      func() time.Time
}

func NewCompletionWorker(store CompletionStore, log *slog.Logger, interval time.Duration, batch int64) *CompletionWorker {
	return &CompletionWorker{store: store, log: log, interval: interval, batch: batch, now: time.Now}
}

func (w *CompletionWorker) Run(ctx context.Context) {
	w.complete(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.complete(ctx)
		}
	}
}

func (w *CompletionWorker) complete(ctx context.Context) {
	for {
		count, err := w.store.CompleteDue(ctx, w.now(), w.batch)
		if err != nil {
			if ctx.Err() == nil {
				w.log.Error("complete due events", "error", err)
			}
			return
		}
		if count > 0 {
			w.log.Info("events completed", "count", count)
		}
		if count < w.batch {
			return
		}
	}
}
