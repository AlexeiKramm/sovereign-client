// Command sovereign-desktop is the Windows tray client for the Sovereign phone server.
//
// With no flags it runs the tray app: single instance, connect/reconnect to the phone, tray state,
// and the settings shell. The flags below drive the same transport headlessly for development and
// verification; hotkeys, injection and the pill overlay arrive with the rest of Chunk 4.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/alexeikramm/sovereign-desktop/internal/core"
	"github.com/alexeikramm/sovereign-desktop/internal/ui"
	winplat "github.com/alexeikramm/sovereign-desktop/internal/windows"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sovereign-desktop: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		addr     = flag.String("addr", "", "phone host:port; manual connect, bypasses mDNS")
		pin      = flag.String("pin", "", "server SPKI fingerprint to pin (with -addr)")
		token    = flag.String("token", "", "bearer token, overriding stored credentials")
		pair     = flag.Bool("pair", false, "pair with the phone at -addr, store credentials, and exit")
		unpair   = flag.Bool("unpair", false, "delete stored credentials and exit")
		discover = flag.Bool("discover", false, "browse mDNS once, print any phone found, and exit")
		once     = flag.Bool("once", false, "connect, dictate for -seconds, print the result, and exit")
		seconds  = flag.Float64("seconds", 5, "dictation duration for -once")
		timeout  = flag.Duration("timeout", 5*time.Second, "mDNS discovery timeout")
		verbose  = flag.Bool("v", false, "log session events")
		tray     = flag.Bool("tray", false, "run the tray app (the default when no other flags are given)")
	)
	flag.Parse()

	idPath, err := core.DefaultIdentityPath()
	if err != nil {
		return err
	}
	ident, err := core.LoadOrCreateIdentity(idPath)
	if err != nil {
		return err
	}
	store, err := core.DefaultCredentialStore()
	if err != nil {
		return err
	}

	logf := func(format string, args ...any) {
		fmt.Printf("[core] "+format+"\n", args...)
	}

	switch {
	case *unpair:
		if err := store.Delete(); err != nil {
			return err
		}
		fmt.Println("credentials deleted; the phone still holds the pairing until you unpair there")
		return nil

	case *discover:
		return runDiscover(*timeout)

	case *pair:
		if *addr == "" {
			return errors.New("-pair needs -addr host:port")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		creds, err := core.Pair(ctx, core.PairOptions{
			Addr:  *addr,
			Ident: ident,
			OnFingerprint: func(fp string) {
				fmt.Printf("server fingerprint: %s\n", fp)
			},
			Logf: logf,
		})
		if err != nil {
			return err
		}
		if err := store.Save(creds); err != nil {
			return err
		}
		fmt.Printf("paired with %q (%s); credentials stored at %s\n", creds.ServerName, creds.ServerID, store.Path)
		return nil
	}

	if *tray || flag.NFlag() == 0 {
		return runTray(ident, store, *addr, *pin, *timeout)
	}

	return runClient(*addr, *pin, *token, *once, *seconds, *timeout, *verbose, ident, store, logf)
}

// runTray starts the tray application, refusing to run a second instance in the same session.
func runTray(ident core.Identity, store core.CredentialStore, addr, pin string, timeout time.Duration) error {
	// Claim the single-instance mutex before touching the log: a second launch must not truncate
	// the running instance's log.
	already, release, err := winplat.AcquireSingleInstance(winplat.SingleInstanceMutexName)
	if err != nil {
		return err
	}
	defer release()
	if already {
		return errors.New("sovereign-desktop is already running; use its tray icon")
	}

	logger, closeLog, err := ui.OpenLog()
	if err != nil {
		return err
	}
	defer closeLog()

	dir, err := core.DataDir()
	if err != nil {
		return err
	}
	logger.Printf("starting tray (addr=%q pin=%q)", addr, pin)
	ui.Run(ui.Options{
		Identity:        ident,
		Store:           store,
		SettingsPath:    filepath.Join(dir, "settings.json"),
		Addr:            addr,
		Pin:             pin,
		DiscoverTimeout: timeout,
		Logf:            logger.Printf,
	})
	return nil
}

