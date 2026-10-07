package ui

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
	"github.com/alexeikramm/sovereign-desktop/internal/core/iface"
	winplat "github.com/alexeikramm/sovereign-desktop/internal/windows"
)

// Options parameterises the tray application.
type Options struct {
	Identity        core.Identity
	Store           core.CredentialStore
	SettingsPath    string        // where settings.json lives
	Addr            string        // optional manual host:port, bypassing discovery
	Pin             string        // optional pin for Addr
	DiscoverTimeout time.Duration // mDNS/probe timeout; defaults to 5 s
	Logf            func(format string, args ...any)
}

// Run starts the tray and blocks until the user quits. It must be called from the main goroutine:
// systray owns the message loop.
func Run(opts Options) {
	if opts.DiscoverTimeout == 0 {
		opts.DiscoverTimeout = 5 * time.Second
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	a := &app{opts: opts, logf: opts.Logf, settings: DefaultSettings()}
	if exe, err := os.Executable(); err == nil {
		a.exePath = exe
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	systray.Run(a.onReady, a.onExit)
}

type app struct {
	opts     Options
	logf     func(string, ...any)
	settings Settings
	exePath  string

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	creds      core.Credentials
	paired     bool
	client     *core.Client
	connecting bool
	connCancel context.CancelFunc
	dictating  bool                    // a hotkey-initiated session is live
	phoneState core.ServerStatePayload // last unsolicited SERVER_STATE (v1.1); advisory
	target     uintptr                 // window to inject into, captured when dictation started
	hotkey     *winplat.HotkeyWatcher
	armed      bool
	smartReset chan struct{} // signals the smart-mode loop that the session ended externally

	// Pill overlay (Chunk 4c). overlayOnce guards creation; pillMu guards the overlay handle, the
	// Esc watcher and the view, kept separate from mu so a client callback never nests locks.
	overlayOnce sync.Once
	pillMu      sync.Mutex
	overlay     *winplat.Overlay
	escWatcher  *winplat.HotkeyWatcher
	pill        pillView

	mStatus    *systray.MenuItem
	mTest      *systray.MenuItem
	mPair      *systray.MenuItem
	mUnpair    *systray.MenuItem
	mAuto      *systray.MenuItem
	mHold      *systray.MenuItem
	mToggle    *systray.MenuItem
	mHotkey    *systray.MenuItem
	mSetHotkey *systray.MenuItem
	mStartup   *systray.MenuItem
	mQuit      *systray.MenuItem
	hotkeys    map[string]*systray.MenuItem
	insertion  map[string]*systray.MenuItem
	positions  map[string]*systray.MenuItem
}

func (a *app) onReady() {
	if s, err := LoadSettings(a.opts.SettingsPath); err != nil {
		a.logf("settings load failed: %v", err)
	} else {
		a.settings = s
	}

	creds, paired, err := a.opts.Store.Load()
	if err != nil {
		a.logf("credential load failed: %v", err)
		paired = false
	}
	a.mu.Lock()
	a.creds, a.paired = creds, paired
	a.mu.Unlock()

	a.buildMenu(paired)
	a.setState(StateOffline, "starting")
	a.logf("tray ready; paired=%v", paired)
	go a.connect()
}

func (a *app) onExit() {
	a.cancel()
}

func (a *app) buildMenu(paired bool) {
	systray.SetIcon(trayIcon(StateOffline))
	systray.SetTooltip("Sovereign — Offline")

	a.mStatus = systray.AddMenuItem("Offline", "Connection status")
	a.mStatus.Disable()
	a.mTest = systray.AddMenuItem("Test dictation (5s)", "Start and stop a five-second dictation")
	a.mTest.Disable()

	systray.AddSeparator()
	a.mPair = systray.AddMenuItem("Pair with phone…", "Run mutual-accept pairing")
	a.mUnpair = systray.AddMenuItem("Unpair…", "Delete the stored token and pin")

	systray.AddSeparator()
	mode := systray.AddMenuItem("Dictation mode", "Tap to toggle, hold to talk")
	a.mAuto = mode.AddSubMenuItemCheckbox("Tap to toggle, hold to talk", "", a.settings.Mode == ModeAuto)
	a.mHold = mode.AddSubMenuItemCheckbox("Hold to talk", "", a.settings.Mode == ModeHold)
	a.mToggle = mode.AddSubMenuItemCheckbox("Press to toggle", "", a.settings.Mode == ModeToggle)

	a.mHotkey = systray.AddMenuItem(hotkeyTitle(a.settings.Hotkey), "The global dictation hotkey")
	a.hotkeys = map[string]*systray.MenuItem{}
	for _, p := range HotkeyPresets {
		a.hotkeys[p] = a.mHotkey.AddSubMenuItemCheckbox(p, "", p == a.settings.Hotkey)
	}
	a.mSetHotkey = a.mHotkey.AddSubMenuItem("Set custom hotkey…", "Press the combination you want")

	insert := systray.AddMenuItem("Insertion", "How the finished text is written")
	a.insertion = map[string]*systray.MenuItem{}
	for _, m := range insertions {
		a.insertion[m] = insert.AddSubMenuItemCheckbox(insertionLabel(m), "", m == a.settings.Insertion)
	}

	pos := systray.AddMenuItem("Overlay position", "Where the pill appears on screen")
	a.positions = map[string]*systray.MenuItem{}
	for _, p := range overlayPositions {
		a.positions[p] = pos.AddSubMenuItemCheckbox(posLabel(p), "", p == a.settings.OverlayPosition)
	}

	a.mStartup = systray.AddMenuItemCheckbox("Start with Windows", "Launch the tray at sign-in", a.startupEnabled())

	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("Quit", "Exit Sovereign desktop")

	if paired {
		a.mPair.Disable()
	} else {
		a.mUnpair.Disable()
	}

	a.onClicks(a.mTest, func() { go a.testDictation() })
	a.onClicks(a.mPair, func() { go a.doPair() })
	a.onClicks(a.mUnpair, func() { go a.doUnpair() })
	a.onClicks(a.mAuto, func() { a.setMode(ModeAuto) })
	a.onClicks(a.mHold, func() { a.setMode(ModeHold) })
	a.onClicks(a.mToggle, func() { a.setMode(ModeToggle) })
	a.onClicks(a.mSetHotkey, func() { go a.doSetHotkey() })
	a.onClicks(a.mStartup, func() { a.toggleStartup() })
	a.onClicks(a.mQuit, a.quit)
	for p, item := range a.hotkeys {
		p, item := p, item
		a.onClicks(item, func() { a.applyHotkey(p) })
	}
	for m, item := range a.insertion {
		m, item := m, item
		a.onClicks(item, func() { a.setInsertion(m) })
	}
	for p, item := range a.positions {
		p, item := p, item
		a.onClicks(item, func() { a.setPosition(p) })
	}
}

// onClicks runs fn on every click of item until the app exits. Each item gets its own goroutine so
// no handler can block the tray's message loop.
func (a *app) onClicks(item *systray.MenuItem, fn func()) {
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-item.ClickedCh:
				fn()
			}
		}
	}()
}

