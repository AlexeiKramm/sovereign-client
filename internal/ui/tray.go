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

	mStatus   *systray.MenuItem
	mTest     *systray.MenuItem
	mPair     *systray.MenuItem
	mUnpair   *systray.MenuItem
	mHold     *systray.MenuItem
	mToggle   *systray.MenuItem
	mFallback *systray.MenuItem
	mStartup  *systray.MenuItem
	mQuit     *systray.MenuItem
	hotkeys   map[string]*systray.MenuItem
	positions map[string]*systray.MenuItem
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
	mode := systray.AddMenuItem("Dictation mode", "Hold-to-talk or press-to-toggle")
	a.mHold = mode.AddSubMenuItemCheckbox("Hold to talk", "", a.settings.Mode == ModeHold)
	a.mToggle = mode.AddSubMenuItemCheckbox("Press to toggle", "", a.settings.Mode == ModeToggle)

	hotkey := systray.AddMenuItem("Hotkey", "The global dictation hotkey")
	a.hotkeys = map[string]*systray.MenuItem{}
	for _, p := range HotkeyPresets {
		a.hotkeys[p] = hotkey.AddSubMenuItemCheckbox(p, "", p == a.settings.Hotkey)
	}

	a.mFallback = systray.AddMenuItemCheckbox("Clipboard fallback for blocked apps",
		"Paste instead when SendInput is refused (elevated windows)", a.settings.InjectionFallback)

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
	a.onClicks(a.mHold, func() { a.setMode(ModeHold) })
	a.onClicks(a.mToggle, func() { a.setMode(ModeToggle) })
	a.onClicks(a.mFallback, func() { a.setFallback(!a.settingsFallback()) })
	a.onClicks(a.mStartup, func() { a.toggleStartup() })
	a.onClicks(a.mQuit, a.quit)
	for p, item := range a.hotkeys {
		p, item := p, item
		a.onClicks(item, func() { a.setHotkey(p) })
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
		},
		OnStarted: func(sessionID string) {
			a.setState(StateDictating, "listening")
		},
		OnState: func(sessionID, st string) {
			if st == core.StateIdle {
				a.setState(StateReady, "")
			} else {
				a.setState(StateDictating, strings.ToLower(st))
			}
		},
		OnBusy: func(holder string, takeover bool) {
			a.setState(StateError, "phone busy ("+holder+")")
		},
		OnCommitted: func(sessionID, text string, perf core.CommitPerf) {
			a.logf("commit: %q (asr=%dms cleanup=%dms)", text, perf.AsrMs, perf.CleanupMs)
		},
		OnProvisional: func(sessionID, text string) {
			a.logf("live: %q", text)
		},
		OnUtterance: func(sessionID, delivery, raw, reason string) {
			a.setState(StateReady, "")
			a.logf("utterance done (%s): %q", reason, delivery)
		},
		OnReleased: func(sessionID, reason string) {
			a.setState(StateReady, "")
		},
		OnError: func(code, msg string, fatal bool) {
			a.logf("phone error: %s: %s (fatal=%v)", code, msg, fatal)
			if fatal {
				a.setState(StateError, code)
			}
		},
		OnLatency: func(line string) { a.logf("latency: %s", line) },
	}
}

func (a *app) testDictation() {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return
	}
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
	if m == ModeHold {
		a.mHold.Check()
		a.mToggle.Uncheck()
	} else {
		a.mToggle.Check()
		a.mHold.Uncheck()
	}
	a.persist(s)
}

func (a *app) setHotkey(p string) {
	a.mu.Lock()
	a.settings.Hotkey = p
	s := a.settings
	a.mu.Unlock()
	for k, item := range a.hotkeys {
		if k == p {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	a.persist(s)
}

func (a *app) setPosition(p string) {
	a.mu.Lock()
	a.settings.OverlayPosition = p
	s := a.settings
	a.mu.Unlock()
	for k, item := range a.positions {
		if k == p {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	a.persist(s)
}

func (a *app) setFallback(v bool) {
	a.mu.Lock()
	a.settings.InjectionFallback = v
	s := a.settings
	a.mu.Unlock()
	if v {
		a.mFallback.Check()
	} else {
		a.mFallback.Uncheck()
	}
	a.persist(s)
}

func (a *app) settingsFallback() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.InjectionFallback
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
