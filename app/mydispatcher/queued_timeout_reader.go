package mydispatcher

import (
	"sync"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/features/stats"
)

// queuedTimeoutBufReader wraps any buf.Reader for sniffing. Unlike buf.TimeoutWrapperReader,
// if ReadMultiBufferTimeout hits the deadline while inner.ReadMultiBuffer is still blocked,
// the eventual read is merged into a FIFO pending buffer so bytes are never dropped before
// the next consumer read (required for Vision / REALITY uplink TLS replay).
type queuedTimeoutBufReader struct {
	mu         sync.Mutex
	inner      buf.Reader
	pending    buf.MultiBuffer
	inflightCh chan asyncReadResult
	Counter    stats.Counter
}

type asyncReadResult struct {
	mb  buf.MultiBuffer
	err error
}

func (r *queuedTimeoutBufReader) countRead(mb buf.MultiBuffer) {
	if r.Counter != nil && !mb.IsEmpty() {
		r.Counter.Add(int64(mb.Len()))
	}
}

// absorbInflight blocks until a timed-out inner read completes, then merges bytes into pending.
func (r *queuedTimeoutBufReader) absorbInflight() {
	r.mu.Lock()
	ch := r.inflightCh
	r.mu.Unlock()
	if ch == nil {
		return
	}
	res := <-ch
	r.mu.Lock()
	r.inflightCh = nil
	if !res.mb.IsEmpty() {
		r.pending, _ = buf.MergeMulti(r.pending, res.mb)
	}
	r.mu.Unlock()
}

func (r *queuedTimeoutBufReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	r.absorbInflight()

	r.mu.Lock()
	if r.pending != nil && !r.pending.IsEmpty() {
		mb := r.pending
		r.pending = nil
		r.mu.Unlock()
		r.countRead(mb)
		return mb, nil
	}
	r.mu.Unlock()

	mb, err := r.inner.ReadMultiBuffer()
	r.countRead(mb)
	return mb, err
}

func (r *queuedTimeoutBufReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	r.absorbInflight()

	r.mu.Lock()
	if r.pending != nil && !r.pending.IsEmpty() {
		mb := r.pending
		r.pending = nil
		r.mu.Unlock()
		r.countRead(mb)
		return mb, nil
	}
	ch := make(chan asyncReadResult, 1)
	r.inflightCh = ch
	r.mu.Unlock()

	go func() {
		mb, err := r.inner.ReadMultiBuffer()
		ch <- asyncReadResult{mb, err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		r.mu.Lock()
		r.inflightCh = nil
		r.mu.Unlock()
		r.countRead(res.mb)
		return res.mb, res.err
	case <-timer.C:
		return nil, buf.ErrReadTimeout
	}
}

func (r *queuedTimeoutBufReader) Interrupt() {
	common.Interrupt(r.inner)
}
