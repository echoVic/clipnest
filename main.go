package main

import (
	"bytes"
	"embed"
	"flag"
	"github.com/egoist/mygo"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed web
var frontend embed.FS

func trayIcon() []byte {
	im := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	c := color.NRGBA{104, 83, 220, 255}
	if runtime.GOOS == "darwin" {
		c = color.NRGBA{0, 0, 0, 255}
	}
	for y := 4; y < 28; y++ {
		for x := 5; x < 27; x++ {
			if (x < 8 || x >= 24 || y < 7 || y >= 25) || (y >= 12 && y <= 14 && x >= 11 && x < 21) || (y >= 18 && y <= 20 && x >= 11 && x < 18) {
				im.SetNRGBA(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	return b.Bytes()
}
func main() {
	webAddress := flag.String("web", "", "启动仅含示例数据的浏览器交互演示")
	dataDir := flag.String("data-dir", "", "历史存储目录")
	flag.Parse()
	web, _ := fs.Sub(frontend, "web")
	if *webAddress != "" {
		host, _, err := net.SplitHostPort(*webAddress)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("浏览器演示仅支持回环地址")
		}
		s := http.Server{Addr: *webAddress, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			http.FileServer(http.FS(web)).ServeHTTP(w, r)
		})}
		log.Print("Clipnest 交互演示已启动（示例数据，不读取系统剪贴板）")
		log.Fatal(s.ListenAndServe())
		return
	}
	mygo.App.SetName("Clipnest")
	mygo.App.SetVersion("0.1.1")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	if os.Getenv("MYGO_GENERATE") != "" {
		mygo.Bind(&Desktop{})
		if err := mygo.App.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	dir := *dataDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(base, "Clipnest")
	}
	h, err := openHistory(filepath.Join(dir, "history.json"))
	if err != nil {
		log.Fatal(err)
	}
	d := &Desktop{h: h, lastRevision: -2, stop: make(chan struct{}), shortcut: "⌘ ⇧ V"}
	mygo.SetFrontend(web)
	mygo.Bind(d)
	mygo.App.WhenReady(func() {
		d.n, err = openNative()
		if err != nil {
			log.Fatal("无法初始化系统剪贴板：", err)
		}
		d.win = mygo.NewWindow(mygo.WindowOptions{Title: "Clipnest · 剪贴板历史", URL: "/", Width: 860, Height: 570, Hidden: true, Frameless: true, Transparent: true, BackgroundColor: "#00000000", DisableResize: true, DisableMaximize: true, DisableMinimize: true, DisableFullScreen: true, AlwaysOnTop: true, SkipTaskbar: true})
		d.win.SetMenu(mygo.NewMenu(nil))
		d.win.OnBlur(func() {
			if !d.busy {
				d.win.Hide()
			}
		})
		d.win.OnClose(func(e *mygo.CloseEvent) {
			if !d.quitting {
				e.PreventDefault()
				d.win.Hide()
			}
		})
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开 Clipnest", Click: func(*mygo.MenuItem, *mygo.Window) { d.show() }},
			{Label: "暂停 / 恢复记录", Click: func(*mygo.MenuItem, *mygo.Window) {
				_, p := h.Snapshot()
				h.Pause(!p)
				d.lastRevision = d.n.Revision()
				_ = panelChanged.Broadcast(d.snapshot())
			}},
			{Label: "问题反馈…", Click: func(*mygo.MenuItem, *mygo.Window) {
				_ = mygo.Shell.OpenExternal("https://github.com/echoVic/clipnest/issues/new")
			}},
			mygo.Separator(), {Label: "退出 Clipnest", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		})
		d.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: runtime.GOOS == "darwin", ToolTip: "Clipnest · ⌘⇧V 打开剪贴板历史"})
		if err != nil {
			d.notice = "菜单栏图标不可用，仍可使用全局快捷键"
		} else if runtime.GOOS == "linux" {
			d.tray.SetMenu(menu)
		} else {
			d.tray.OnClick(d.show)
			d.tray.OnRightClick(func() { d.win.Hide(); d.tray.SetMenu(menu); defer d.tray.SetMenu(nil); d.tray.PopUpMenu() })
		}
		if err := mygo.GlobalShortcut.Register("CmdOrCtrl+Shift+V", d.show); err != nil {
			d.shortcut = "⌥ ⇧ V"
			if err := mygo.GlobalShortcut.Register("Alt+Shift+V", d.show); err != nil {
				d.shortcut = "菜单栏打开"
				d.notice = "快捷键已被占用，请从菜单栏打开"
			} else {
				d.notice = "⌘⇧V 已被占用，已改用 ⌥⇧V"
			}
		}
		if runtime.GOOS != "darwin" {
			d.shortcut = "Ctrl Shift V"
		}
		mygo.App.OnActivate(func(bool) {
			if !d.win.IsVisible() {
				d.show()
			}
		})
		d.win.OnReadyToShow(func() { d.show() })
		go d.monitor()
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { d.quitting = true; close(d.stop) })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		_ = h.Save()
		mygo.GlobalShortcut.UnregisterAll()
		if d.tray != nil {
			d.tray.Destroy()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
