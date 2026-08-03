package mount

import "sync"

type ActivityEvent struct {
	Path      string `json:"path"`
	Direction string `json:"direction"`
	Bytes     int64  `json:"bytes"`
	Timestamp int64  `json:"timestamp"`
	Error     string `json:"error,omitempty"`
}

const activityRingSize = 100

type activityRing struct {
	mu    sync.Mutex
	buf   [activityRingSize]ActivityEvent
	next  int
	count int
}

func (r *activityRing) add(ev ActivityEvent) {
	r.mu.Lock()
	r.buf[r.next] = ev
	r.next = (r.next + 1) % activityRingSize
	if r.count < activityRingSize {
		r.count++
	}
	r.mu.Unlock()
}

func (r *activityRing) snapshot() []ActivityEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ActivityEvent, 0, r.count)
	for i := 0; i < r.count; i++ {
		idx := ((r.next-1-i)%activityRingSize + activityRingSize) % activityRingSize
		out = append(out, r.buf[idx])
	}
	return out
}
