#!/usr/bin/env python3
"""Build a separate native diagnostic app without editing production sources.
Run with --output <directory>. The app uses generated fixtures only, an isolated
identity/data directory and Cmd+Ctrl+Shift+V. It never signs in or updates.
Performance records contain timings/labels, not clipboard content.
"""
import argparse, json, pathlib, shutil, subprocess

parser=argparse.ArgumentParser()
parser.add_argument('--output',required=True)
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
output=pathlib.Path(args.output).resolve()
output.mkdir(parents=True,exist_ok=False)
source=output/'source';source.mkdir()
for path in root.glob('*.go'):
    if not path.name.endswith('_test.go'):shutil.copy2(path,source/path.name)
for name in ('go.mod','go.sum'):shutil.copy2(root/name,source/name)
shutil.copytree(root/'web',source/'web')
data=output/'data';data.mkdir()
records=output/'native-samples.jsonl'
# Deterministic legal text workload, below the existing 32 MiB content cap.
entries=[]
for i in range(200):
    text=f'Fixture {i:03d}\n'+('HELLO 中文 Clipboard '*5700)+' EndNeedle'
    entries.append(dict(text=text,source='Performance fixture',created=1792990000000+i))
(data/'history.json').write_text(json.dumps(dict(version=1,paused=True,entries=entries),ensure_ascii=False))
(data/'phrases.json').write_text(json.dumps(dict(version=1,phrases=[dict(id='perf-phrase',text='CLIPNEST_NATIVE_PASTE_FIXTURE')]),ensure_ascii=False))
(data/'history.json').chmod(0o600);(data/'phrases.json').chmod(0o600)