func runDiscover(timeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	peer, err := core.Discover(ctx, timeout)
	if err != nil {
		return fmt.Errorf("discovery failed (%w); pass -addr host:port instead", err)
	}
	fmt.Printf("found via: %s\n", peer.Via)
	fmt.Printf("instance:  %s\n", peer.Instance)
	fmt.Printf("name:      %s\n", peer.Name)
	fmt.Printf("deviceId:  %s\n", peer.DeviceID)
	fmt.Printf("addr:      %s\n", peer.Addr)
	fmt.Printf("protover:  %d\n", peer.Protover)
	fmt.Printf("spki:      %s\n", peer.Spki)
	fmt.Printf("paired:    %v\n", peer.Paired)
	return nil
}

func runClient(addr, pin, token string, once bool, seconds float64, timeout time.Duration, verbose bool, ident core.Identity, store core.CredentialStore, logf func(string, ...any)) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	creds, _, err := store.Load()
	if err != nil {
		return err
	}
	if token != "" {
		creds.Token = token
	}
	if pin != "" {
		creds.ServerSpki = pin
	}

	target := addr
	if target == "" {
		peer, err := core.Discover(ctx, timeout)
		if err != nil {
			return fmt.Errorf("discovery failed (%w); pass -addr host:port", err)
		}
		target = peer.Addr
		if creds.ServerSpki == "" {
			creds.ServerSpki = peer.Spki
		}
		if peer.Via == core.ViaProbe {
			fmt.Printf("found phone at %s by LAN probe (mDNS found nothing)\n", peer.Addr)
		} else {
			fmt.Printf("discovered %q at %s\n", peer.Name, peer.Addr)
		}
	}
	if creds.ServerSpki == "" {
		if target != "" {
			return fmt.Errorf("phone at %s is not pinned yet; run: sovereign-desktop -pair -addr %s", target, target)
		}
		return errors.New("no pin available: pass -pin, run -pair first, or use -discover")
	}
	if creds.Token == "" {
		fmt.Println("warning: no token; the phone will answer AUTH_FAIL unless it was paired")
	}

	var (
		client    *core.Client
		startOnce sync.Once
		doneOnce  sync.Once
		done      = make(chan struct{})
	)

	h := core.Handlers{
		OnConn: func(state core.ConnState, detail string) {
			if detail != "" {
				fmt.Printf("[conn] %s: %s\n", state, detail)
			} else {
				fmt.Printf("[conn] %s\n", state)
			}
			if state == core.StateReady && once {
				startOnce.Do(func() {
					if err := client.StartDictation(ctx, core.ModeHold); err != nil {
						fmt.Printf("[once] start failed: %v\n", err)
						doneOnce.Do(func() { close(done) })
						return
					}
					go func() {
						select {
						case <-time.After(time.Duration(seconds * float64(time.Second))):
							if err := client.StopDictation(ctx); err != nil {
								fmt.Printf("[once] stop failed: %v\n", err)
							}
						case <-done:
						}
					}()
				})
			}
		},
		OnUtterance: func(sessionID, delivery, raw, reason string) {
			fmt.Printf("[utterance] reason=%s\n", reason)
			fmt.Printf("delivery: %s\n", delivery)
			doneOnce.Do(func() { close(done) })
		},
		OnLatency: func(line string) { fmt.Printf("[latency] %s\n", line) },
	}
	if verbose {
		h.OnStarted = func(id string) { fmt.Printf("[started] %s\n", id) }
		h.OnBusy = func(name string, canTakeover bool) {
			fmt.Printf("[busy] held by %s (takeover=%v)\n", name, canTakeover)
		}
		h.OnServerState = func(s core.ServerStatePayload) {
			fmt.Printf("[phone] %s (holder %q)\n", s.State, s.HolderName)
		}
		h.OnState = func(id, state string) { fmt.Printf("[state] %s\n", state) }
		h.OnCommitted = func(id, text string, perf core.CommitPerf) {
			fmt.Printf("[commit] %q (asr=%dms cleanup=%dms)\n", text, perf.AsrMs, perf.CleanupMs)
		}
		h.OnProvisional = func(id, text string) { fmt.Printf("[live] %q\n", text) }
		h.OnError = func(code, msg string, fatal bool) { fmt.Printf("[error] %s: %s (fatal=%v)\n", code, msg, fatal) }
	}

	client = core.NewClient(core.ClientConfig{
		Addr:     target,
		Pin:      creds.ServerSpki,
		Ident:    ident,
		Creds:    creds,
		Handlers: h,
		Logf:     logf,
	})

	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()

	fmt.Printf("connecting to wss://%s%s\n", target, core.Path)
	select {
	case <-done:
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}