func (a *app) quit() {
	a.disarmHotkey()
	a.closePill()
	a.cancel()
	systray.Quit()
}

// setState updates the icon, tooltip and status row. It is safe to call from any goroutine.
func (a *app) setState(s TrayState, detail string) {
	systray.SetIcon(trayIcon(s))
	systray.SetTooltip("Sovereign — " + statusTitle(s, detail))
	if a.mStatus != nil {
		a.mStatus.SetTitle(statusTitle(s, detail))
	}
	if a.mTest != nil {
		if s == StateReady {
			a.mTest.Enable()
		} else {
			a.mTest.Disable()
		}
	}

	// Hotkeys are live only while there is a link to dictate over.
	switch s {
	case StateReady, StateDictating:
		a.armHotkey()
	default:
		a.disarmHotkey()
	}
}

// connect resolves the phone and runs the client until the link dies fatally or the app exits.
// Only one connect goroutine runs at a time.
func (a *app) connect() {
	a.mu.Lock()
	if a.connecting || !a.paired {
		a.mu.Unlock()
		return
	}
	a.connecting = true
	cctx, ccancel := context.WithCancel(a.ctx)
	a.connCancel = ccancel
	a.mu.Unlock()

	defer func() {
		ccancel()
		a.mu.Lock()
		a.connecting = false
		a.client = nil
		a.connCancel = nil
		a.mu.Unlock()
	}()

	for cctx.Err() == nil {
		target, pin, err := a.resolve(cctx)
		if err != nil {
			// No phone yet (or discovery is flaky): keep trying, so starting the tray before the
			// phone server is not a dead end.
			a.setState(StateError, "searching for the phone")
			a.logf("discovery failed: %v", err)
			if !sleepCtx(cctx, 3*time.Second) {
				return
			}
			continue
		}

		client := core.NewClient(core.ClientConfig{
			Addr:     target,
			Pin:      pin,
			Ident:    a.opts.Identity,
			Creds:    a.credsSnapshot(),
			Handlers: a.handlers(),
			Logf:     a.logf,
		})
		a.mu.Lock()
		a.client = client
		a.mu.Unlock()

		a.logf("connecting to wss://%s%s", target, core.Path)
		err = client.Run(cctx)
		if cctx.Err() != nil {
			return
		}

		// Run reconnects internally, so returning means a fatal condition: a rejected identity or
		// a certificate mismatch. Surface it and stop; the user must re-pair or fix the network.
		var ae *core.AuthError
		switch {
		case errors.As(err, &ae):
			a.setState(StateError, "re-pair required")
			a.logf("authentication failed: %v", err)
		case errors.Is(err, core.ErrPinMismatch):
			a.setState(StateError, "certificate pin mismatch")
			a.logf("pin mismatch: %v", err)
		default:
			a.setState(StateError, errString(err))
			a.logf("link ended: %v", err)
		}
		return
	}
}