probe=r'''package main
import("encoding/json";"os";"sync";"time")
var probeStarted = time.Now()
var probeOpen time.Time
var probeMu sync.Mutex
var probePath = PATH
// Bound only in this diagnostic build. No identifiers or clipboard contents.
type ResponseProbe struct{}
func (*ResponseProbe) Report(label string, ms float64) {
 probeMu.Lock();defer probeMu.Unlock()
 f,err:=os.OpenFile(probePath,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if err!=nil{return};defer f.Close()
 _=json.NewEncoder(f).Encode(map[string]any{"label":label,"ms":ms,"at_ms":time.Since(probeStarted).Milliseconds()})
}
func (p *ResponseProbe) OpenPaint() {p.Report("native_show_to_paint_ack",float64(time.Since(probeOpen).Nanoseconds())/1e6)}
'''.replace('PATH',json.dumps(str(records)))
(source/'response_probe.go').write_text(probe)
main=(source/'main.go').read_text()
main=main.replace('"flag"','"flag"\n"runtime/debug"')
startup = '''func main() {
 startup, startupErr := os.OpenFile(STARTUP_PATH, os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
 if startupErr == nil {
  os.Stderr=startup;log.SetOutput(startup)
  _=debug.SetCrashOutput(startup,debug.CrashOptions{})
  flag.CommandLine.SetOutput(startup)
  log.Print("diagnostic main entered")
 }
'''.replace('STARTUP_PATH',json.dumps(str(output/'startup.log')))
main=main.replace('func main() {',startup)
main=main.replace('dataDir := flag.String("data-dir", "",','dataDir := flag.String("data-dir", '+json.dumps(str(data))+',')
main=main.replace('mygo.App.SetName("Clipnest")','mygo.App.SetName("Clipnest Perf")')
main=main.replace('mygo.Use(updater.New(updater.Options{Interval: 4 * time.Hour}))','// Diagnostic builds never check for or install updates.')
main=main.replace('\t"github.com/egoist/mygo/plugins/updater"\n','')
# Both plugin setup and every consumer must go together; a leftover MenuItem
# panics on native startup, even though a bare build succeeds.
assert 'updater.' not in main, 'diagnostic main still references updater'
main=main.replace('mygo.Bind(d)','mygo.Bind(d, &ResponseProbe{})')
main=main.replace('shortcut: "⌘ ⇧ V"','shortcut: "⌘ ⌃ ⇧ V"')
(source/'main.go').write_text(main)
menu=(source/'desktop_menu.go').read_text()
menu=menu.replace('\t\tupdater.MenuItem(),\n','')
menu=menu.replace('\t"github.com/egoist/mygo/plugins/updater"\n','')
assert 'updater.' not in menu, 'diagnostic tray still references updater'
(source/'desktop_menu.go').write_text(menu)
panel=(source/'desktop_panel.go').read_text()
panel=panel.replace('"runtime"','"runtime"\n"time"')
panel=panel.replace('"CmdOrCtrl+Shift+V"','"Command+Control+Shift+V"')
panel=panel.replace('Title: "Clipnest · 剪贴板历史"','Title: "Clipnest Perf · 隔离响应测试"')
panel=panel.replace('func (d *Desktop) openPanel() {','func (d *Desktop) openPanel() {\n probeOpen=time.Now()\n defer func(){(&ResponseProbe{}).Report("native_show_callback",float64(time.Since(probeOpen).Nanoseconds())/1e6)}()')
(source/'desktop_panel.go').write_text(panel)
desktop=(source/'desktop.go').read_text()
desktop=desktop.replace('func (d *Desktop) CheckForUpdates() { updater.CheckForUpdates() }','func (d *Desktop) CheckForUpdates() {}')
desktop=desktop.replace('\t"github.com/egoist/mygo/plugins/updater"\n','')
assert 'updater.' not in desktop, 'diagnostic service still references updater'
(source/'desktop.go').write_text(desktop)
app=(source/'web/app.js').read_text()
app=app.replace("const call = (method, ...args) => api.call(method, ...args);",'''const report = (label, ms) => window.mygo.call('ResponseProbe.Report',label,ms).catch(()=>{});
const call = async (method, ...args) => {
 const start=performance.now();try{return await api.call(method,...args);}finally{report('bridge_'+method,performance.now()-start);}
};
let probeInputAt=0;
document.addEventListener('input',event=>{if(event.target.id==='search')probeInputAt=performance.now();},true);
document.addEventListener('keydown',event=>{if(event.key==='ArrowDown'||event.key==='ArrowUp')probeInputAt=performance.now();},true);
const paintAck = label => requestAnimationFrame(()=>requestAnimationFrame(()=>{
 if(probeInputAt) {report(label,performance.now()-probeInputAt);probeInputAt=0;}
}));''')
app=app.replace("requestAnimationFrame(() => $('#list').focus({ preventScroll: true }));","requestAnimationFrame(()=>requestAnimationFrame(()=>window.mygo.call('ResponseProbe.OpenPaint').catch(()=>{}))); requestAnimationFrame(() => $('#list').focus({ preventScroll: true }));")
app=app.replace('renderWindow();\n  updatePreview();','renderWindow();\n  updatePreview(); paintAck("input_to_list_paint_opportunity");')
app=app.replace("$('#preview-meta').replaceChildren(info, remove);","$('#preview-meta').replaceChildren(info, remove); paintAck('input_to_detail_paint_opportunity');")
app=app.replace("$('#preview-meta').replaceChildren(node('span', '', `${characterCount(p.text)} 个字符`), actions);","$('#preview-meta').replaceChildren(node('span', '', `${characterCount(p.text)} 个字符`), actions); paintAck('input_to_phrase_paint_opportunity');")
(source/'web/app.js').write_text(app)
appdir=output/'Clipnest Perf.app';mac=appdir/'Contents/MacOS';mac.mkdir(parents=True)
plist=dict(CFBundleName='Clipnest Perf',CFBundleDisplayName='Clipnest Perf',CFBundleIdentifier='app.clipnest.response-diagnostic',CFBundleExecutable='ClipnestPerf',CFBundlePackageType='APPL',CFBundleVersion='1',LSUIElement=True,LSMinimumSystemVersion='13.0')
import plistlib
(appdir/'Contents/Info.plist').write_bytes(plistlib.dumps(plist))
subprocess.run(['go','build','-o',str(mac/'ClipnestPerf'),'.'],cwd=source,check=True)
subprocess.run(['codesign','--force','--sign','-',str(appdir)],check=True)
print(json.dumps({'app':str(appdir),'data':str(data),'samples':str(records),'source':str(source)},ensure_ascii=False))
