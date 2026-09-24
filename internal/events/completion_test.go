package events

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type completionStoreStub struct {
	counts []int64
	calls  int
}

func (s *completionStoreStub) CompleteDue(context.Context, time.Time, int64) (int64, error) {
	value := s.counts[s.calls]
	s.calls++
	return value, nil
}

func TestCompletionWorkerDrainsBatches(t *testing.T) {
	store := &completionStoreStub{counts: []int64{2, 1}}
	worker := NewCompletionWorker(store, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, 2)
	worker.complete(context.Background())
	if store.calls != 2 {
		t.Fatalf("calls=%d, want 2", store.calls)
	}
}