// resolve returns the address and pin to dial: the manual override when set, else discovery.
func (a *app) resolve(ctx context.Context) (addr, pin string, err error) {
	creds := a.credsSnapshot()
	if a.opts.Addr != "" {
		pin = a.opts.Pin
		if pin == "" {
			pin = creds.ServerSpki
		}
		return a.opts.Addr, pin, nil
	}
	peer, err := core.Discover(ctx, a.opts.DiscoverTimeout)
	if err != nil {
		return "", "", err
	}
	pin = creds.ServerSpki
	if pin == "" {
		pin = peer.Spki
	}
	return peer.Addr, pin, nil
}

func (a *app) handlers() core.Handlers {
	return core.Handlers{
		OnConn: func(st core.ConnState, detail string) {
			a.setState(mapConnState(st), detail)
			if st != core.StateReady {
				a.hidePill() // a dropped or unauthenticated link takes the pill down with it
				a.signalSmartReset()
			}
		},
		OnStarted: func(sessionID string) {
			a.setDictating(true)
			a.setState(StateDictating, "listening")
			a.startPill()
		},
		OnState: func(sessionID, st string) {
			if st == core.StateIdle {
				a.setDictating(false)
				a.setState(StateReady, "")
				a.hidePill()
				a.signalSmartReset()
			} else {
				a.setState(StateDictating, strings.ToLower(st))
				a.setPillStatus(pillStatus(st))
			}
		},
		OnBusy: func(holder string, takeover bool) {
			a.setDictating(false)
			a.setState(StateError, "phone busy ("+holder+")")
			a.hidePill()
			a.signalSmartReset()
		},
		OnServerState: func(s core.ServerStatePayload) {
			a.setPhoneState(s)
		},
		OnCommitted: func(sessionID, text string, perf core.CommitPerf) {
			a.logf("commit: %q (asr=%dms cleanup=%dms)", text, perf.AsrMs, perf.CleanupMs)
			a.setPillCommitted(text)
		},
		OnProvisional: func(sessionID, text string) {
			a.logf("live: %q", text)
			a.setPillProvisional(text)
		},
		// UTTERANCE_DONE is the only injection trigger (PROTOCOL.md §6), and only FINAL carries
		// text worth typing.
		OnUtterance: func(sessionID, delivery, raw, reason string) {
			a.setDictating(false)
			a.setState(StateReady, "")
			a.hidePill()
			a.signalSmartReset()
			a.logf("utterance done (%s): %q", reason, delivery)
			if reason == core.DoneFinal && delivery != "" {
				a.inject(delivery)
			}
		},
		OnReleased: func(sessionID, reason string) {
			a.setDictating(false)
			a.setState(StateReady, "")
			a.hidePill()
			a.signalSmartReset()
		},
		OnError: func(code, msg string, fatal bool) {
			a.logf("phone error: %s: %s (fatal=%v)", code, msg, fatal)
			if fatal {
				a.setDictating(false)
				a.setState(StateError, code)
				a.hidePill()
				a.signalSmartReset()
			} else {
				a.setPillError(true)
			}
		},
		OnLatency: func(line string) { a.logf("latency: %s", line) },
	}
}

