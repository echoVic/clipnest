//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const imageTextSupported = true

// THREAD_MODE_BACKGROUND_BEGIN in processthreadsapi.h.
const threadModeBackgroundBegin = 0x00010000

// runImageText recognizes queued images every two seconds until stop closes.
// Like utility QoS on macOS, background mode lowers the worker thread's CPU,
// I/O and memory priority; it decodes and converts the images, while
// Windows recognizes them on its own thread pool. The thread is never
// unlocked: it ends with the goroutine.
func (d *Desktop) runImageText() {
	runtime.LockOSThread()
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	thread, _, _ := kernel32.NewProc("GetCurrentThread").Call()
	kernel32.NewProc("SetThreadPriority").Call(thread, threadModeBackgroundBegin)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	d.h.recognizeImages(recognizeImageText, t.C, d.stop)
}

// The activation factories of Windows.Media.Ocr and the classes it takes.
type ocrRuntime struct {
	engines, languages, bitmaps, buffers *comObject
}

var loadOCR = sync.OnceValues(func() (*ocrRuntime, error) {
	var cookie uintptr
	if r, _, _ := procCoIncrementMTAUsage.Call(uintptr(unsafe.Pointer(&cookie))); failed(uint32(r)) {
		return nil, hresultError{"CoIncrementMTAUsage", uint32(r)}
	}
	o := &ocrRuntime{}
	for _, f := range []struct {
		out   **comObject
		class string
		iid   *syscall.GUID
	}{
		{&o.engines, "Windows.Media.Ocr.OcrEngine", &iidOcrEngineStatics},
		{&o.languages, "Windows.Globalization.Language", &iidLanguageFactory},
		{&o.bitmaps, "Windows.Graphics.Imaging.SoftwareBitmap", &iidSoftwareBitmapStatics},
		{&o.buffers, "Windows.Security.Cryptography.CryptographicBuffer", &iidCryptographicBufferStatics},
	} {
		var err error
		if *f.out, err = activationFactory(f.class, f.iid); err != nil {
			return nil, err
		}
	}
	return o, nil
})

// installed lists the tags of the languages Windows can recognize.
func (o *ocrRuntime) installed() ([]string, error) {
	list, err := o.engines.object("OcrEngine.AvailableRecognizerLanguages", 7)
	if err != nil {
		return nil, err
	}
	defer list.release()
	n, err := list.size()
	if err != nil {
		return nil, err
	}
	tags := []string{}
	for i := range n {
		language, err := list.at(i)
		if err != nil {
			return nil, err
		}
		tag, err := language.text("Language.LanguageTag", 6)
		language.release()
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// engine creates a recognizer for tag, or for the user's preferred
// languages when tag is empty. It is nil when Windows has none.
func (o *ocrRuntime) engine(tag string) (*comObject, error) {
	var e *comObject
	if tag == "" {
		return e, check("OcrEngine.TryCreateFromUserProfileLanguages", o.engines.call(10, uintptr(unsafe.Pointer(&e))))
	}
	h, err := newHString(tag)
	if err != nil {
		return nil, err
	}
	defer h.delete()
	var language *comObject
	if err := check("Language("+tag+")", o.languages.call(6, uintptr(h), uintptr(unsafe.Pointer(&language)))); err != nil {
		return nil, err
	}
	defer language.release()
	return e, check("OcrEngine.TryCreateFromLanguage("+tag+")", o.engines.call(9, uintptr(unsafe.Pointer(language)), uintptr(unsafe.Pointer(&e))))
}

// recognize returns the lines one engine reads in BGRA pixels, as words in
// the pixels' coordinates moved by offset.
func (o *ocrRuntime) recognize(engine *comObject, pixels []byte, size image.Point, offset image.Point) ([][]ocrWord, error) {
	var buffer *comObject
	if err := check("CryptographicBuffer.CreateFromByteArray", o.buffers.call(9, uintptr(len(pixels)), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&buffer)))); err != nil {
		return nil, err
	}
	defer buffer.release()
	var bitmap *comObject
	const bgra8 = 87 // BitmapPixelFormat.Bgra8
	if err := check("SoftwareBitmap.CreateCopyFromBuffer", o.bitmaps.call(9, uintptr(unsafe.Pointer(buffer)), bgra8, uintptr(size.X), uintptr(size.Y), uintptr(unsafe.Pointer(&bitmap)))); err != nil {
		return nil, err
	}
	defer bitmap.release()
	var op *comObject
	if err := check("OcrEngine.RecognizeAsync", engine.call(6, uintptr(unsafe.Pointer(bitmap)), uintptr(unsafe.Pointer(&op)))); err != nil {
		return nil, err
	}
	defer op.release()
	if err := await(op, time.Minute); err != nil {
		return nil, err
	}
	result, err := op.object("RecognizeAsync.GetResults", 8)
	if err != nil {
		return nil, err
	}
	defer result.release()
	lines, err := result.object("OcrResult.Lines", 6)
	if err != nil {
		return nil, err
	}
	defer lines.release()
	n, err := lines.size()
	if err != nil {
		return nil, err
	}
	out := [][]ocrWord{}
	for i := range n {
		words, err := o.words(lines, i, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, words)
	}
	return out, nil
}

