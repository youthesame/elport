package client

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type eofOnCancelReader struct {
	ctx  context.Context
	sent bool
}

func (r *eofOnCancelReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	<-r.ctx.Done()
	return 0, io.EOF
}

func TestReadAllWithIdleTimeoutRejectsEOFOnTimeoutCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := readAllWithIdleTimeout(ctx, &eofOnCancelReader{ctx: ctx}, 20*time.Millisecond, cancel)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded on timeout-triggered EOF, got %v", err)
	}
}
