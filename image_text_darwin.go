//go:build darwin

package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const imageTextSupported = true

// VNRequestTextRecognitionLevelAccurate in Vision's VNTypes.h.
const visionAccurate = 0

type cgRect struct{ X, Y, W, H float64 }
type nsRange struct{ Location, Length uint }

var loadVision = sync.OnceValue(func() error {
	for _, framework := range []string{"Foundation", "Vision"} {
		if _, err := purego.Dlopen("/System/Library/Frameworks/"+framework+".framework/"+framework, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return err
		}
	}
	return nil
})

// runImageText recognizes queued images every two seconds until stop closes.
// The worker keeps one OS thread at utility QoS, so recognition yields to the
// panel and to other apps. The thread is never unlocked: it ends with the
// goroutine instead of running other goroutines at that QoS.
func (d *Desktop) runImageText() {
	runtime.LockOSThread()
	if libSystem, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW); err == nil {
		var setQOS func(class uint32, priority int32) int32
		purego.RegisterLibFunc(&setQOS, libSystem, "pthread_set_qos_class_self_np")
		setQOS(qosUtility, 0)
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	d.h.recognizeImages(recognizeImageText, t.C, d.stop)
}

// QOS_CLASS_UTILITY in sys/qos.h.
const qosUtility = 0x11

// recognizeImageText returns the lines Vision reads in a PNG. An image larger
// than a tile is decoded once, then each region is encoded and recognized with
// its own request and autorelease pool, which bounds Vision's memory to about
// one tile.
func recognizeImageText(data []byte) (string, error) {
	if err := loadVision(); err != nil {
		return "", err
	}
	c, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	tiles := textTiles(c.Width, c.Height)
	var img interface {
		SubImage(image.Rectangle) image.Image
	}
	if len(tiles) > 1 {
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		var ok bool
		if img, ok = decoded.(interface {
			SubImage(image.Rectangle) image.Image
		}); !ok {
			return "", errors.New("图片格式无法分块识别")
		}
		// The decoded image takes 4 bytes per pixel. An idle app kept 230 MB
		// for minutes after a 46-megapixel image; returning it leaves 60–95 MB.
		defer debug.FreeOSMemory()
	}
	// Each pool must be drained on the thread that created it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	for i := range tiles {
		tile := data
		if img != nil {
			var b bytes.Buffer
			if err := enc.Encode(&b, img.SubImage(tiles[i].region)); err != nil {
				return "", err
			}
			tile = b.Bytes()
		}
		if err := tiles[i].recognize(tile); err != nil {
			return "", err
		}
	}
	return strings.Join(mergeTextTiles(tiles), "\n"), nil
}

// recognize appends the lines Vision reads in the tile's PNG, in image pixels.
// Lines crossing an edge where cores meet also get their glyphs' boxes.
func (t *textTile) recognize(data []byte) error {
	sel := objc.RegisterName
	class := func(name string) objc.ID { return objc.ID(objc.GetClass(name)) }
	pool := class("NSAutoreleasePool").Send(sel("new"))
	defer pool.Send(sel("drain"))
	nsData := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&data[0]), uintptr(len(data)))
	handler := class("VNImageRequestHandler").Send(sel("alloc")).Send(sel("initWithData:options:"), nsData, class("NSDictionary").Send(sel("dictionary")))
	defer handler.Send(sel("release"))
	request := class("VNRecognizeTextRequest").Send(sel("alloc")).Send(sel("init"))
	defer request.Send(sel("release"))
	request.Send(sel("setRecognitionLevel:"), visionAccurate)
	// Language correction splits code such as tx.Commit() at the dot.
	request.Send(sel("setUsesLanguageCorrection:"), false)
	request.Send(sel("setAutomaticallyDetectsLanguage:"), true)
	var nsErr objc.ID
	if !objc.Send[bool](handler, sel("performRequests:error:"), class("NSArray").Send(sel("arrayWithObject:"), request), unsafe.Pointer(&nsErr)) {
		reason := "Vision 无法识别图片"
		if nsErr != 0 {
			reason += "：" + objc.Send[string](nsErr.Send(sel("localizedDescription")), sel("UTF8String"))
		}
		return errors.New(reason)
	}
	// Vision's boxes are normalized with a lower-left origin.
	w, h := float64(t.region.Dx()), float64(t.region.Dy())
	box := func(b cgRect) textBox {
		return textBox{float64(t.region.Min.X) + b.X*w, float64(t.region.Min.Y) + (1-b.Y-b.H)*h, b.W * w, b.H * h}
	}
	results := request.Send(sel("results"))
	for i := range objc.Send[uint](results, sel("count")) {
		observation := results.Send(sel("objectAtIndex:"), i)
		candidates := observation.Send(sel("topCandidates:"), 1)
		if objc.Send[uint](candidates, sel("count")) == 0 {
			continue
		}
		candidate := candidates.Send(sel("objectAtIndex:"), 0)
		line := textObservation{text: objc.Send[string](candidate.Send(sel("string")), sel("UTF8String")), box: box(objc.Send[cgRect](observation, sel("boundingBox")))}
		if t.needsGlyphs(line.box) {
			// Vision's ranges count UTF-16 code units.
			offset := uint(0)
			for _, r := range line.text {
				ch, n := textChar{text: string(r)}, uint(utf16.RuneLen(r))
				if !unicode.IsSpace(r) {
					if glyph := candidate.Send(sel("boundingBoxForRange:error:"), nsRange{offset, n}, uintptr(0)); glyph != 0 {
						if b := objc.Send[cgRect](glyph, sel("boundingBox")); b.W > 0 {
							ch.box = box(b)
						}
					}
				}
				line.chars = append(line.chars, ch)
				offset += n
			}
		}
		t.lines = append(t.lines, line)
	}
	return nil
}
