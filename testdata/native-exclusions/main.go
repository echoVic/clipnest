package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
)

// The launcher compiles this entrypoint alongside copied application files;
// only the picker's initial fixture directory differs from the application.
//
//go:embed web
var frontend embed.FS

const fixtureID = "app.clipnest.e2e.fixture"
const fixtureName = "Native E2E Fixture"
const historyText = "native e2e preserved history"
const phraseText = "native e2e preserved phrase"
const sentinelRevision = int64(-777)

type nativeCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type nativeReport struct {
	Mode   string        `json:"mode"`
	Checks []nativeCheck `json:"checks"`
	Error  string        `json:"error,omitempty"`
}

type nativeOutcome struct {
	report nativeReport
	err    error
}

type nativeDriver struct {
	hasText      func(uintptr, string) int32
	dump         func(uintptr, string)
	press        func(uintptr, string) int32
	observe      func(uintptr, string, int32) int32
	confirmation func() int32
	cancel       func(uintptr)
}

func openDriver(path string) (*nativeDriver, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	d := &nativeDriver{}
	purego.RegisterLibFunc(&d.hasText, lib, "e2e_has_text")
	purego.RegisterLibFunc(&d.dump, lib, "e2e_dump")
	purego.RegisterLibFunc(&d.press, lib, "e2e_press")
	purego.RegisterLibFunc(&d.observe, lib, "e2e_observe_confirmation")
	purego.RegisterLibFunc(&d.confirmation, lib, "e2e_confirmation")
	purego.RegisterLibFunc(&d.cancel, lib, "e2e_cancel")
	return d, nil
}

func main() {
	dir := flag.String("data-dir", "", "isolated acceptance data")
	driverPath := flag.String("driver", "", "acceptance AppKit driver")
	fixture := flag.String("fixture", "", "fixture application bundle")
	result := flag.String("result", "", "JSON result path")
	mode := flag.String("mode", "manage", "manage, restart, quit-open, quit-confirm")
	negative := flag.Bool("negative-control", false, "assert an intentionally wrong bundle ID")
	uiTimeout := flag.Duration("ui-timeout", 180*time.Second, "time allowed for real UI confirmation")
	flag.Parse()
	report := nativeReport{Mode: *mode, Checks: []nativeCheck{}}
	finish := func(err error) {
		if err != nil {
			report.Error = err.Error()
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(*result, data, 0600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if report.Error != "" {
			fmt.Fprintln(os.Stderr, report.Error)
			os.Exit(1)
		}
	}
	if err := os.WriteFile(*result+".pid", []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		finish(err)
		return
	}
	unlock, err := acquireDataLock(*dir)
	if err != nil {
		finish(err)
		return
	}
	defer unlock()
	store, err := openStorage(*dir)
	if err != nil {
		finish(err)
		return
	}
	defer store.Close()
	if *mode != "restart" {
		entry, err := newEntry(historyText, nil, "E2E")
		if err == nil {
			err = store.History.Add(entry)
		}
		if err == nil {
			_, err = store.Phrases.Save("", phraseText)
		}
		if err != nil {
			finish(err)
			return
		}
	}
	store.History.Pause(true)
	if err := store.History.Save(); err != nil {
		finish(err)
		return
	}
	view, err := store.History.OpenView()
	if err != nil {
		finish(err)
		return
	}
	defer view.Close()
	driver, err := openDriver(*driverPath)
	if err != nil {
		finish(err)
		return
	}
	d := &Desktop{h: store.History, p: store.Phrases, storage: store,
		view: view, session: 73, lastRevision: sentinelRevision, shortcut: "E2E"}
	ready := make(chan struct{})
	done := make(chan nativeOutcome, 1)
	mygo.App.SetName("Clipnest Native E2E")
	mygo.App.SetLanguages("zh-Hans")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		d.n, err = openNative()
		if err == nil {
			d.OpenPreferences()
		}
		close(ready)
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		d.quitting = true
		d.cancelExclusions()
	})
	go func() {
		<-ready
		workerReport := nativeReport{Mode: *mode, Checks: []nativeCheck{}}
		testErr := err
		if testErr == nil {
			testErr = exerciseNativeExclusions(d, driver, *fixture, *mode, *negative, *uiTimeout, *result+".phase.json", &workerReport)
		}
		if testErr != nil {
			_ = runExclusionsMain(mygo.RunOnMain, func() error {
				if d.preferences != nil {
					driver.dump(d.preferences.win.NativeHandle(), *result+".ui.txt")
				}
				return nil
			})
		}
		done <- nativeOutcome{report: workerReport, err: testErr}
		mygo.App.Quit()
	}()
	runErr := mygo.App.Run()
	select {
	case outcome := <-done:
		report = outcome.report
		finish(errors.Join(runErr, outcome.err))
	case <-time.After(5 * time.Second):
		// The worker may still be running. Its report is never read here.
		finish(errors.Join(runErr, errors.New("acceptance worker did not unblock after quit")))
	}
}

