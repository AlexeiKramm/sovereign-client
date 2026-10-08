package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/alexeikramm/sovereign-client/internal/core"
	"github.com/alexeikramm/sovereign-client/internal/core/iface"
	"github.com/alexeikramm/sovereign-client/internal/ui/render"
	winplat "github.com/alexeikramm/sovereign-client/internal/windows"
)

// Options parameterises the tray application.
type Options struct {
	Identity     core.Identity
	Store        core.CredentialStore
	SettingsPath string // where settings.json lives
	Addr         string // optional manual host:port, bypassing discovery
	Pin          string // optional pin for Addr
	Logf         func(format string, args ...any)
}

// Run starts the tray and blocks until the user quits. It must be called from the main goroutine:
// systray owns the message loop.
func Run(opts Options) {
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

	mu            sync.Mutex
	creds         core.Credentials
	paired        bool
	client        *core.Client
	connecting    bool
	connCancel    context.CancelFunc
	attemptCancel context.CancelFunc      // cancels only the in-flight connect attempt, not the manager
	connState     core.ConnState          // last transport state reported by OnConn
	resolveFull   bool                    // the next resolve must run the full ladder (user-asked)
	reResolve     bool                    // the in-flight attempt was cancelled to re-resolve
	pending       bool                    // a hotkey/test is waiting for a link to dictate over
	pendingTest   bool                    // the pending dictation is the 5 s "Test dictation"
	resolveKick   chan struct{}           // buffered(1): wake the connect loop for an immediate resolve
	dictating     bool                    // a hotkey-initiated session is live
	busy          bool                    // the phone is taken by another capture (link is healthy)
	phoneState    core.ServerStatePayload // last unsolicited SERVER_STATE (v1.1); advisory
	target        uintptr                 // window to inject into, captured when dictation started
	hotkey        *winplat.HotkeyWatcher
	armed         bool
	smartReset    chan struct{} // signals the smart-mode loop that the session ended externally
	hotkeyCapture bool          // the panel is capturing a new hotkey
	hotkeyAbort   chan struct{} // cancels an in-flight capture (panel closed)
	startupOn     bool          // cached "Start with Windows" state, for the panel's toggle

	// Pill overlay (Chunk 4c). overlayOnce guards creation; pillMu guards the overlay handle, the
	// Esc watcher and the view, kept separate from mu so a client callback never nests locks.
	overlayOnce sync.Once
	pillMu      sync.Mutex
	overlay     *winplat.Overlay
	escWatcher  *winplat.HotkeyWatcher
	pill        pillView

	// History store for local dictation transcripts.
	history *HistoryStore

	// Control panel (Chunk 4d). panelOnce guards creation; panelMu guards the window, the view,
	// the last rendered layout (for hit-testing) and the drag/position state.
	panelOnce          sync.Once
	panelMu            sync.Mutex
	panelWin           *winplat.Overlay
	panelContent       panelView
	panelLayout        render.Panel
	panelPos           [2]int
	panelPlaced        bool
	panelUserMoved     bool
	panelLastH         int
	panelDragging      bool
	panelDragX         int
	panelDragY         int
	accentDropdownOpen bool
	panelActiveTab     int
	panelCopiedID      string

	// Panel footer telemetry, guarded by mu.
	lastAsrMs     int64
	lastCleanupMs int64
	lastLatency   string
	sessionCount  int

	// The tray is deliberately small: settings live in the panel (Chunk 4d-b).
	mStatus *systray.MenuItem
	mTest   *systray.MenuItem
	mPanel  *systray.MenuItem
	mPair   *systray.MenuItem
	mQuit   *systray.MenuItem
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
	a.startupOn = a.startupEnabled()
	a.mu.Unlock()

	histPath := ""
	if a.opts.SettingsPath != "" {
		histPath = filepath.Join(filepath.Dir(a.opts.SettingsPath), "history.json")
	} else if dir, err := core.DataDir(); err == nil {
		histPath = filepath.Join(dir, "history.json")
	}
	a.history = NewHistoryStore(histPath)

	a.resolveKick = make(chan struct{}, 1)
	a.buildMenu(paired)
	a.setState(StateOffline, "starting")
	a.logf("tray ready; paired=%v", paired)
	if paired {
		// The first connect runs the full ladder once to learn the phone's address. After that the
		// background only retries the remembered address, and the full ladder is user-asked.
		a.mu.Lock()
		a.resolveFull = true
		a.mu.Unlock()
	}
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
	a.mPanel = systray.AddMenuItem("Open control panel", "Live transcript and settings")

	systray.AddSeparator()
	a.mPair = systray.AddMenuItem("Pair with phone…", "Run mutual-accept pairing")

	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("Quit", "Exit Sovereign desktop")

	// The tray is deliberately small: dictation mode, hotkey, insertion, overlay position, Start
	// with Windows, unpair and quit live in the control panel (Chunk 4d-b).
	if paired {
		a.mPair.Disable()
	}

	a.onClicks(a.mTest, func() { go a.testDictation() })
	a.onClicks(a.mPanel, func() { a.openPanel() })
	a.onClicks(a.mPair, func() { go a.doPair() })
	a.onClicks(a.mQuit, a.quit)

	// systray reports a single left tap, not a double-click, so a left click opens the panel and
	// the right-click menu keeps everything else. The callback must not block the message loop.
	systray.SetOnTapped(func() { go a.openPanel() })
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
	a.closePanelWindow()
	a.cancel()
	systray.Quit()
}

