package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"
)

// dataDirInfo is where main found the data.
type dataDirInfo struct {
	dir, defaultDir string
	fixed           bool   // --data-dir
	notice          string // what became of a move
}

func (d *Desktop) openDataDir() error { return mygo.Shell.OpenPath(d.data.dir) }

// dataMoveTarget resolves a folder picked in the preferences.
func (d *Desktop) dataMoveTarget(picked string) (moveTarget, error) {
	if d.data.fixed {
		return moveTarget{}, errors.New("数据位置由启动参数 --data-dir 指定，不能在这里更改")
	}
	return resolveTarget(picked, d.data.dir, d.data.defaultDir)
}

// changeDataDir moves the data to t, or switches to the data t already has,
// and restarts Clipnest there. Like a migration, it keeps recording and
// panel edits off while it copies.
func (d *Desktop) changeDataDir(t moveTarget) error {
	if d.data.fixed {
		return errors.New("数据位置由启动参数 --data-dir 指定，不能在这里更改")
	}
	if t.existing {
		// Data that won't open is never recorded.
		if err := prepareDataDir(t.dir); err != nil {
			return fmt.Errorf("这里的数据无法打开：%w", err)
		}
		if err := writeDataLocation(d.data.defaultDir, dataLocation{Dir: t.dir}); err != nil {
			return err
		}
		mygo.App.Relaunch()
		return nil
	}
	started := false
	mygo.RunOnMain(func() {
		if d.busy || d.quitting {
			return
		}
		d.busy, started = true, true
		// The panel gives back its read transaction, which holds the only
		// database connection.
		if d.win != nil && d.win.IsVisible() {
			d.hide()
		}
	})
	if !started {
		return errors.New("正在处理其他操作，请稍后再试")
	}
	err := d.storage.MoveTo(t.dir, func() error {
		return writeDataLocation(d.data.defaultDir, dataLocation{Dir: t.dir, Previous: d.data.dir})
	})
	if err != nil {
		mygo.RunOnMain(func() { d.busy = false })
		return err
	}
	mygo.App.Relaunch()
	return nil
}

// pickDataDir asks for a folder and then whether to move there.
func (p *preferencesWindow) pickDataDir() {
	if p.view.busy {
		return
	}
	start := filepath.Dir(p.desktop.data.dir)
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: p.win, Title: "选择数据位置", ButtonLabel: "选择",
			Message:   "选一个文件夹存放 Clipnest 的数据",
			Directory: true, CreateDirectories: true, DefaultPath: start,
		})
		if err != nil {
			p.win.Update(func() { p.view.notice, p.view.failed = err.Error(), true })
			return
		}
		if len(paths) > 0 {
			p.chooseDataDir(paths[0])
		}
	}()
}

// chooseDataDir resolves picked off the main thread and asks to move there.
func (p *preferencesWindow) chooseDataDir(picked string) {
	t, err := p.desktop.dataMoveTarget(picked)
	p.win.Update(func() {
		if err != nil {
			p.view.notice, p.view.failed = err.Error(), true
			return
		}
		p.view.notice = ""
		p.view.askMove(t)
	})
}

func (p *preferencesWindow) moveData(t moveTarget) {
	progress, done := "正在移动数据，完成后 Clipnest 会重启…", "数据已移动，正在重启…"
	if t.existing {
		progress, done = "正在重启…", "正在重启…"
	}
	if p.view.busy {
		return
	}
	p.run(func() error { return p.desktop.changeDataDir(t) }, done, false)
	if p.view.busy {
		p.view.notice, p.view.failed = progress, false
	}
}
