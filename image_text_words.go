package main

import (
	"slices"
	"strings"
	"unicode"
)

// Windows' OCR engines each read one language and return words rather than
// lines with glyph boxes. The Chinese engine reads Chinese well but splits and
// misreads Latin text; the English engine drops Chinese entirely. Lines are
// rebuilt from both engines' words here, independent of the platform so the
// rules can be tested anywhere.

// ocrWord is a word an engine read, in image pixels.
type ocrWord struct {
	text string
	box  textBox
}

// Gaps between words are measured against the line's median word height.
const (
	// Wider than this, a gap next to CJK text is a space. CJK glyphs sit
	// about a tenth of the height apart.
	cjkSpaceGap = 0.25
	// CJK characters are a space apart only when a gap of half the height
	// is left, as a misread character leaves.
	cjkCharGap = 0.5
	// Windows ends Latin words at punctuation inside code and numbers. A
	// split this tight always joins.
	latinSplitGap = 0.15
	// A wider split is under half as wide as the line's spaces, taken as its
	// upper quartile gap: in monospaced code a space is a whole cell.
	latinSpaceShare = 0.5
	// Windows splits most at a dot, as in tx. Commit(), and a line of
	// code can hold more splits than spaces. Up to this gap a dot joins.
	latinDotGap = 0.4
	// Camel case such as Begin Tx splits between letters. Only a gap this
	// tight joins letters, under any proportional font's space.
	latinLetterGap = 0.25
)

// asciiMarks are the full-width marks the Chinese engine writes for ASCII
// punctuation in numbers, versions and code, and 一 for a hyphen.
var asciiMarks = map[rune]string{
	'，': ",", '．': ".", '：': ":", '；': ";", '（': "(", '）': ")", '！': "!", '／': "/",
	'〔': "(", '〕': ")", '一': "-", '—': "-", '＃': "#", '＄': "$", '％': "%", '＝': "=",
	'＋': "+", '［': "[", '］': "]", '｛': "{", '｝': "}", '＜': "<", '＞': ">", '＂': "\"", '＇': "'",
}

// Both engines write dashes for the hyphens in flags such as --short.
var latinDashes = strings.NewReplacer("—", "-", "–", "-", "―", "-", "－", "-")

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

func cjkRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// fullWidthMark reports CJK punctuation, which never has a space beside it.
func fullWidthMark(r rune) bool {
	return r >= 0x3000 && r <= 0x303f ||
		r >= 0xff00 && r <= 0xffef && !unicode.IsLetter(r) && !unicode.IsDigit(r) ||
		strings.ContainsRune("“”‘’…", r)
}

func asciiRune(r rune) bool { return r > ' ' && r < 0x7f }

func latinWord(s string) bool {
	for _, r := range s {
		if r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

// quantile is the value at fraction q of values in ascending order.
func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(values))
	return s[int(q*float64(len(s)))]
}

func lineHeight(ws []ocrWord) float64 {
	hs := make([]float64, len(ws))
	for i, w := range ws {
		hs[i] = w.box.H
	}
	return quantile(hs, 0.5)
}

func lineBox(ws []ocrWord) textBox {
	b := ws[0].box
	for _, w := range ws[1:] {
		b = b.union(w.box)
	}
	return b
}

func wordGap(a, b ocrWord) float64 { return b.box.X - a.box.right() }

// normalizeMarks turns a full-width mark tight between ASCII words into ASCII:
// the Chinese engine reads 1,299.00 as 1，299．00. A Chinese comma before a
// space stays.
func normalizeMarks(ws []ocrWord) []ocrWord {
	h := lineHeight(ws)
	out := slices.Clone(ws)
	for i := 1; i < len(out)-1; i++ {
		r := []rune(out[i].text)
		if len(r) != 1 {
			continue
		}
		mark, ok := asciiMarks[r[0]]
		prev, next := out[i-1], out[i+1]
		if ok && asciiRune(lastRune(prev.text)) && asciiRune(firstRune(next.text)) &&
			wordGap(prev, out[i]) <= cjkSpaceGap*h && wordGap(out[i], next) <= cjkSpaceGap*h {
			out[i].text = mark
		}
	}
	return out
}

// spaced reports whether a space separates two adjacent words of a line.
func spaced(a, b ocrWord, h, spaces float64) bool {
	gap, before, after := wordGap(a, b), lastRune(a.text), firstRune(b.text)
	punctuation := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	switch {
	case fullWidthMark(before) || fullWidthMark(after):
		return false
	case cjkRune(before) && cjkRune(after):
		return gap > cjkCharGap*h
	case cjkRune(before) || cjkRune(after):
		return gap > cjkSpaceGap*h
	case before == '.' || after == '.':
		return gap > latinDotGap*h && gap >= latinSpaceShare*spaces
	case !punctuation(before) && !punctuation(after):
		return gap > latinLetterGap*h || gap >= latinSpaceShare*spaces
	}
	return gap > latinSplitGap*h && gap >= latinSpaceShare*spaces
}