// setState updates the icon, tooltip and status row. It is safe to call from any goroutine.
func (a *app) setState(s TrayState, detail string) {
	// Any state other than StateBusy means the phone is no longer marked taken.
	if s != StateBusy {
		a.mu.Lock()
		a.busy = false
		a.mu.Unlock()
	}

	if s == StateReady && detail == "" {
		detail = a.serverName()
	}

	title := statusTitle(s, detail)
	systray.SetIcon(trayIcon(s))
	systray.SetTooltip("Sovereign — " + title)
	if a.mStatus != nil {
		a.mStatus.SetTitle(title)
	}
	a.setPanelStatus(title)

	if a.mTest != nil {
		if s == StateReady || s == StateBusy {
			a.mTest.Enable()
		} else {
			a.mTest.Disable()
		}
	}

	// Hotkeys are live while there is a link to dictate over. A taken phone is still a live link —
	// the phone refuses the session, it does not break the connection — so the hotkey stays armed;
	// disarming it here would wedge dictation until a reconnect.
	if hotkeysLive(s) {
		a.armHotkey()
	} else {
		a.disarmHotkey()
	}
}

// hotkeysLive reports whether the tray state should have the dictation hotkey armed. The hotkey
// stays armed across states so pressing the hotkey when offline or busy displays immediate visual
// error feedback on the pill rather than dead input.
func hotkeysLive(s TrayState) bool {
	return true
}

// connect resolves the phone and runs the client until the link dies fatally or the app exits.
// Only one connect goroutine runs at a time. Discovery is on demand: a background retry only probes
// the remembered address (cheap and silent), while the full ladder — remembered address, short
// mDNS, then the subnet sweep — runs at startup and whenever a hotkey asks for a link.
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
		a.attemptCancel = nil
		a.mu.Unlock()
	}()

	for cctx.Err() == nil {
		a.mu.Lock()
		full := a.resolveFull
		a.resolveFull = false
		hint := a.settings.LastAddr
		a.mu.Unlock()

		target, pin, err := a.resolve(cctx, hint, full)
		if err != nil {
			if cctx.Err() != nil {
				return
			}
			a.logf("discovery failed: %v", err)
			if full {
				// A user asked for this resolve; report where they are looking rather than hiding
				// the failure in the tray.
				a.setState(StateError, "no phone found")
				a.failPendingDictation("No phone found — is the phone on this network?")
			} else {
				a.setState(StateOffline, "searching for the phone")
			}
			if !a.waitRetry(cctx) {
				return
			}
			continue
		}

		a.setLastAddr(target)

		client := core.NewClient(core.ClientConfig{
			Addr:     target,
			Pin:      pin,
			Ident:    a.opts.Identity,
			Creds:    a.credsSnapshot(),
			Handlers: a.handlers(),
			Logf:     a.logf,
		})
		actx, acancel := context.WithCancel(cctx)
		a.mu.Lock()
		a.client = client
		a.attemptCancel = acancel
		a.mu.Unlock()

		a.logf("connecting to wss://%s%s", target, core.Path)
		err = client.Run(actx)

		a.mu.Lock()
		a.client = nil
		a.attemptCancel = nil
		re := a.reResolve
		a.reResolve = false
		a.mu.Unlock()
		if cctx.Err() != nil {
			return
		}
		if re {
			// A hotkey asked for a fresh resolve: keep its deferred dictation pending and run the
			// ladder again from the top.
			continue
		}

		// Run reconnects internally, so returning means a fatal condition: a rejected identity or
		// a certificate mismatch. Surface it and stop; the user must re-pair or fix the network.
		var ae *core.AuthError
		switch {
		case errors.As(err, &ae):
			a.setState(StateError, "re-pair required")
			a.logf("authentication failed: %v", err)
			a.failPendingDictation("Re-pair required — the phone no longer recognises this desktop")
		case errors.Is(err, core.ErrPinMismatch):
			a.setState(StateError, "certificate pin mismatch")
			a.logf("pin mismatch: %v", err)
			a.failPendingDictation("Certificate pin mismatch — possible impersonation")
		default:
			a.setState(StateError, errString(err))
			a.logf("link ended: %v", err)
			a.failPendingDictation("Phone connection ended")
		}
		return
	}
}

