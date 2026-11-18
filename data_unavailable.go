package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// The choices when the data directory can't be used, by button index.
const (
	unavailableRetry = iota
	unavailableRepick
	unavailableDefault
	unavailableQuit
)

func unavailableMessage(dir string, problem error) mygo.MessageOptions {
	detail := "原因：" + problem.Error()
	if dir != "" {
		detail = "数据位置：" + dir + "\n" + detail
	}
	detail += "\n\nClipnest 不会在别处新建数据。改回默认位置后，原来的数据仍在原处，之后可以在偏好设置里用「更改…」接回来。"
	return mygo.MessageOptions{
		Type: mygo.MessageWarning, Title: "Clipnest",
		Message: "Clipnest 的数据目录无法使用", Detail: detail,
		Buttons:      []string{"重试", "重新选择…", "改回默认位置", "退出"},
		CancelButton: unavailableQuit,
	}
}

// repickDataDir records the folder picked at startup as the data directory,
// once it has opened there. confirmBlank asks before starting over in a
// folder without Clipnest data; recorded is false when the user declines.
func repickDataDir(picked, defaultDir string, confirmBlank func(dir string) bool) (recorded bool, err error) {
	t, err := resolveTarget(picked, "", defaultDir)
	if err != nil {
		return false, err
	}
	if !t.existing && !confirmBlank(t.dir) {
		return false, nil
	}
	if err := prepareDataDir(t.dir); err != nil {
		return false, err
	}
	return true, writeDataLocation(defaultDir, dataLocation{Dir: t.dir})
}

// unavailableNext is what follows a button of the dialog.
type unavailableNext int

const (
	unavailableAsk unavailableNext = iota // show the dialog again
	unavailableRestart
	unavailableQuitApp
)

// unavailableUI is what the dialog's buttons may ask besides.
type unavailableUI struct {
	pick         func(start string) (dir string, ok bool)
	confirmBlank func(dir string) bool
	showError    func(message string, err error)
}

// answerUnavailable carries out a button of the dialog about dir.
func answerUnavailable(button int, dir, defaultDir string, ui unavailableUI) unavailableNext {
	switch button {
	case unavailableRetry:
		return unavailableRestart
	case unavailableRepick:
		start := filepath.Dir(dir)
		if dir == "" {
			start, _ = os.UserHomeDir()
		}
		picked, ok := ui.pick(start)
		if !ok {
			return unavailableAsk
		}
		recorded, err := repickDataDir(picked, defaultDir, ui.confirmBlank)
		if err != nil {
			ui.showError("没能改用这个文件夹", err)
			return unavailableAsk
		}
		if recorded {
			return unavailableRestart
		}
		return unavailableAsk
	case unavailableDefault:
		if err := writeDataLocation(defaultDir, dataLocation{}); err != nil {
			ui.showError("没能改回默认位置", err)
			return unavailableAsk
		}
		return unavailableRestart
	default:
		return unavailableQuitApp
	}
}

// runDataDirUnavailable runs Clipnest only to ask what to do about a data
// directory it can't use, then restarts it or quits. It never creates that
// directory or writes the data elsewhere on its own.
func runDataDirUnavailable(dir string, problem error, defaultDir string) {
	ui := unavailableUI{
		pick: func(start string) (string, bool) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Title: "选择数据位置", ButtonLabel: "选择", Message: "选 Clipnest 数据所在的文件夹，或一个新文件夹从空白开始",
				Directory: true, CreateDirectories: true, DefaultPath: start,
			})
			if err != nil || len(paths) == 0 {
				return "", false
			}
			return paths[0], true
		},
		confirmBlank: confirmBlankDataDir,
		showError:    showDataDirError,
	}
	mygo.App.WhenReady(func() {
		go func() {
			mygo.App.Focus()
			for {
				res, err := mygo.Dialog.Message(unavailableMessage(dir, problem))
				if err != nil {
					log.Print("数据目录对话框无法显示：", err)
					mygo.App.Quit()
					return
				}
				switch answerUnavailable(res.Button, dir, defaultDir, ui) {
				case unavailableRestart:
					mygo.App.Relaunch()
					return
				case unavailableQuitApp:
					mygo.App.Quit()
					return
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func confirmBlankDataDir(dir string) bool {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type: mygo.MessageQuestion, Title: "Clipnest",
		Message: "这里没有 Clipnest 数据，要从空白开始吗？",
		Detail:  "Clipnest 会在「" + dir + "」新建数据并改用这里。",
		Buttons: []string{"从空白开始", "取消"}, CancelButton: 1,
	})
	return err == nil && res.Button == 0
}

func showDataDirError(message string, err error) {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageError, Title: "Clipnest", Message: message, Detail: err.Error()})
}