// joinWords rebuilds a line from its words. Each word's box is split evenly
// among its characters, which is close enough to decide which tile owns a
// glyph: both tiles see the same word box.
func joinWords(ws []ocrWord) textObservation {
	ws = slices.Clone(ws)
	for i := range ws {
		if latinWord(ws[i].text) {
			ws[i].text = latinDashes.Replace(ws[i].text)
		}
	}
	h := lineHeight(ws)
	gaps := []float64{}
	for i := 1; i < len(ws); i++ {
		if !cjkRune(lastRune(ws[i-1].text)) && !cjkRune(firstRune(ws[i].text)) {
			gaps = append(gaps, wordGap(ws[i-1], ws[i]))
		}
	}
	spaces := quantile(gaps, 0.75)
	o := textObservation{box: lineBox(ws)}
	var text strings.Builder
	for i, w := range ws {
		if i > 0 && spaced(ws[i-1], w, h, spaces) {
			text.WriteByte(' ')
			o.chars = append(o.chars, textChar{text: " "})
		}
		text.WriteString(w.text)
		runes := []rune(w.text)
		width := w.box.W / float64(len(runes))
		for k, r := range runes {
			o.chars = append(o.chars, textChar{text: string(r), box: textBox{w.box.X + float64(k)*width, w.box.Y, width, w.box.H}})
		}
	}
	o.text = text.String()
	return o
}

// chineseLine reports a Chinese engine line that holds Chinese rather than
// Latin text misread as stray Han characters: a run of three Han characters,
// or a fifth of the line in Han.
func chineseLine(ws []ocrWord) bool {
	han, run, longest, all := 0, 0, 0, 0
	for _, w := range ws {
		for _, r := range w.text {
			all++
			if unicode.Is(unicode.Han, r) {
				han++
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
	}
	return longest >= 3 || 5*han >= all
}

func wordLinesOverlap(a, b textBox) bool {
	return verticalOverlap(a, b) > 0.5 && min(a.right(), b.right()) > max(a.X, b.X)
}

func nonEmptyLines(lines [][]ocrWord) [][]ocrWord {
	out := [][]ocrWord{}
	for _, l := range lines {
		l = slices.DeleteFunc(slices.Clone(l), func(w ocrWord) bool { return strings.TrimSpace(w.text) == "" })
		if len(l) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// fuseWordLines combines one tile's lines from the Chinese and the English
// engine; either may be missing. A Chinese line that holds Chinese, or that
// no English line covers, is kept. Any other line is replaced by the English
// lines covering it. English lines no Chinese line covers follow the English
// line they follow in the English engine's reading order.
func fuseWordLines(zh, en [][]ocrWord) [][]ocrWord {
	zh, en = nonEmptyLines(zh), nonEmptyLines(en)
	if len(en) == 0 {
		for i := range zh {
			zh[i] = normalizeMarks(zh[i])
		}
		return zh
	}
	out := [][]ocrWord{}
	at := make([]int, len(en)) // the line in out that covers each English line
	for j := range at {
		at[j] = -1
	}
	for _, z := range zh {
		box := lineBox(z)
		hits := []int{}
		for j, e := range en {
			if wordLinesOverlap(box, lineBox(e)) {
				hits = append(hits, j)
			}
		}
		if len(hits) == 0 || chineseLine(z) {
			for _, j := range hits {
				if at[j] < 0 {
					at[j] = len(out)
				}
			}
			out = append(out, normalizeMarks(z))
			continue
		}
		for _, j := range hits {
			if at[j] < 0 {
				at[j] = len(out)
				out = append(out, en[j])
			}
		}
	}
	follows := map[int][][]ocrWord{}
	previous := -1
	for j, e := range en {
		if at[j] >= 0 {
			previous = at[j]
		} else {
			follows[previous] = append(follows[previous], e)
		}
	}
	fused := slices.Clone(follows[-1])
	for i, l := range out {
		fused = append(append(fused, l), follows[i]...)
	}
	return fused
}

// addWordLines appends lines rebuilt from words in image pixels. Only lines
// crossing an edge where cores meet keep their glyph boxes.
func (t *textTile) addWordLines(lines [][]ocrWord) {
	for _, l := range lines {
		o := joinWords(l)
		if !t.needsGlyphs(o.box) {
			o.chars = nil
		}
		t.lines = append(t.lines, o)
	}
}

// ocrLanguages picks the Chinese and the English recognizer among the
// installed ones, preferring Simplified Chinese; either is empty when none
// is installed.
func ocrLanguages(available []string) (zh, en string) {
	simplified := false
	for _, tag := range available {
		lower := strings.ToLower(tag)
		switch {
		case lower == "en" || strings.HasPrefix(lower, "en-"):
			if en == "" {
				en = tag
			}
		case lower == "zh" || strings.HasPrefix(lower, "zh-"):
			traditional := strings.Contains(lower, "-hant") || strings.HasSuffix(lower, "-tw") ||
				strings.HasSuffix(lower, "-hk") || strings.HasSuffix(lower, "-mo")
			if !simplified && (zh == "" || !traditional) {
				zh, simplified = tag, !traditional
			}
		}
	}
	return zh, en
}

// imageTextLanguageNote explains in preferences what the installed
// recognizers cannot read.
func imageTextLanguageNote(available []string) string {
	if len(available) == 0 {
		return "Windows 没有安装文字识别语言，图片文字暂不识别。在 Windows「设置 → 时间和语言 → 语言和区域」添加语言后会自动开始。"
	}
	if zh, _ := ocrLanguages(available); zh == "" {
		return "未安装中文文字识别，截图中的中文可能识别不出。可在 Windows「设置 → 时间和语言 → 语言和区域」添加中文，或在中文的「语言选项」里安装「光学字符识别」。装好后关闭再打开上面的开关，已有截图会重新识别。"
	}
	return ""
}
