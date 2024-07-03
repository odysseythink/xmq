package writers

import (
	"context"
	"io"
	"time"
)

type SpreadWriter struct {
	w         io.Writer
	interval  time.Duration
	buf       [][]byte
	ctx       context.Context
	ctxCancel context.CancelFunc
	parentCtx context.Context
}

func NewSpreadWriter(w io.Writer, interval time.Duration, ctx context.Context) *SpreadWriter {
	writer := &SpreadWriter{
		w:         w,
		interval:  interval,
		buf:       make([][]byte, 0),
		parentCtx: ctx,
	}
	writer.ctx, writer.ctxCancel = context.WithCancel(ctx)

	return writer
}

func (s *SpreadWriter) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	s.buf = append(s.buf, b)
	return len(p), nil
}

func (s *SpreadWriter) Flush() {
	if len(s.buf) == 0 {
		// nothing to write, just wait
		select {
		case <-time.After(s.interval):
		case <-s.parentCtx.Done():
		case <-s.ctx.Done():
		}
		return
	}
	sleep := s.interval / time.Duration(len(s.buf))
	ticker := time.NewTicker(sleep)
	for _, b := range s.buf {
		s.w.Write(b)
		select {
		case <-ticker.C:
		case <-s.parentCtx.Done():
		case <-s.ctx.Done():
		}
	}
	ticker.Stop()
	s.buf = s.buf[:0]
}
