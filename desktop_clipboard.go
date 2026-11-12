package main

import (
	"errors"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// ClipboardAccess describes the system's policy, not a persisted app setting.
type ClipboardAccess string

const (
	clipboardUnsupported ClipboardAccess = "unsupported"
	clipboardDefault     ClipboardAccess = "default"
	clipboardAsk         ClipboardAccess = "ask"
	clipboardAllowed     ClipboardAccess = "allowed"
	clipboardDenied      ClipboardAccess = "denied"
	clipboardUnknown     ClipboardAccess = "unknown"
)

func (a ClipboardAccess) permitsRead(request bool) bool {
	switch a {
	case clipboardUnsupported, clipboardAllowed:
		return true
	case clipboardDefault, clipboardAsk:
		return request
	default:
		return false
	}
}

// Metadata queries must not materialize pasteboard contents.
type clipboardReader interface {
	Revision() int64
	ClipboardAccess() ClipboardAccess
	AvailableFormats() []string
	Formats() []transfer.Format
	ReadFormat(transfer.Format) ([]byte, error)
	ReadImage() []byte
	Target() targetApp
}

type systemClipboard struct {
	*nativeBridge
	mygo.ClipboardModule
}

// ClipboardPermission checks policy without reading clipboard contents.
func (d *Desktop) ClipboardPermission() (access ClipboardAccess) {
	mygo.RunOnMain(func() {
		d.clipboardAccess = d.n.ClipboardAccess()
		access = d.clipboardAccess
	})
	return
}

func (d *Desktop) OpenClipboardPermission() error {
	if runtime.GOOS != "darwin" {
		return errors.New("剪贴板记录权限设置仅适用于 macOS")
	}
	// Open the known privacy pane; its clipboard section varies by OS version.
	return mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security")
}

// A user-initiated recording action lets the OS ask for programmatic access.
// Permission belongs to macOS; a successful one-time read is not an Allow grant.
func (d *Desktop) RequestClipboardPermission() (s PanelState, err error) {
	mygo.RunOnMain(func() {
		visible := d.win != nil && d.win.IsVisible()
		if d.preferences != nil && d.preferences.win.IsVisible() {
			visible = true
		}
		if d.busy || d.quitting || !visible {
			err = errors.New("请打开面板并等待当前操作完成")
			return
		}
		if d.h.IsPaused() {
			err = errors.New("请先恢复记录，再请求剪贴板权限")
			return
		}
		d.busy = true
		defer func() { d.busy = false }()
		var captured bool
		captured, err = d.captureFrom(systemClipboard{d.n, mygo.Clipboard}, true)
		s = d.snapshot()
		if err != nil {
			err = errors.New("剪贴板读取未成功，请检查系统中的记录权限")
		} else if d.clipboardAccess == clipboardDenied {
			err = errors.New("系统已拒绝剪贴板读取，请在系统设置中改为「允许」")
		} else if !captured && d.clipboardAccess != clipboardAllowed {
			err = errors.New("请在其他应用复制一段普通文字，再点「开始记录」")
		}
		if captured && d.win != nil && d.win.IsVisible() {
			s = d.reload()
		}
	})
	return
}

// Notices for transient failures. Each clears once its own operation
// succeeds again.
const (
	captureFailedNotice = "剪贴板暂时无法读取，请检查记录权限后重新复制"
	saveFailedNotice    = "历史暂时无法保存，请检查磁盘空间与目录权限"
	cleanupFailedNotice = "历史自动清理失败，请检查磁盘空间与目录权限"
)

// showFailure shows notice after its operation fails and clears it after the
// operation succeeds, but only while the banner still shows it: a different
// notice set in between is not the operation's to clear. It reports whether
// the banner changed. Main thread.
func (d *Desktop) showFailure(notice string, failed bool) bool {
	next := d.notice
	if failed {
		next = notice
	} else if next == notice {
		next = ""
	}
	if next == d.notice {
		return false
	}
	d.notice = next
	if !d.quitting && d.win != nil && d.win.IsVisible() {
		_ = panelChanged.Broadcast(d.snapshot())
	}
	return true
}

// reportFailure records whether an operation failed and reports whether the
// main thread has to hear of it: every failure, so that a notice replaced in
// the meantime returns while the failure lasts, and the first success after
// one. The goroutine running the operation keeps failed, so an operation that
// keeps succeeding never waits for the main thread.
func reportFailure(failed *bool, err error) bool {
	was := *failed
	*failed = err != nil
	return was || *failed
}

func (d *Desktop) capture() { d.captureWith(systemClipboard{d.n, mygo.Clipboard}) }

// captureWith is capture with the clipboard as a parameter, so tests can
// substitute it. A capture without an error, recorded or not, is a success.
func (d *Desktop) captureWith(c clipboardReader) {
	before := d.clipboardAccess
	_, err := d.captureFrom(c, false)
	// A changed banner has already been broadcast, with the policy in it.
	if !d.showFailure(captureFailedNotice, err != nil) && before != d.clipboardAccess && d.win != nil && d.win.IsVisible() {
		_ = panelChanged.Broadcast(d.snapshot())
	}
	if before != d.clipboardAccess && d.preferences != nil {
		d.preferences.refresh()
	}
}

// Only an explicit permission action may read while the OS would ask.
// Recheck the policy even when changeCount is unchanged, so a Settings change
// takes effect without waiting for another copy.
func (d *Desktop) captureFrom(c clipboardReader, request bool) (bool, error) {
	d.clipboardAccess = c.ClipboardAccess()
	rev := c.Revision()
	if d.h.IsPaused() {
		d.lastRevision = rev
		return false, nil
	}
	if rev >= 0 && rev == d.lastRevision {
		return false, nil
	}
	// Cache metadata even while policy prevents a read. Opening settings later
	// must not turn the same copy into content attributed to Clipnest.
	if !d.sourceObserved || rev < 0 || rev != d.observedRevision {
		d.observedSource, d.observedRevision, d.sourceObserved = c.Target(), rev, true
	}
	if !d.clipboardAccess.permitsRead(request) {
		return false, nil
	}
	source := d.observedSource
	if runtime.GOOS == "darwin" && d.storage != nil {
		rules := d.storage.Exclusions
		if rules.Contains(source.BundleID) || (source.BundleID == "" && len(rules.List()) > 0) {
			d.lastRevision = rev
			return false, nil
		}
	}
	formats := c.AvailableFormats()
	for _, f := range formats {
		if slices.Contains(concealedFormats, f) {
			d.lastRevision = rev
			return false, nil
		}
	}
	var e Entry
	var err error
	portable := c.Formats()
	if slices.Contains(portable, transfer.FileList) {
		d.lastRevision = rev
		return false, nil
	}
	// Metadata queries can race a new copy or a permission change. Check again
	// immediately before the operation that can trigger a system prompt.
	d.clipboardAccess = c.ClipboardAccess()
	if !d.clipboardAccess.permitsRead(request) || (rev >= 0 && c.Revision() != rev) {
		return false, nil
	}
	if slices.Contains(portable, transfer.Text) {
		data, readErr := c.ReadFormat(transfer.Text)
		d.clipboardAccess = c.ClipboardAccess()
		if readErr != nil {
			return false, readErr
		}
		e, err = newEntry(string(data), nil, source.Name)
	} else if slices.Contains(portable, transfer.PNG) || slices.Contains(formats, "public.tiff") {
		data := c.ReadImage()
		d.clipboardAccess = c.ClipboardAccess()
		e, err = newEntry("", data, source.Name)
	} else {
		d.lastRevision = rev
		return false, nil
	}
	if !d.clipboardAccess.permitsRead(request) || (rev >= 0 && c.Revision() != rev) {
		return false, nil
	}
	d.lastRevision = rev
	if err != nil {
		return false, nil
	}
	if rev < 0 && e.ID == d.lastID {
		return false, nil
	}
	d.lastID = e.ID
	if err := d.h.Add(e); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Desktop) monitor() {
	// Owned by this goroutine: a save that keeps succeeding every two seconds
	// never waits for the main thread.
	var saveFailed, cleanupFailed bool
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	save := time.NewTicker(2 * time.Second)
	defer save.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			mygo.RunOnMain(func() {
				if !d.quitting && !d.busy {
					d.capture()
				}
			})
		case <-save.C:
			if reportFailure(&saveFailed, d.h.Save()) {
				failed := saveFailed
				mygo.RunOnMain(func() { d.showFailure(saveFailedNotice, failed) })
			}
		case <-cleanup.C:
			if reportFailure(&cleanupFailed, d.h.Cleanup()) {
				failed := cleanupFailed
				mygo.RunOnMain(func() { d.showFailure(cleanupFailedNotice, failed) })
			}
		}
	}
}