// pillStatus maps a wire session state to the pill's header text, mirroring the phone card's
// wording (floating/FloatingPillView.kt).
func pillStatus(state string) string {
	switch state {
	case core.StateWarming:
		return "Warming up…"
	case core.StateProcessing:
		return "Finishing…"
	case core.StateListening:
		return "Listening…"
	case core.StateError:
		return "Error"
	default:
		return state
	}
}

func (a *app) testDictation() {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return
	}
	a.captureTarget()
	if err := c.StartDictation(a.ctx, core.ModeHold); err != nil {
		a.logf("test dictation start: %v", err)
		return
	}
	time.AfterFunc(5*time.Second, func() {
		a.mu.Lock()
		c := a.client
		a.mu.Unlock()
		if c == nil {
			return
		}
		if err := c.StopDictation(a.ctx); err != nil {
			a.logf("test dictation stop: %v", err)
		}
	})
}

// captureTarget records the window that should receive the finished text. It is called just before
// START_DICTATION, while the user's editor is still the foreground window.
func (a *app) captureTarget() {
	t := winplat.ForegroundWindow()
	a.mu.Lock()
	a.target = t
	a.mu.Unlock()
}

func (a *app) setDictating(v bool) {
	a.mu.Lock()
	a.dictating = v
	a.mu.Unlock()
}

func (a *app) isDictating() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dictating
}

// setPhoneState records the phone's advertised pipeline availability (SERVER_STATE, v1.1). It is
// advisory and deliberately has **no tray rendering yet**: the "phone taken" response belongs in the
// desktop pill window (4c/4d), which does not exist. It is stored so that window can read it, and
// logged so the state is observable meanwhile (CHUNK-8).
func (a *app) setPhoneState(s core.ServerStatePayload) {
	a.mu.Lock()
	a.phoneState = s
	a.mu.Unlock()
	if s.State == core.ServerStateIdle {
		a.logf("phone availability: idle")
	} else {
		a.logf("phone availability: %s (holder %q)", s.State, s.HolderName)
	}
}

// phoneAvailability returns the last advertised availability; the zero value (IDLE) before any
// frame arrives. Read by the future desktop pill window when it decides how to respond to a hotkey.
func (a *app) phoneAvailability() core.ServerStatePayload {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.phoneState
}

// tapWindow is how long a chord may be held before a release is treated as "hold" rather than
// "tap". Short enough that a deliberate hold registers almost immediately, long enough that a
// normal tap is not mistaken for one.
const tapWindow = 250 * time.Millisecond

// hotkeyLoop dispatches the watcher's chord edges for the configured mode. It runs for the lifetime
// of one armed watcher; a mode or chord change disarms and re-arms (see rearmHotkey).
func (a *app) hotkeyLoop(w *winplat.HotkeyWatcher, mode string, reset <-chan struct{}) {
	switch mode {
	case ModeToggle:
		for ev := range w.Events() {
			if ev == iface.Press { // RegisterHotKey reports Press only
				a.toggleDictation()
			}
		}
	case ModeHold:
		for ev := range w.Events() {
			switch ev {
			case iface.Press:
				if !a.isDictating() {
					a.startDictation()
				}
			case iface.Release:
				if a.isDictating() {
					a.stopDictation()
				}
			}
		}
	default: // ModeAuto
		a.smartHotkeyLoop(w, reset)
	}
}

