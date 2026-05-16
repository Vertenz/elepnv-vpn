package xrayconfig

// ringBuf is an io.Writer that retains only the last `cap` bytes written.
// Used by Validate to cap stderr capture so a misbehaving xray-core process
// cannot pin arbitrary memory by spamming the error stream. Plan-1 §8.3
// quotas its IPC payloads; this is the equivalent guard for subprocess output.
//
// Not safe for concurrent use; Validate writes from a single goroutine.
type ringBuf struct {
	buf  []byte
	full bool // true once we've wrapped at least once
	pos  int  // next write position
	cap  int
}

func newRingBuf(cap int) *ringBuf {
	return &ringBuf{buf: make([]byte, 0, cap), cap: cap}
}

// Write implements io.Writer. The full input length is always reported back
// (so callers like exec.Cmd think every byte was accepted), even though only
// the last `cap` bytes are retained.
func (r *ringBuf) Write(p []byte) (int, error) {
	wrote := len(p)
	// Drop everything but the last `cap` bytes if the single write is bigger.
	if len(p) >= r.cap {
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		r.full = true
		r.pos = 0
		return wrote, nil
	}
	// Grow buf up to cap on first writes; ring-overwrite thereafter.
	if !r.full && len(r.buf)+len(p) <= r.cap {
		r.buf = append(r.buf, p...)
		if len(r.buf) == r.cap {
			r.full = true
			r.pos = 0
		}
		return wrote, nil
	}
	// Mixed case: some existing bytes fit, the rest are overwritten.
	// We keep the last (r.cap - len(p)) existing bytes and place them at the
	// END of the buffer so the new bytes p can occupy positions 0..len(p)-1.
	// That gives the correct ring layout where String() returns the last r.cap
	// bytes in chronological order.
	//
	// newRingBuf always does make([]byte, 0, cap), so cap(r.buf) == r.cap; we
	// extend the slice to its full capacity before doing any indexed writes so
	// that all index operations are in-range.
	r.full = true
	keep := r.cap - len(p)              // how many existing bytes survive
	existing := r.buf[len(r.buf)-keep:] // the surviving tail of what was written
	r.buf = r.buf[:r.cap]
	copy(r.buf[len(p):], existing)
	copy(r.buf[:len(p)], p)
	r.pos = len(p)
	return wrote, nil
}

// String returns the retained bytes in write order.
func (r *ringBuf) String() string {
	if !r.full {
		return string(r.buf)
	}
	out := make([]byte, 0, r.cap)
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return string(out)
}
