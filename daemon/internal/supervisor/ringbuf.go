package supervisor

import "sync"

type ringBuf struct {
	mu  sync.Mutex
	buf []byte
	cap int // = 4096 when zero
	pos int
}

func (r *ringBuf) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cap == 0 {
		r.cap = 4096
	}
	if r.buf == nil {
		r.buf = make([]byte, 0, r.cap)
	}
	for _, b := range p {
		if len(r.buf) < r.cap {
			r.buf = append(r.buf, b)
		} else {
			r.buf[r.pos] = b
			r.pos = (r.pos + 1) % r.cap
		}
	}
	return len(p), nil
}

func (r *ringBuf) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) < r.cap {
		return string(r.buf)
	}
	out := make([]byte, r.cap)
	copy(out, r.buf[r.pos:])
	copy(out[r.cap-r.pos:], r.buf[:r.pos])
	return string(out)
}