// smartHotkeyLoop implements tap-to-toggle + hold-to-talk on one hotkey: dictation starts on the
// press either way, and the tap window only decides whether the release stops it. The timer lives
// here so the SmartMode machine is driven from a single goroutine. reset arrives when the session
// ended for a reason the machine did not initiate, so it can return to idle.
func (a *app) smartHotkeyLoop(w *winplat.HotkeyWatcher, reset <-chan struct{}) {
	sm := &SmartMode{}
	var timer *time.Timer
	var tick <-chan time.Time
	cancel := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		tick = nil
	}
	defer cancel()

	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			switch ev {
			case iface.Press:
				act, arm := sm.Press()
				a.applyAction(act)
				if arm {
					cancel()
					timer = time.NewTimer(tapWindow)
					tick = timer.C
				}
			case iface.Release:
				act, stop := sm.Release()
				if stop {
					cancel()
				}
				a.applyAction(act)
			}
		case <-tick:
			cancel()
			sm.TimerFired()
		case <-reset:
			cancel()
			sm.Reset()
		}
	}
}

func (a *app) applyAction(act Action) {
	switch act {
	case ActionStart:
		a.startDictation()
	case ActionStop:
		a.stopDictation()
	}
}

func (a *app) toggleDictation() {
	if a.isDictating() {
		a.stopDictation()
	} else {
		a.startDictation()
	}
}

func (a *app) startDictation() {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		a.signalSmartReset()
		return
	}
	a.captureTarget()
	if err := c.StartDictation(a.ctx, core.ModeHold); err != nil {
		a.logf("dictation start failed: %v", err)
		a.signalSmartReset()
	}
}

func (a *app) stopDictation() {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return
	}
	if err := c.StopDictation(a.ctx); err != nil {
		a.logf("dictation stop failed: %v", err)
	}
}

// inject writes the finished block with the configured method, falling back to the other method so
// text is never dropped silently. Focus is verified before anything is written. Paste is the default
// because it is fast and atomic for any length; typing leaves the clipboard untouched (D-120 amended
// 2026-10-07).
func (a *app) inject(text string) {
	a.mu.Lock()
	target := a.target
	insertion := a.settings.Insertion
	a.mu.Unlock()

	if err := a.insert(insertion, target, text); err == nil {
		a.logf("inserted %d chars via %s", len(text), insertion)
		return
	} else {
		a.logf("insertion via %s failed: %v", insertion, err)
	}

	other := InsertType
	if insertion == InsertType {
		other = InsertPaste
	}
	if err := a.insert(other, target, text); err == nil {
		a.logf("inserted %d chars via %s (fallback)", len(text), other)
		return
	} else {
		a.logf("insertion via %s failed: %v", other, err)
	}

	if err := winplat.CopyText(text); err != nil {
		a.logf("could not place text on the clipboard: %v", err)
	}
	a.setState(StateError, "insertion blocked; text on clipboard")
}

func (a *app) insert(method string, target uintptr, text string) error {
	if target != 0 && winplat.ForegroundWindow() != target {
		return winplat.ErrFocusChanged
	}
	if method == InsertType {
		return winplat.InjectText(0, text) // focus already verified
	}
	return winplat.PasteText(text)
}

// armHotkey starts the global hotkey watcher for the configured chord and mode. It is idempotent.
func (a *app) armHotkey() {
	a.mu.Lock()
	if a.armed {
		a.mu.Unlock()
		return
	}
	chord, err := iface.ParseHotkey(a.settings.Hotkey)
	mode := a.settings.Mode
	a.mu.Unlock()
	if err != nil {
		a.logf("hotkey %q is invalid: %v", a.settings.Hotkey, err)
		return
	}

	w, err := winplat.StartHotkey(chord, mode == ModeToggle)
	if err != nil {
		a.logf("hotkey unavailable: %v", err)
		a.setState(StateError, "hotkey unavailable")
		return
	}
	reset := make(chan struct{}, 1)
	a.mu.Lock()
	a.hotkey = w
	a.armed = true
	a.smartReset = reset
	a.mu.Unlock()
	a.logf("hotkey armed: %s (mode=%s)", chord, mode)
	go a.hotkeyLoop(w, mode, reset)
}