func (o *ocrRuntime) words(lines *comObject, i uint32, offset image.Point) ([]ocrWord, error) {
	line, err := lines.at(i)
	if err != nil {
		return nil, err
	}
	defer line.release()
	list, err := line.object("OcrLine.Words", 6)
	if err != nil {
		return nil, err
	}
	defer list.release()
	n, err := list.size()
	if err != nil {
		return nil, err
	}
	words := make([]ocrWord, 0, n)
	for j := range n {
		word, err := list.at(j)
		if err != nil {
			return nil, err
		}
		var r struct{ X, Y, W, H float32 }
		err = check("OcrWord.BoundingRect", word.call(6, uintptr(unsafe.Pointer(&r))))
		var text string
		if err == nil {
			text, err = word.text("OcrWord.Text", 7)
		}
		word.release()
		if err != nil {
			return nil, err
		}
		words = append(words, ocrWord{text, textBox{float64(r.X) + float64(offset.X), float64(r.Y) + float64(offset.Y), float64(r.W), float64(r.H)}})
	}
	return words, nil
}

// ocrEngines are created once and again when the installed languages
// change, checked at most once a minute. With Chinese and English both
// installed, both read every tile; otherwise one engine reads, the user's
// preferred language when Chinese is missing.
type ocrEngines struct {
	mu        sync.Mutex
	checked   time.Time
	tags      []string
	ready     bool
	zh, other *comObject
}

var imageTextEngines ocrEngines

func (e *ocrEngines) close() {
	e.zh.release()
	e.other.release()
	e.zh, e.other, e.ready = nil, nil, false
}

func (e *ocrEngines) refresh(o *ocrRuntime) error {
	if e.ready && time.Since(e.checked) < time.Minute {
		return nil
	}
	tags, err := o.installed()
	if err != nil {
		return err
	}
	e.checked = time.Now()
	if e.ready && slices.Equal(tags, e.tags) {
		return nil
	}
	e.close()
	zh, en := ocrLanguages(tags)
	if zh != "" {
		if e.zh, err = o.engine(zh); err != nil {
			return err
		}
	}
	switch {
	case en != "" && zh != "":
		e.other, err = o.engine(en)
	case zh == "" && len(tags) > 0:
		if e.other, err = o.engine(""); err == nil && e.other == nil {
			e.other, err = o.engine(tags[0])
		}
	}
	if err != nil {
		e.close()
		return err
	}
	e.tags, e.ready = tags, true
	return nil
}

// recognizeImageText returns the lines Windows reads in a PNG. Each tile is
// converted to the pixels Windows takes; with two engines, both read it and
// their lines are fused.
func recognizeImageText(data []byte) (string, error) {
	c, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	leave, err := enterMTA()
	if err != nil {
		return "", err
	}
	defer leave()
	o, err := loadOCR()
	if err != nil {
		return "", fmt.Errorf("%w：%v", errImageTextUnavailable, err)
	}
	e := &imageTextEngines
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refresh(o); err != nil {
		return "", fmt.Errorf("%w：%v", errImageTextUnavailable, err)
	}
	if e.zh == nil && e.other == nil {
		return "", errImageTextUnavailable
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	tiles := textTiles(c.Width, c.Height)
	if len(tiles) > 1 {
		// See recognizeImageText on macOS: the decoded image is returned to
		// the system rather than kept for minutes.
		defer debug.FreeOSMemory()
	}
	for i := range tiles {
		r := tiles[i].region
		pixels := bgra(img, r.Add(img.Bounds().Min))
		var zh, other [][]ocrWord
		if e.zh != nil {
			if zh, err = o.recognize(e.zh, pixels, r.Size(), r.Min); err != nil {
				return "", err
			}
		}
		if e.other != nil {
			if other, err = o.recognize(e.other, pixels, r.Size(), r.Min); err != nil {
				return "", err
			}
		}
		tiles[i].addWordLines(fuseWordLines(zh, other))
	}
	return strings.Join(mergeTextTiles(tiles), "\n"), nil
}

// bgra returns r of img as the BGRA pixels SoftwareBitmap takes, drawn over
// white: a transparent screenshot reads like it does on a light window.
func bgra(img image.Image, r image.Rectangle) []byte {
	dst := image.NewRGBA(image.Rectangle{Max: r.Size()})
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Over)
	p := dst.Pix
	for i := 0; i < len(p); i += 4 {
		p[i], p[i+2] = p[i+2], p[i]
	}
	return p
}

// imageTextNote explains in preferences what the installed recognizers
// cannot read. The preferences call it on the main thread, which is in a
// single-threaded apartment, so it reads them on another thread.
func imageTextNote() string {
	note := make(chan string, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		note <- installedLanguageNote()
	}()
	select {
	case n := <-note:
		return n
	case <-time.After(2 * time.Second):
		return ""
	}
}

func installedLanguageNote() string {
	leave, err := enterMTA()
	if err == nil {
		defer leave()
		var o *ocrRuntime
		if o, err = loadOCR(); err == nil {
			var tags []string
			if tags, err = o.installed(); err == nil {
				return imageTextLanguageNote(tags)
			}
		}
	}
	return "无法读取 Windows 的文字识别语言：" + err.Error()
}
