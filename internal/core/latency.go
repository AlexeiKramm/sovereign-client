package core

import (
	"fmt"
	"sync"
	"time"
)

// rttPendingCap bounds the in-flight PING map so a server that never answers cannot grow it
// without limit. At a 5 s heartbeat and a 15 s dead-link timeout, a healthy link holds ~3.
const rttPendingCap = 16

// RTT tracks PING/PONG round-trip times for one link. PING carries clientTs (epoch ms) and the
// matching PONG echoes it (PROTOCOL.md §6), so the round trip is measured on the client's own
// clock and needs no clock agreement with the phone.
type RTT struct {
	mu      sync.Mutex
	pending map[int64]time.Time
	order   []int64
	last    time.Duration
	worst   time.Duration
	samples int
}

func newRTT() *RTT { return &RTT{pending: make(map[int64]time.Time)} }

// sent records an outbound PING's clientTs.
func (r *RTT) sent(clientTs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[clientTs] = time.Now()
	r.order = append(r.order, clientTs)
	for len(r.order) > rttPendingCap {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.pending, oldest)
	}
}

// received matches a PONG against a known PING and records the round trip. An unknown or
// duplicate clientTs (a stale PONG) is ignored.
func (r *RTT) received(clientTs int64) (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.pending[clientTs]
	if !ok {
		return 0, false
	}
	delete(r.pending, clientTs)
	d := time.Since(t)
	r.last = d
	if d > r.worst {
		r.worst = d
	}
	r.samples++
	return d, true
}

// Last returns the most recent measured round-trip time.
func (r *RTT) Last() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// Stats returns the last and worst observed RTT and the sample count.
func (r *RTT) Stats() (last, worst time.Duration, samples int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, r.worst, r.samples
}

// Utterance accumulates one dictation session's timing so it can be logged as a single line at
// injection time (PLAN §7). It is confined to the client's reader goroutine.
type Utterance struct {
	sessionID    string
	started      time.Time
	asrMs        int64
	cleanupMs    int64
	units        int
	provisionals int
	committedLen int
}

func newUtterance(sessionID string) *Utterance {
	return &Utterance{sessionID: sessionID, started: time.Now()}
}

// commit folds one COMMIT_TEXT snapshot's timing in. The snapshot length is recorded (not
// accumulated) because COMMIT_TEXT.text is authoritative and may be rewritten.
func (u *Utterance) commit(seq int, perf CommitPerf, textLen int) {
	u.asrMs += perf.AsrMs
	u.cleanupMs += perf.CleanupMs
	u.units++
	u.committedLen = textLen
}

// provisional counts one live-tail frame.
func (u *Utterance) provisional() { u.provisionals++ }

// line renders the one-line per-utterance breakdown for the log.
func (u *Utterance) line(reason string) string {
	total := time.Since(u.started).Milliseconds()
	return fmt.Sprintf("utterance session=%s reason=%s total=%dms asr=%dms cleanup=%dms units=%d committed=%d provisional=%d",
		u.sessionID, reason, total, u.asrMs, u.cleanupMs, u.units, u.committedLen, u.provisionals)
}