func exerciseNativeExclusions(d *Desktop, driver *nativeDriver, fixture, mode string, negative bool, uiTimeout time.Duration, phasePath string, report *nativeReport) error {
	check := func(name string, test func() error) error {
		err := test()
		item := nativeCheck{Name: name, Passed: err == nil}
		if err != nil {
			item.Detail = err.Error()
		}
		report.Checks = append(report.Checks, item)
		return err
	}
	main := func(work func() error) error { return runExclusionsMain(mygo.RunOnMain, work) }
	wait := func(name string, test func() bool) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			ok := false
			if err := main(func() error { ok = test(); return nil }); err != nil {
				return err
			}
			if ok {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: %w", name, ctx.Err())
			case <-ticker.C:
			}
		}
	}
	unchanged := func() error {
		if d.session != 73 || !d.h.IsPaused() {
			return errors.New("session or paused state changed")
		}
		current, err := d.h.OpenView()
		if err != nil {
			return err
		}
		defer current.Close()
		// A held SQLite snapshot can survive deletion of current history.
		// Verify both the existing session and freshly flushed storage.
		for _, view := range []*historyView{d.view, current} {
			page, err := view.Page("", "", 0, 50)
			if err != nil || page.Total != 1 {
				return fmt.Errorf("history changed: total=%d err=%v", page.Total, err)
			}
			entry, err := view.Detail(page.Entries[0].ID)
			if err != nil || entry.Text != historyText {
				return errors.New("history content changed")
			}
		}
		phrases, err := d.p.List("")
		if err != nil || len(phrases) != 1 {
			return errors.New("phrases changed")
		}
		phrase, err := d.p.Get(phrases[0].ID)
		if err != nil || phrase.Text != phraseText {
			return errors.New("phrase content changed")
		}
		return nil
	}
	parent := func() uintptr { return d.preferences.win.NativeHandle() }
	awaitConfirmation := func() error {
		data, err := json.Marshal(map[string]any{"mode": mode, "fixture": fixture, "action": "select-and-confirm"})
		if err != nil {
			return err
		}
		if err := os.WriteFile(phasePath, data, 0600); err != nil {
			return err
		}
		timer := time.NewTimer(uiTimeout)
		defer timer.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			switch driver.confirmation() {
			case 1:
				return nil
			case -1:
				return errors.New("native confirmation selected a different application")
			}
			select {
			case <-timer.C:
				return fmt.Errorf("real native confirmation was not observed within %s", uiTimeout)
			case <-ticker.C:
			}
		}
	}
	open := func() error {
		if err := wait("native add button ready", func() bool {
			return driver.press(parent(), "添加应用…") == 1
		}); err != nil {
			return err
		}
		if err := wait("native application sheet", func() bool { return d.appPicker != nil && d.appPicker.panel != 0 }); err != nil {
			return err
		}
		return main(func() error {
			quit := int32(0)
			if mode == "quit-confirm" {
				quit = 1
			}
			if driver.observe(d.appPicker.panel, fixture, quit) != 1 {
				return errors.New("cannot attach native confirmation observer")
			}
			return nil
		})
	}
	settle := func() error {
		return wait("preferences operation", func() bool {
			return !d.busy && !d.preferences.view.busy && d.appPicker == nil
		})
	}
	add := func() error {
		if err := open(); err != nil {
			return err
		}
		if err := awaitConfirmation(); err != nil {
			return err
		}
		return settle()
	}
	assertRule := func() error {
		expected := fixtureID
		if negative {
			expected += ".intentionally-wrong"
		}
		rules := d.ExcludedApps()
		if !slices.Equal(rules, []ExcludedApp{{BundleID: expected, Name: fixtureName}}) {
			return fmt.Errorf("confirmed selection mismatch: got=%v expected=%s", rules, expected)
		}
		persisted, err := openAppExclusions(filepath.Join(d.storage.dir, "excluded-apps.json"))
		if err != nil || !slices.Equal(persisted.List(), rules) {
			return errors.New("confirmed rule not persisted")
		}
		info, err := os.Stat(filepath.Join(d.storage.dir, "excluded-apps.json"))
		if err != nil || info.Mode().Perm() != 0600 {
			return errors.New("rule file permissions are not 0600")
		}
		if err := wait("confirmed rule rendered in native preferences", func() bool {
			return driver.hasText(parent(), fixtureName) == 1 && driver.hasText(parent(), fixtureID) == 1
		}); err != nil {
			var state string
			_ = main(func() error {
				state = fmt.Sprintf("view rules=%v busy=%t desktop busy=%t revision=%d notice=%q",
					d.preferences.view.state.ExcludedApps, d.preferences.view.busy, d.busy, d.lastRevision, d.preferences.view.notice)
				return nil
			})
			return fmt.Errorf("%w (%s)", err, state)
		}
		if err := main(func() error {
			if d.preferences.view.failed || driver.hasText(parent(), fixtureName) != 1 || driver.hasText(parent(), fixtureID) != 1 {
				return errors.New("native settings did not display the confirmed rule")
			}
			return nil
		}); err != nil {
			return err
		}
		return unchanged()
	}
	if err := wait("native preferences accessibility tree", func() bool {
		return driver.hasText(parent(), "隐私") == 1
	}); err != nil {
		return err
	}
	if err := main(func() error {
		if driver.press(parent(), "隐私") != 1 {
			return errors.New("native privacy tab was not pressed")
		}
		return nil
	}); err != nil {
		return err
	}
	if err := wait("native privacy tab", func() bool { return d.preferences.view.tab == 1 }); err != nil {
		return err
	}
	if mode == "restart" {
		if err := check("restart restores confirmed rule", func() error {
			if !slices.Equal(d.ExcludedApps(), []ExcludedApp{{BundleID: fixtureID, Name: fixtureName}}) {
				return errors.New("confirmed rule lost across process restart")
			}
			return main(func() error {
				if driver.hasText(parent(), fixtureID) != 1 || driver.hasText(parent(), fixtureName) != 1 {
					return errors.New("restored rule missing from native settings")
				}
				return nil
			})
		}); err != nil {
			return err
		}
		if err := check("confirming the same app does not duplicate or consume a revision", func() error {
			if err := add(); err != nil {
				return err
			}
			if err := assertRule(); err != nil {
				return err
			}
			return main(func() error {
				if d.lastRevision != sentinelRevision {
					return errors.New("duplicate confirmation consumed a clipboard revision")
				}
				return nil
			})
		}); err != nil {
			return err
		}
		return check("native remove clears disk and UI and consumes the revision", func() error {
			if err := main(func() error {
				if driver.press(parent(), "移除 "+fixtureName) != 1 {
					return errors.New("native remove button was not pressed")
				}
				return nil
			}); err != nil {
				return err
			}
			// Accessibility press queues a UI action; an idle flag alone can
			// still be true before the preferences runner begins that action.
			if err := wait("removed rule published", func() bool { return len(d.ExcludedApps()) == 0 }); err != nil {
				return err
			}
			if err := settle(); err != nil {
				return err
			}
			saved, err := openAppExclusions(filepath.Join(d.storage.dir, "excluded-apps.json"))
			if err != nil || len(saved.List()) != 0 || len(d.ExcludedApps()) != 0 {
				return errors.New("removed rule remained in storage")
			}
			if err := main(func() error {
				if driver.hasText(parent(), fixtureID) != 0 || driver.hasText(parent(), fixtureName) != 0 ||
					d.lastRevision != d.n.Revision() {
					return errors.New("removed rule remained in UI or did not consume current revision")
				}
				return nil
			}); err != nil {
				return err
			}
			return unchanged()
		})
	}
	if mode == "quit-open" || mode == "quit-confirm" {
		worker := make(chan error, 1)
		// Same preferences runner and bound method, with completion observed
		// independently so quit cannot leave a blocked acceptance goroutine.
		if err := main(func() error {
			d.preferences.view.actions.addExcludedApp = func() {
				d.preferences.run(func() error {
					_, err := d.AddExcludedApp()
					worker <- err
					return err
				}, "", false)
			}
			return nil
		}); err != nil {
			return err
		}
		return check(mode+" unblocks native add worker", func() error {
			if err := open(); err != nil {
				return err
			}
			if mode == "quit-confirm" {
				// The observer schedules native quit in the confirmation turn,
				// without retaining the panel or synthesizing its result.
				if err := awaitConfirmation(); err != nil {
					return err
				}
			} else {
				mygo.App.Quit()
			}
			select {
			case err := <-worker:
				if err != nil && err.Error() != "应用正在退出" {
					return fmt.Errorf("unexpected add error during quit: %w", err)
				}
				if mode == "quit-open" && err == nil {
					return errors.New("open picker did not return the quit error")
				}
				return unchanged()
			case <-time.After(5 * time.Second):
				return errors.New("native add worker hung during quit")
			}
		})
	}
	if err := check("cancel leaves rules and revision unchanged", func() error {
		if err := open(); err != nil {
			return err
		}
		if err := main(func() error { driver.cancel(d.appPicker.panel); return nil }); err != nil {
			return err
		}
		if err := settle(); err != nil {
			return err
		}
		if len(d.ExcludedApps()) != 0 || d.lastRevision != sentinelRevision {
			return errors.New("cancel changed rules or consumed a clipboard revision")
		}
		if _, err := os.Stat(filepath.Join(d.storage.dir, "excluded-apps.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("cancel created rule configuration")
		}
		return unchanged()
	}); err != nil {
		return err
	}
	return check("native file selection confirmation persists rule and consumes the revision", func() error {
		if err := add(); err != nil {
			return err
		}
		if err := assertRule(); err != nil {
			return err
		}
		return main(func() error {
			if d.lastRevision != d.n.Revision() {
				return errors.New("published rule did not consume the current clipboard revision")
			}
			return nil
		})
	})
}
