package main

import (
	"errors"
	"runtime"
	"slices"
	"sync"

	"github.com/egoist/mygo"
)

type applicationPicker struct {
	panel, parent uintptr
	result        chan string
	once          sync.Once
}

func (p *applicationPicker) resolve(path string) {
	p.once.Do(func() { p.result <- path })
}

// Main-thread-only: retire the handle before a waiting worker can dispatch
// follow-up work or quitting can attempt to cancel the completed sheet.
func (p *applicationPicker) complete(path string) uintptr {
	panel := p.panel
	p.panel = 0
	p.resolve(path)
	return panel
}

func (d *Desktop) cancelExclusions() {
	if d.appPicker != nil {
		d.n.closeApplicationPicker(d.appPicker)
		d.appPicker = nil
	}
}

func (d *Desktop) ExcludedApps() []ExcludedApp { return d.storage.Exclusions.List() }

func runExclusionsMain(dispatch func(func()), work func() error) error {
	err := errors.New("应用正在退出")
	dispatch(func() { err = work() })
	return err
}

func (d *Desktop) beginExclusions() (parent *mygo.Window, err error) {
	err = runExclusionsMain(mygo.RunOnMain, func() error {
		if runtime.GOOS != "darwin" {
			return errors.New("排除应用仅适用于 macOS")
		}
		if d.busy || d.quitting {
			return errors.New("正在处理其他操作，请稍后")
		}
		if d.preferences != nil && d.preferences.win.IsVisible() {
			parent = d.preferences.win
		} else if d.win != nil && d.win.IsVisible() {
			parent = d.win
		} else {
			return errors.New("请先打开偏好设置")
		}
		d.busy = true
		return nil
	})
	return
}

func (d *Desktop) finishExclusions(before []ExcludedApp) {
	after := d.ExcludedApps()
	mygo.RunOnMain(func() {
		// Even a post-rename sync error can publish a new rule. Consume the
		// current copy on any published change, without rebuilding the panel.
		if !slices.Equal(before, after) {
			d.lastRevision = d.n.Revision()
		}
		d.busy = false
		if !d.quitting && d.preferences != nil {
			d.preferences.refresh()
		}
	})
}

func (d *Desktop) AddExcludedApp() ([]ExcludedApp, error) {
	parent, err := d.beginExclusions()
	if err != nil {
		return d.ExcludedApps(), err
	}
	before := d.ExcludedApps()
	defer d.finishExclusions(before)
	var picker *applicationPicker
	err = runExclusionsMain(mygo.RunOnMain, func() error {
		if d.quitting {
			return errors.New("应用正在退出")
		}
		var pickerErr error
		picker, pickerErr = d.n.openApplicationPicker(parent.NativeHandle())
		d.appPicker = picker
		return pickerErr
	})
	if err != nil {
		return d.ExcludedApps(), err
	}
	path := <-picker.result
	var app ExcludedApp
	err = runExclusionsMain(mygo.RunOnMain, func() error {
		d.appPicker = nil
		if d.quitting {
			return errors.New("应用正在退出")
		}
		if path != "" {
			var metadataErr error
			app, metadataErr = d.n.Application(path)
			return metadataErr
		}
		return nil
	})
	if err == nil && path != "" {
		err = d.storage.Exclusions.Add(app)
	}
	return d.ExcludedApps(), err
}

func (d *Desktop) RemoveExcludedApp(bundleID string) ([]ExcludedApp, error) {
	if _, err := d.beginExclusions(); err != nil {
		return d.ExcludedApps(), err
	}
	before := d.ExcludedApps()
	defer d.finishExclusions(before)
	err := d.storage.Exclusions.Remove(bundleID)
	return d.ExcludedApps(), err
}