// resolve returns the address and pin to dial: the manual override when set, else the discovery
// ladder. full selects the whole ladder (remembered address → short mDNS → subnet sweep); without
// it only the cheap remembered-address probe runs, so an idle client never multicasts or sweeps.
func (a *app) resolve(ctx context.Context, hint string, full bool) (addr, pin string, err error) {
	creds := a.credsSnapshot()
	if a.opts.Addr != "" {
		pin = a.opts.Pin
		if pin == "" {
			pin = creds.ServerSpki
		}
		return a.opts.Addr, pin, nil
	}

	var peer core.Peer
	if full {
		peer, err = core.Resolve(ctx, core.ResolveOptions{Hint: hint, Logf: a.logf})
	} else if hint != "" && core.Reachable(ctx, hint) {
		peer = core.Peer{Addr: hint, Port: core.DefaultPort, Via: core.ViaHint}
	} else {
		err = core.ErrNoPeer
	}
	if err != nil {
		return "", "", err
	}
	pin = creds.ServerSpki
	if pin == "" {
		pin = peer.Spki
	}
	return peer.Addr, pin, nil
}

// requestFullResolve asks the connect manager to run the full discovery ladder now. It is called
// only from the hotkey/test path, never on a timer. If a connect attempt is in flight it is
// cancelled so the manager re-resolves; otherwise the running backoff wait is woken.
func (a *app) requestFullResolve() {
	a.mu.Lock()
	a.resolveFull = true
	cancel := a.attemptCancel
	if cancel != nil {
		a.reResolve = true
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case a.resolveKick <- struct{}{}:
	default:
	}
}

// waitRetry blocks for the reconnect backoff, or until a hotkey asks for an immediate resolve. It
// returns false when the manager has been cancelled.
func (a *app) waitRetry(ctx context.Context) bool {
	t := time.NewTimer(3 * time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-a.resolveKick:
		return true
	case <-t.C:
		return true
	}
}

// setLastAddr remembers the address a successful resolve produced, so the next reconnect (and the
// next idle retry) can skip mDNS and the sweep.
func (a *app) setLastAddr(addr string) {
	a.mu.Lock()
	if a.settings.LastAddr == addr {
		a.mu.Unlock()
		return
	}
	a.settings.LastAddr = addr
	s := a.settings
	a.mu.Unlock()
	a.logf("remembered phone address %s", addr)
	a.persist(s)
}

// beginOnDemandDictation shows the pill immediately and asks the connect manager for a link, so a
// hotkey press never looks dead while the discovery ladder runs. It is called when a dictation is
// requested but no authenticated link is up.
func (a *app) beginOnDemandDictation() {
	a.mu.Lock()
	a.pending = true
	connecting := a.connecting
	a.mu.Unlock()

	a.showConnectingPill("Connecting…") // respond before any discovery happens

	a.requestFullResolve()
	if !connecting {
		go a.connect()
	}
}

