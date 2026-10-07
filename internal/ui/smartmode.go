package ui

// Action is what a hotkey edge asks the tray to do.
type Action int

const (
	ActionNone Action = iota
	ActionStart
	ActionStop
)

// SmartMode combines tap-to-toggle and hold-to-talk on one hotkey: a quick tap starts dictation and
// leaves it running (or stops it if running), while holding starts dictation immediately and stops
// it on release. Because dictation starts on the press in both cases, no audio is lost waiting to
// classify the gesture; the tap window only decides what the *release* does.
//
// It is single-threaded by contract: the caller drives it from one goroutine and delivers the
// tap-window elapse as TimerFired.
type SmartMode struct {
	state smartState
}

type smartState int

const (
	smartIdle    smartState = iota
	smartPending            // started on press, waiting to learn whether the release is a tap or a hold
	smartHolding            // held past the tap window
	smartOn                 // a tap toggled dictation on; no key is held
	smartSuppress
)

// Press handles the chord going down. armTimer reports whether the caller should start its tap
// window (and later call TimerFired).
func (s *SmartMode) Press() (action Action, armTimer bool) {
	if s.state == smartIdle {
		s.state = smartPending
		return ActionStart, true
	}
	// Pending/holding/on: this press stops the session and its release is ignored.
	s.state = smartSuppress
	return ActionStop, false
}

// Release handles the chord going up. cancelTimer reports whether the caller should stop its tap
// window.
func (s *SmartMode) Release() (action Action, cancelTimer bool) {
	switch s.state {
	case smartPending:
		// Released inside the tap window: a tap, so dictation keeps running.
		s.state = smartOn
		return ActionNone, true
	case smartHolding:
		s.state = smartIdle
		return ActionStop, false
	case smartSuppress:
		s.state = smartIdle
		return ActionNone, false
	default:
		return ActionNone, false
	}
}

// TimerFired is the caller's tap window elapsing: it promotes a pending press to a hold.
func (s *SmartMode) TimerFired() Action {
	if s.state == smartPending {
		s.state = smartHolding
	}
	return ActionNone
}
