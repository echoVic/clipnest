package main

import (
	"embed"
	"flag"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/plugins/updater"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed web
var frontend embed.FS

func main() {
	webAddress := flag.String("web", "", "启动仅含示例数据的浏览器交互演示")
	dataDir := flag.String("data-dir", "", "历史与常用语的存储目录")
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
	mygo.App.SetLanguages("zh-Hans")
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
	unlock, err := acquireDataLock(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer unlock()
	store, err := openStorage(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Print("存储关闭失败：", err)
		}
	}()
	h := store.History
	d := &Desktop{h: h, p: store.Phrases, storage: store, lastRevision: -2, stop: make(chan struct{}), shortcut: "⌘ ⇧ V"}
	mygo.Use(updater.New(updater.Options{Interval: 4 * time.Hour}))
	mygo.SetFrontend(web)
	mygo.Bind(d)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { d.openPanel() })
	mygo.App.OnActivate(func(bool) { d.openPanel() })
	mygo.App.WhenReady(d.ready)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { d.quitting = true; d.closeView(); close(d.stop) })
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
