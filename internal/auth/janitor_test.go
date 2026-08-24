package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// janitorStore intercepta DeleteExpired para observar las pasadas del janitor.
type janitorStore struct {
	SessionStore
	deleted chan time.Time
}

func (j *janitorStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	j.deleted <- now
	return 1, nil
}

func TestService_RunJanitor(t *testing.T) {
	store := &janitorStore{SessionStore: NewMemorySessionStore(), deleted: make(chan time.Time, 8)}
	svc := NewService(NewMemoryUserStore(), store, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		svc.RunJanitor(ctx, 10*time.Millisecond, 5*time.Millisecond, logger)
		close(done)
	}()
	select {
	case <-store.deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("RunJanitor no llamó a DeleteExpired")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunJanitor no terminó al cancelar el contexto")
	}
}