// startPendingDictation runs the dictation a hotkey (or the 5 s test) deferred until the link came
// up. It is called from OnConn the moment the client reports Ready.
func (a *app) startPendingDictation() {
	a.mu.Lock()
	pending := a.pending
	a.pending = false
	test := a.pendingTest
	a.pendingTest = false
	c := a.client
	busy := a.busy
	phone := a.phoneState
	a.mu.Unlock()
	if !pending || c == nil {
		return
	}
	if busy {
		a.showErrorPill(busyMessage(phone.HolderName))
		a.signalSmartReset()
		return
	}
	if err := c.StartDictation(a.ctx, core.ModeHold); err != nil {
		a.logf("dictation start failed: %v", err)
		a.showErrorPill("Dictation start failed")
		a.signalSmartReset()
		return
	}
	if test {
		a.scheduleTestStop()
	}
}

// failPendingDictation ends a deferred dictation (a hotkey/test waiting for a link) with a visible
// error, so a failed connect is never silent.
func (a *app) failPendingDictation(msg string) {
	a.mu.Lock()
	pending := a.pending
	a.pending = false
	a.pendingTest = false
	a.mu.Unlock()
	if pending {
		a.showErrorPill(msg)
		a.signalSmartReset()
	}
}

func (a *app) handlers() core.Handlers {
	return core.Handlers{
		OnConn: func(st core.ConnState, detail string) {
			a.mu.Lock()
			a.connState = st
			pending := a.pending
			a.mu.Unlock()
			a.setState(mapConnState(st), detail)
			a.setPanelStatus(statusTitle(mapConnState(st), detail))
			if st != core.StateReady {
				// Keep the connecting pill up while a hotkey's deferred dictation still waits for
				// the link; a dropped link otherwise takes the pill down with it.
				if !pending {
					a.hidePill()
					a.signalSmartReset()
				}
				return
			}
			a.startPendingDictation()
		},
		OnStarted: func(sessionID string) {
			a.setDictating(true)
			a.setState(StateDictating, "listening")
			a.startPill()
			a.countSession()
		},
		OnState: func(sessionID, st string) {
			if st == core.StateIdle {
				a.setDictating(false)
				a.setState(StateReady, "")
				a.hideListeningPill()
				a.signalSmartReset()
			} else {
				a.setState(StateDictating, strings.ToLower(st))
				a.setPillStatus(pillStatus(st))
			}
		},
		OnBusy: func(holder string, takeover bool) {
			a.mu.Lock()
			a.pending = false
			a.pendingTest = false
			a.mu.Unlock()
			a.setDictating(false)
			// A refusal is not a link failure: mark the phone taken (the hotkey stays armed) and
			// show it where the user is looking, the way the phone's own pill marks a refused tap.
			a.setBusy(busyHolder(holder))
			a.hidePill()
			a.showBusyPill(busyMessage(holder))
			a.signalSmartReset()
		},
		OnServerState: func(s core.ServerStatePayload) {
			a.setPhoneState(s)
		},
		OnCommitted: func(sessionID, text string, perf core.CommitPerf) {
			a.logf("commit: %d chars (asr=%dms cleanup=%dms)", len(text), perf.AsrMs, perf.CleanupMs)
			a.debugf("commit text: %q", text)
			a.setLiveCommitted(text)
			a.recordCommitted(perf)
		},
		OnProvisional: func(sessionID, text string) {
			a.debugf("live: %q", text)
			a.setLiveProvisional(text)
		},
		// UTTERANCE_DONE is the only injection trigger (PROTOCOL.md §6), and only FINAL carries
		// text worth typing.
		OnUtterance: func(sessionID, delivery, raw, reason string) {
			a.setDictating(false)
			a.setState(StateReady, "")
			a.signalSmartReset()
			a.logf("utterance done (%s): %d chars", reason, len(delivery))
			a.debugf("utterance done (%s): %q", reason, delivery)
			if reason == core.DoneFinal && delivery != "" {
				a.mu.Lock()
				asr, cleanup := a.lastAsrMs, a.lastCleanupMs
				a.mu.Unlock()
				var words int
				if a.history != nil {
					it := a.history.Add(delivery, raw, asr, cleanup)
					words = it.Words
				} else {
					words = countWords(delivery)
				}
				a.refreshPanel()
				a.inject(delivery)
				a.showInsertedPill(words, delivery)
			} else {
				a.hidePill()
			}
		},
		OnReleased: func(sessionID, reason string) {
			a.setDictating(false)
			a.setState(StateReady, "")
			if reason == core.ReleaseCancelled {
				a.hidePill()
			} else {
				a.hideListeningPill()
			}
			a.signalSmartReset()
		},
		OnError: func(code, msg string, fatal bool) {
			a.logf("phone error: %s: %s (fatal=%v)", code, msg, fatal)
			if fatal {
				a.mu.Lock()
				a.pending = false
				a.pendingTest = false
				a.mu.Unlock()
				a.setDictating(false)
				a.setState(StateError, code)
				a.setPanelStatus(code)
				a.hidePill()
				a.signalSmartReset()
			} else {
				a.setLiveError(true)
			}
		},
		OnLatency: func(line string) {
			a.logf("latency: %s", line)
			a.recordLatency(line)
		},
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
	ready := a.connState == core.StateReady
	a.mu.Unlock()

	if c == nil || !ready {
		a.mu.Lock()
		a.pendingTest = true
		a.mu.Unlock()
		a.beginOnDemandDictation()
		return
	}
	a.captureTarget()
	if err := c.StartDictation(a.ctx, core.ModeHold); err != nil {
		a.logf("test dictation start: %v", err)
		return
	}
	a.scheduleTestStop()
}

// scheduleTestStop ends the 5 s "Test dictation" once its session is running.
func (a *app) scheduleTestStop() {
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

// setPhoneState records the phone's advertised pipeline availability (SERVER_STATE, v1.1), drives
// the tray's "phone busy" state from it, and repaints the control window's footer. It is advisory,
// so it is **not** used to short-circuit a hotkey: the reply to START_DICTATION stays authoritative
// (PROTOCOL.md §5), which avoids refusing a dictation over a stale "busy" from a session that has
// just ended. A refusal still shows up as the pill's busy message (OnBusy).
func (a *app) setPhoneState(s core.ServerStatePayload) {
	a.mu.Lock()
	a.phoneState = s
	a.mu.Unlock()
	if s.State == core.ServerStateIdle {
		a.logf("phone availability: idle")
	} else {
		a.logf("phone availability: %s (holder %q)", s.State, s.HolderName)
	}

	// The advertised availability is the only thing that reliably says when the phone frees again,
	// so it drives the tray's busy state in both directions. Our own session reports BUSY ("another
	// desktop"), which must not overwrite the dictating state.
	switch s.State {
	case core.ServerStateReserved, core.ServerStateBusy:
		if !a.isDictating() {
			a.setBusy(busyClause(s))
		}
	default:
		a.clearBusy()
	}
	a.refreshPanel()
}

// setBusy marks the phone as taken by another capture. The link is healthy, so this is not an error:
// the hotkey stays armed and the tray returns to Ready when SERVER_STATE reports idle.
func (a *app) setBusy(clause string) {
	a.mu.Lock()
	a.busy = true
	a.mu.Unlock()
	a.setState(StateBusy, clause)
}

// clearBusy returns the tray to Ready if the phone is still marked taken. It is a no-op otherwise,
// so a dictation that started in the meantime is not clobbered.
func (a *app) clearBusy() {
	a.mu.Lock()
	busy := a.busy
	a.busy = false
	a.mu.Unlock()
	if busy {
		a.setState(StateReady, "")
	}
}

// phoneAvailability returns the last advertised availability; the zero value (IDLE) before any
// frame arrives. The control window reads it for its footer.
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
	paired := a.paired
	c := a.client
	busy := a.busy
	phone := a.phoneState
	ready := a.connState == core.StateReady
	a.mu.Unlock()

	if !paired {
		a.showErrorPill("No phone paired — open Sovereign to pair")
		a.signalSmartReset()
		return
	}
	if busy {
		a.showErrorPill(busyMessage(phone.HolderName))
		a.signalSmartReset()
		return
	}

	// Capture the target window while the user's editor still holds focus. The pill is
	// non-activating, so focus is preserved while the discovery ladder runs.
	a.captureTarget()

	if c == nil || !ready {
		// Connect on demand: the pill appears now, and the session starts as soon as the link is up.
		a.beginOnDemandDictation()
		return
	}

	if err := c.StartDictation(a.ctx, core.ModeHold); err != nil {
		a.logf("dictation start failed: %v", err)
		a.showErrorPill("Dictation start failed")
		a.signalSmartReset()
	}
}

func (a *app) stopDictation() {
	a.mu.Lock()
	c := a.client
	pending := a.pending
	if pending {
		a.pending = false
		a.pendingTest = false
	}
	a.mu.Unlock()
	if pending && c == nil {
		// Released before the link came up: abandon the deferred dictation rather than start one
		// the user is no longer holding.
		a.hidePill()
		return
	}
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
	if a.mPair != nil {
		a.mPair.Disable()
	}
	a.setState(StateConnecting, "pairing")
	target := a.opts.Addr
	if target == "" {
		peer, err := core.Discover(a.ctx, core.MDNSTimeout)
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

	a.setState(StateReady, "paired with "+creds.ServerName)
	a.refreshPanel()
	a.logf("paired with %q", creds.ServerName)
	a.mu.Lock()
	a.resolveFull = true
	a.mu.Unlock()
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
	a.setState(StateOffline, "not paired")
	a.refreshPanel()
	a.logf("unpaired; credentials deleted")
}

func (a *app) setMode(m string) {
	a.mu.Lock()
	a.settings.Mode = m
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.rearmHotkey()
	a.refreshPanel()
}

// applyHotkey stores a hotkey (from the panel's capture) and re-arms the watcher.
func (a *app) applyHotkey(spec string) {
	a.mu.Lock()
	a.settings.Hotkey = spec
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.rearmHotkey()
	a.refreshPanel()
}

func (a *app) setInsertion(m string) {
	a.mu.Lock()
	a.settings.Insertion = m
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.refreshPanel()
}

func (a *app) setPosition(p string) {
	a.mu.Lock()
	a.settings.OverlayPosition = p
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.refreshPanel()
}

func (a *app) setThemeMode(m string) {
	a.mu.Lock()
	a.settings.ThemeMode = m
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.refreshPanel()
	a.repaintPill()
}

func (a *app) setThemePreset(p string) {
	a.mu.Lock()
	a.settings.ThemePreset = p
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	a.refreshPanel()
	a.repaintPill()
}

func (a *app) themePreset() string {
	a.mu.Lock()
	p := a.settings.ThemePreset
	a.mu.Unlock()
	if p == "" {
		return DefaultThemePreset
	}
	return p
}

func (a *app) isDarkTheme() bool {
	a.mu.Lock()
	mode := a.settings.ThemeMode
	a.mu.Unlock()
	switch mode {
	case ThemeDark:
		return true
	case ThemeLight:
		return false
	default:
		return winplat.IsSystemDarkTheme()
	}
}

func (a *app) toggleStartup() {
	enable := !a.startupEnabled()
	if err := winplat.SetAutostart(a.exePath, enable); err != nil {
		a.logf("autostart: %v", err)
		winplat.InfoBox("Start with Windows", err.Error())
		return
	}
	a.mu.Lock()
	a.startupOn = enable
	a.mu.Unlock()
	a.logf("autostart set to %v", enable)
	a.refreshPanel()
}

// toggleDebug flips transcript logging (the panel's "Log transcripts" toggle). Regular mode logs
// only metadata, never dictated text.
func (a *app) toggleDebug() {
	a.mu.Lock()
	a.settings.Debug = !a.settings.Debug
	on := a.settings.Debug
	s := a.settings
	a.mu.Unlock()
	a.persist(s)
	if on {
		a.logf("transcript logging enabled (debug)")
	} else {
		a.logf("transcript logging disabled")
	}
	a.refreshPanel()
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

func (a *app) serverName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.creds.ServerName
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

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// debugLogs reports whether transcript-bearing log lines are allowed: the persisted setting, or a
// truthy SOVEREIGN_DEBUG so the coding agent can force verbose logs for a headless run without
// changing the user's saved settings.
func (a *app) debugLogs() bool {
	a.mu.Lock()
	on := a.settings.Debug
	a.mu.Unlock()
	return on || envTruthy(os.Getenv("SOVEREIGN_DEBUG"))
}

// debugf logs only when transcript logging is enabled; regular mode never writes dictated text.
func (a *app) debugf(format string, args ...any) {
	if a.debugLogs() {
		a.logf(format, args...)
	}
}

// envTruthy reports whether an environment variable is set to a conventional affirmative value.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
