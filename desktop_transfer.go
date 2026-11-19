package main

import (
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/egoist/mygo"
)

// exportData writes the export file. Recording and panel edits go on.
func (d *Desktop) exportData(path string, secrets bool) (ExportSummary, error) {
	var shortcut string
	mygo.RunOnMain(func() { shortcut = d.shortcutAccelerator })
	return d.storage.Export(path, exportOptions{Secrets: secrets, Shortcut: shortcut, App: applicationVersion(), Now: time.Now()})
}

// importData imports a checked file the way a migration runs: recording,
// panel edits and opening the panel wait, and an open panel closes first,
// giving back its read transaction.
func (d *Desktop) importData(path string, withSettings bool) (ImportResult, error) {
	started := false
	mygo.RunOnMain(func() {
		if d.busy || d.quitting {
			return
		}
		d.busy, started = true, true
		if d.win != nil && d.win.IsVisible() {
			d.hide()
		}
	})
	if !started {
		return ImportResult{}, errors.New("正在处理其他操作，请稍后再试")
	}
	defer mygo.RunOnMain(func() { d.busy = false })
	privacy, apps := d.storage.Privacy.Settings(), d.ExcludedApps()
	result, err := d.storage.Import(path, withSettings)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(privacy, d.storage.Privacy.Settings()) || !slices.Equal(apps, d.ExcludedApps()) {
		// Like savePrivacy: the copy on the clipboard was judged by the old rules.
		mygo.RunOnMain(func() { d.lastRevision = d.n.Revision() })
	}
	if result.Shortcut != "" {
		if err := d.applyShortcut(result.Shortcut); err != nil {
			result.Problems = append(result.Problems, err.Error())
		}
	}
	return result, nil
}