func (a *app) disarmHotkey() {
	a.mu.Lock()
	w := a.hotkey
	a.hotkey = nil
	a.armed = false
	a.smartReset = nil
	a.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

// signalSmartReset tells the smart-mode hotkey loop that the session ended for a reason it did not
// initiate, so its tap/hold state returns to idle and the next press starts a dictation instead of
// trying to stop a dead one. It never blocks: a pending reset already means the same thing.
func (a *app) signalSmartReset() {
	a.mu.Lock()
	ch := a.smartReset
	a.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// rearmHotkey applies a hotkey/mode change if the watcher is currently running.
func (a *app) rearmHotkey() {
	a.mu.Lock()
	armed := a.armed
	a.mu.Unlock()
	if armed {
		a.disarmHotkey()
		a.armHotkey()
	}
}

func (a *app) doPair() {
	a.mPair.Disable()
	a.setState(StateConnecting, "pairing")
	target := a.opts.Addr
	if target == "" {
		peer, err := core.Discover(a.ctx, a.opts.DiscoverTimeout)
		if err != nil {
			a.setState(StateError, "no phone found")
			a.mPair.Enable()
			winplat.InfoBox("Pairing failed", "Could not find the phone: "+err.Error())
			return
		}
		target = peer.Addr
	}
	a.logf("pairing with %s", target)

	creds, err := core.Pair(a.ctx, core.PairOptions{
		Addr:  target,
		Ident: a.opts.Identity,
		OnFingerprint: func(fp string) {
			a.logf("server fingerprint %s", fp)
			winplat.InfoBox("Sovereign pairing",
				"Confirm this fingerprint matches the phone's screen:\n\n"+fp+
					"\n\nThen tap Approve on the phone.")
		},
		Logf: a.logf,
	})
	if err != nil {
		a.setState(StateError, "pairing failed")
		a.mPair.Enable()
		winplat.InfoBox("Pairing failed", err.Error())
		return
	}
	if err := a.opts.Store.Save(creds); err != nil {
		a.setState(StateError, "could not save credentials")
		a.mPair.Enable()
		winplat.InfoBox("Pairing failed", err.Error())
		return
	}

	a.mu.Lock()
	a.creds, a.paired = creds, true
	a.mu.Unlock()

	a.mUnpair.Enable()
	a.setState(StateReady, "paired with "+creds.ServerName)
	a.logf("paired with %q", creds.ServerName)
	go a.connect()
}

func (a *app) doUnpair() {
	if !winplat.ConfirmBox("Unpair",
		"Delete this desktop's token and pin?\n\nThe phone keeps its paired-devices entry until you remove it there.") {
		return
	}
	if err := a.opts.Store.Delete(); err != nil {
		a.logf("unpair: %v", err)
		winplat.InfoBox("Unpair", err.Error())
		return
	}
	a.cancelConn()
	a.mu.Lock()
	a.creds, a.paired = core.Credentials{}, false
	a.mu.Unlock()

	a.mPair.Enable()
	a.mUnpair.Disable()
	a.setState(StateOffline, "not paired")
	a.logf("unpaired; credentials deleted")
}

func (a *app) setMode(m string) {
	a.mu.Lock()
	a.settings.Mode = m
	s := a.settings
	a.mu.Unlock()
	setChecked(a.mAuto, m == ModeAuto)
	setChecked(a.mHold, m == ModeHold)
	setChecked(a.mToggle, m == ModeToggle)
	a.persist(s)
	a.rearmHotkey()
}

// applyHotkey stores a hotkey (a preset or a captured custom one), updates the menu, and re-arms.
func (a *app) applyHotkey(spec string) {
	a.mu.Lock()
	a.settings.Hotkey = spec
	s := a.settings
	a.mu.Unlock()
	for k, item := range a.hotkeys {
		setChecked(item, k == spec)
	}
	if a.mHotkey != nil {
		a.mHotkey.SetTitle(hotkeyTitle(spec))
	}
	a.persist(s)
	a.rearmHotkey()
}

func (a *app) setInsertion(m string) {
	a.mu.Lock()
	a.settings.Insertion = m
	s := a.settings
	a.mu.Unlock()
	for k, item := range a.insertion {
		setChecked(item, k == m)
	}
	a.persist(s)
}

func (a *app) setPosition(p string) {
	a.mu.Lock()
	a.settings.OverlayPosition = p
	s := a.settings
	a.mu.Unlock()
	for k, item := range a.positions {
		setChecked(item, k == p)
	}
	a.persist(s)
}

// doSetHotkey captures the next key combination the user presses and binds it. The main watcher is
// disarmed first so the pressed chord does not also fire the current hotkey.
func (a *app) doSetHotkey() {
	winplat.InfoBox("Set hotkey",
		"Click OK, then press the key combination you want to dictate with.\n\n"+
			"Use at least one modifier (Ctrl/Alt/Shift/Win), or an F-key on its own.")

	a.disarmHotkey()
	w, err := winplat.StartHotkeyCapture()
	if err != nil {
		a.logf("hotkey capture failed: %v", err)
		a.armHotkey()
		return
	}
	defer func() {
		_ = w.Close()
		a.armHotkey()
	}()

	select {
	case c := <-w.Captured():
		if c.Mods == (iface.Modifiers{}) && !iface.IsFunctionKey(c.VK) {
			winplat.InfoBox("Set hotkey", "That key has no modifier, so it would be captured everywhere.\n\nUse at least one modifier, or an F-key.")
			return
		}
		chord := iface.NewChord(c.VK, c.Mods)
		a.applyHotkey(chord.String())
		a.logf("hotkey set to %s", chord)
	case <-time.After(15 * time.Second):
		winplat.InfoBox("Set hotkey", "No hotkey was captured; keeping the current one.")
	case <-a.ctx.Done():
	}
}

func (a *app) toggleStartup() {
	enable := !a.startupEnabled()
	if err := winplat.SetAutostart(a.exePath, enable); err != nil {
		a.logf("autostart: %v", err)
		winplat.InfoBox("Start with Windows", err.Error())
		return
	}
	if enable {
		a.mStartup.Check()
	} else {
		a.mStartup.Uncheck()
	}
	a.logf("autostart set to %v", enable)
}

func (a *app) startupEnabled() bool {
	return a.exePath != "" && winplat.AutostartEnabled(a.exePath)
}

func (a *app) persist(s Settings) {
	if err := SaveSettings(a.opts.SettingsPath, s); err != nil {
		a.logf("settings save failed: %v", err)
	}
}

// cancelConn tears down the live client and its connect goroutine, e.g. on unpair.
func (a *app) cancelConn() {
	a.mu.Lock()
	c := a.connCancel
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

func (a *app) credsSnapshot() core.Credentials {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.creds
}

// mapConnState maps the transport state to the tray state; the detail string carries the reason.
func mapConnState(st core.ConnState) TrayState {
	switch st {
	case core.StateConnecting:
		return StateConnecting
	case core.StateReady:
		return StateReady
	case core.StateAuthFailed:
		return StateError
	default:
		return StateOffline
	}
}

func statusTitle(s TrayState, detail string) string {
	if detail != "" {
		return stateLabel(s) + " — " + detail
	}
	return stateLabel(s)
}

func posLabel(p string) string {
	switch p {
	case PosBottomLeft:
		return "Bottom left"
	case PosTopRight:
		return "Top right"
	case PosTopLeft:
		return "Top left"
	default:
		return "Bottom right"
	}
}

func insertionLabel(m string) string {
	if m == InsertType {
		return "Type (keystrokes)"
	}
	return "Paste (fast)"
}

func hotkeyTitle(spec string) string { return "Hotkey: " + spec }

// setChecked is Check/Uncheck by value, for the radio-style menu groups.
func setChecked(item *systray.MenuItem, checked bool) {
	if checked {
		item.Check()
	} else {
		item.Uncheck()
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
