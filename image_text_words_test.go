package main

import (
	"image"
	"reflect"
	"strings"
	"testing"
)

// The word boxes below are what Windows' en-GB and zh-Hans-CN engines read in
// the evaluation corpus of testdata/image-text-eval, in image pixels.

// wordLines rebuilds the lines of one tile covering a w×h image.
func wordLines(w, h int, zh, en [][]ocrWord) []string {
	tile := textTile{region: image.Rect(0, 0, w, h), core: image.Rect(0, 0, w, h)}
	tile.addWordLines(fuseWordLines(zh, en))
	return mergeTextTiles([]textTile{tile})
}

func assertLines(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Windows reads each Chinese character as its own word.
func TestImageTextWordsJoinChineseWithoutSpaces(t *testing.T) {
	zh := [][]ocrWord{
		{{"系", textBox{42, 32, 26, 28}}, {"统", textBox{71, 32, 28, 28}}, {"设", textBox{101, 32, 28, 28}}, {"置", textBox{131, 33, 27, 26}}, {">", textBox{170, 40, 15, 17}}, {"隐", textBox{197, 32, 27, 28}}, {"私", textBox{226, 32, 28, 28}}, {"与", textBox{256, 31, 25, 28}}, {"安", textBox{287, 32, 26, 28}}, {"全", textBox{316, 31, 28, 28}}, {"性", textBox{346, 32, 28, 29}}, {">", textBox{385, 40, 15, 17}}, {"辅", textBox{411, 31, 28, 29}}, {"助", textBox{441, 32, 27, 28}}, {"功", textBox{471, 32, 27, 28}}, {"能", textBox{502, 32, 27, 28}}},
		{{"允", textBox{42, 83, 26, 28}}, {"许", textBox{71, 82, 28, 29}}, {"下", textBox{101, 85, 27, 26}}, {"面", textBox{132, 84, 27, 28}}, {"的", textBox{162, 83, 26, 28}}, {"应", textBox{191, 83, 28, 27}}, {"用", textBox{221, 84, 26, 27}}, {"控", textBox{251, 83, 28, 28}}, {"制", textBox{282, 83, 26, 29}}, {"您", textBox{311, 82, 27, 29}}, {"的", textBox{342, 83, 26, 28}}, {"电", textBox{374, 83, 25, 28}}, {"脑", textBox{401, 83, 28, 28}}, {"。", textBox{432, 101, 9, 10}}},
		{{"剪", textBox{42, 134, 26, 28}}, {"贴", textBox{71, 134, 28, 28}}, {"板", textBox{101, 134, 28, 29}}, {"记", textBox{131, 134, 28, 27}}, {"录", textBox{161, 135, 28, 27}}, {"已", textBox{193, 136, 25, 25}}, {"暂", textBox{221, 134, 28, 28}}, {"停", textBox{251, 134, 28, 29}}, {"，", textBox{283, 156, 4, 8}}, {"新", textBox{311, 134, 28, 28}}, {"的", textBox{342, 134, 26, 28}}, {"复", textBox{371, 134, 28, 28}}, {"制", textBox{402, 134, 26, 28}}, {"不", textBox{431, 136, 28, 26}}, {"会", textBox{461, 133, 28, 29}}, {"保", textBox{491, 134, 28, 28}}, {"存", textBox{521, 133, 28, 29}}, {"。", textBox{552, 152, 10, 10}}},
	}
	assertLines(t, wordLines(864, 324, zh, nil),
		"系统设置 > 隐私与安全性 > 辅助功能",
		"允许下面的应用控制您的电脑。",
		"剪贴板记录已暂停，新的复制不会保存。")
}

// The Chinese engine splits Latin words and swaps letters, so lines without
// Chinese come from the English engine.
func TestImageTextWordsTakeLatinLinesFromTheEnglishEngine(t *testing.T) {
	zh := [][]ocrWord{
		{{"CIipnest", textBox{41, 32, 92, 24}}, {"keeps", textBox{141, 32, 66, 24}}, {"your", textBox{215, 37, 50, 19}}, {"cli", textBox{272, 32, 23, 19}}, {"pboard", textBox{298, 32, 77, 24}}, {"hi", textBox{384, 32, 17, 19}}, {"story", textBox{404, 33, 55, 23}}, {"on", textBox{466, 37, 27, 14}}, {"this", textBox{501, 32, 39, 19}}, {"Mac.", textBox{550, 32, 52, 19}}},
		{{"Press", textBox{42, 76, 63, 19}}, {"Command-Shift-V", textBox{114, 76, 204, 20}}, {"to", textBox{325, 77, 21, 18}}, {"open", textBox{353, 81, 55, 19}}, {"the", textBox{416, 76, 35, 19}}, {"panel,", textBox{459, 76, 66, 24}}, {"then", textBox{534, 76, 48, 19}}, {"pick", textBox{591, 76, 45, 24}}, {"an", textBox{644, 81, 26, 14}}, {"item", textBox{679, 76, 46, 19}}},
		{{"Recognized", textBox{42, 120, 132, 24}}, {"text", textBox{182, 121, 41, 18}}, {"in", textBox{231, 120, 18, 19}}, {"screenshots", textBox{258, 120, 136, 19}}, {"becom", textBox{403, 120, 74, 19}}, {"es", textBox{480, 125, 25, 14}}, {"searchabl", textBox{514, 120, 108, 19}}, {"e.", textBox{625, 125, 18, 14}}},
	}
	en := [][]ocrWord{
		{{"Clipnest", textBox{41, 32, 92, 24}}, {"keeps", textBox{141, 32, 66, 24}}, {"your", textBox{215, 37, 50, 19}}, {"clipboard", textBox{272, 32, 103, 24}}, {"history", textBox{384, 32, 75, 24}}, {"on", textBox{466, 37, 27, 14}}, {"this", textBox{501, 32, 39, 19}}, {"Mac.", textBox{550, 32, 52, 19}}},
		{{"Press", textBox{42, 76, 63, 19}}, {"Command-Shift-V", textBox{114, 76, 204, 20}}, {"to", textBox{325, 77, 21, 18}}, {"open", textBox{353, 81, 55, 19}}, {"the", textBox{416, 76, 35, 19}}, {"panel,", textBox{459, 76, 66, 24}}, {"then", textBox{534, 76, 48, 19}}, {"pick", textBox{591, 76, 45, 24}}, {"an", textBox{644, 81, 26, 14}}, {"item.", textBox{679, 76, 53, 19}}},
		{{"Recognized", textBox{42, 120, 132, 24}}, {"text", textBox{182, 121, 41, 18}}, {"in", textBox{231, 120, 18, 19}}, {"screenshots", textBox{258, 120, 136, 19}}, {"becomes", textBox{403, 120, 102, 19}}, {"searchable.", textBox{514, 120, 129, 19}}},
	}
	assertLines(t, wordLines(864, 180, zh, en),
		"Clipnest keeps your clipboard history on this Mac.",
		"Press Command-Shift-V to open the panel, then pick an item.",
		"Recognized text in screenshots becomes searchable.")
}

// Lines with Chinese keep the Chinese engine's words. It writes full-width
// marks inside numbers; a mark tight between ASCII words is ASCII, while a
// Chinese comma before a space stays.
func TestImageTextWordsKeepChineseLinesAndTheirNumbers(t *testing.T) {
	zh := [][]ocrWord{
		{{"订", textBox{40, 37, 27, 26}}, {"单", textBox{69, 36, 26, 27}}, {"号", textBox{97, 38, 26, 25}}, {"2026102800123", textBox{132, 39, 168, 19}}, {"金", textBox{315, 37, 27, 25}}, {"额", textBox{343, 37, 27, 26}}, {"1", textBox{392, 41, 10, 17}}, {"，", textBox{404, 56, 5, 8}}, {"299", textBox{411, 41, 37, 20}}, {"．", textBox{450, 55, 4, 3}}, {"00", textBox{456, 41, 25, 17}}},
		{{"lnvoice", textBox{41, 88, 83, 18}}, {"#", textBox{132, 87, 16, 20}}, {"INV", textBox{150, 88, 52, 19}}, {"一", textBox{203, 99, 7, 1}}, {"2026", textBox{212, 86, 51, 19}}, {"一", textBox{264, 99, 7, 1}}, {"0042", textBox{273, 88, 51, 17}}, {"Total", textBox{338, 87, 62, 18}}, {"$", textBox{409, 85, 12, 24}}, {"42", textBox{422, 88, 25, 17}}, {"．", textBox{448, 102, 4, 3}}, {"50", textBox{454, 87, 25, 18}}},
		{{"验", textBox{41, 131, 26, 26}}, {"证", textBox{69, 131, 26, 25}}, {"码", textBox{97, 132, 27, 25}}, {"482913", textBox{132, 135, 77, 20}}, {"，", textBox{212, 147, 5, 10}}, {"5", textBox{238, 134, 12, 18}}, {"分", textBox{258, 131, 26, 26}}, {"钟", textBox{286, 131, 26, 26}}, {"内", textBox{316, 131, 23, 26}}, {"有", textBox{342, 131, 26, 26}}, {"效", textBox{370, 130, 27, 27}}, {"。", textBox{400, 147, 8, 8}}},
	}
	en := [][]ocrWord{
		{{"2026102800123", textBox{132, 39, 168, 19}}, {"1,299.00", textBox{392, 41, 89, 23}}},
		{{"Invoice", textBox{41, 88, 83, 18}}, {"#1NV-2026-0042", textBox{132, 86, 192, 21}}, {"Total", textBox{338, 87, 62, 18}}, {"$42.50", textBox{409, 85, 70, 24}}},
		{{"482913,", textBox{132, 135, 85, 22}}},
	}
	assertLines(t, wordLines(864, 180, zh, en),
		"订单号 2026102800123 金额 1,299.00",
		"Invoice #1NV-2026-0042 Total $42.50",
		"验证码 482913，5 分钟内有效。")
	// Without an English engine the Chinese engine's Latin line is kept.
	assertLines(t, wordLines(864, 180, zh[1:2], nil), "lnvoice #INV-2026-0042 Total $42.50")
}

// An English line no Chinese line covers keeps its place in the English
// engine's reading order.
func TestImageTextWordsKeepLinesOnlyTheEnglishEngineRead(t *testing.T) {
	zh := [][]ocrWord{
		{{"func", textBox{42, 35, 60, 20}}, {"（", textBox{125, 34, 6, 23}}, {"s", textBox{138, 41, 11, 14}}, {"*Store)", textBox{169, 34, 106, 23}}, {"Save(ctx", textBox{297, 34, 126, 23}}, {"context", textBox{442, 37, 107, 18}}, {".Context)", textBox{557, 34, 134, 23}}, {"e", textBox{713, 40, 14, 15}}, {"r", textBox{732, 41, 11, 14}}, {"ro", textBox{748, 41, 26, 14}}, {"r", textBox{780, 41, 11, 14}}, {"{", textBox{810, 34, 11, 24}}},
		{{"s.db.PingContext(ctx);", textBox{266, 78, 344, 26}}, {"e", textBox{633, 84, 14, 15}}, {"r", textBox{652, 85, 11, 14}}, {"r", textBox{668, 85, 11, 14}}, {"！", textBox{702, 80, 4, 19}}, {"=", textBox{713, 87, 14, 8}}, {"nil", textBox{746, 79, 44, 20}}, {"{", textBox{810, 78, 11, 24}}},
		{{"return", textBox{172, 125, 90, 18}}, {"fmt.Errorf(\"ping", textBox{282, 122, 252, 26}}, {"：", textBox{541, 129, 5, 14}}, {"%w\"", textBox{568, 123, 44, 20}}, {"，", textBox{620, 138, 6, 9}}, {"e", textBox{649, 128, 14, 15}}, {"r", textBox{668, 129, 11, 14}}, {"r", textBox{684, 129, 11, 14}}, {"）", textBox{700, 122, 7, 23}}},
		{{"return", textBox{108, 213, 90, 18}}, {"niI", textBox{218, 211, 44, 20}}},
	}
	en := [][]ocrWord{
		{{"func", textBox{42, 35, 60, 20}}, {"(s", textBox{125, 34, 24, 23}}, {"*Store)", textBox{169, 34, 106, 23}}, {"Save(ctx", textBox{297, 34, 126, 23}}, {"context.", textBox{442, 37, 120, 18}}, {"Context)", textBox{569, 34, 122, 23}}, {"error", textBox{713, 40, 78, 15}}, {"{", textBox{810, 34, 11, 24}}},
		{{"s.db.PingContext(ctx);", textBox{266, 78, 344, 26}}, {"err", textBox{633, 84, 46, 15}}, {"!=", textBox{702, 80, 25, 19}}, {"nil", textBox{746, 79, 44, 20}}, {"{", textBox{810, 78, 11, 24}}},
		{{"if", textBox{107, 79, 27, 20}}, {"err", textBox{153, 84, 46, 15}}, {".", textBox{221, 94, 4, 5}}},
		{{"return", textBox{172, 125, 90, 18}}, {"fmt.Errorf(\"ping:", textBox{282, 122, 264, 26}}, {"%w\",", textBox{568, 123, 58, 24}}, {"err)", textBox{649, 122, 58, 23}}},
		{{"return", textBox{108, 213, 90, 18}}, {"nil", textBox{218, 211, 44, 20}}},
	}
	assertLines(t, wordLines(864, 324, zh, en),
		"func (s *Store) Save(ctx context.Context) error {",
		"s.db.PingContext(ctx); err != nil {",
		"if err .",
		`return fmt.Errorf("ping: %w", err)`,
		"return nil")
}

// Windows ends a word at a dot inside code. Such a split is narrower than
// the line's spaces; a space in a proportional font is not.
func TestImageTextWordsJoinCodeSplitAtPunctuation(t *testing.T) {
	en := [][]ocrWord{
		{{"at", textBox{93, 110, 23, 15}}, {"Object.", textBox{132, 108, 85, 22}}, {"openSync", textBox{223, 109, 102, 21}}, {"(node:", textBox{343, 107, 69, 20}}},
		{{"Server:", textBox{40, 165, 76, 19}}, {"192.168.1.10:8080", textBox{123, 165, 198, 19}}},
	}
	assertLines(t, wordLines(864, 324, nil, en),
		"at Object.openSync (node:",
		"Server: 192.168.1.10:8080")
}

// The English engine also splits camel case in code, far tighter than the
// line's spaces. Spaces in prose stay (TakeLatinLinesFromTheEnglishEngine).
func TestImageTextWordsJoinCamelCaseSplitInCode(t *testing.T) {
	en := [][]ocrWord{
		{{"s.", textBox{762, 177, 24, 14}}, {"db.", textBox{793, 171, 41, 20}}, {"Begin", textBox{842, 171, 76, 25}}, {"Tx(ctx,", textBox{920, 170, 106, 25}}, {"&sql.TxOptions{Isolation:", textBox{1048, 170, 394, 26}}, {"sql.", textBox{1466, 171, 56, 25}}, {"Level", textBox{1530, 171, 76, 20}}, {"Serializable})", textBox{1609, 170, 218, 24}}},
		{{"insert", textBox{1211, 309, 90, 20}}, {"Item(tx,", textBox{1306, 308, 120, 25}}, {"item);", textBox{1451, 308, 87, 25}}, {"err", textBox{1561, 314, 46, 15}}, {"!=", textBox{1630, 310, 25, 19}}},
	}
	assertLines(t, wordLines(1900, 400, nil, en),
		"s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})",
		"insertItem(tx, item); err !=")
}

// Windows reads the hyphens of command-line flags as dashes.
func TestImageTextWordsTurnDashesInLatinWordsIntoHyphens(t *testing.T) {
	en := [][]ocrWord{{{"$", textBox{562, 1384, 10, 20}}, {"git", textBox{587, 1385, 36, 21}}, {"status", textBox{640, 1386, 74, 15}}, {"——short", textBox{730, 1384, 88, 17}}}}
	zh := [][]ocrWord{{{"等", textBox{40, 30, 26, 26}}, {"一", textBox{68, 30, 26, 26}}, {"下", textBox{96, 30, 26, 26}}, {"——", textBox{124, 42, 52, 2}}, {"马", textBox{178, 30, 26, 26}}, {"上", textBox{206, 30, 26, 26}}}}
	assertLines(t, wordLines(1853, 1500, nil, en), "$ git status --short")
	assertLines(t, wordLines(864, 100, zh, nil), "等一下——马上")
}

// A line cut where two tiles' cores meet is joined once, and a word across
// the edge stays whole: both tiles see the same box, so the glyph boxes split
// from it agree.
func TestImageTextWordsJoinLinesAcrossTiles(t *testing.T) {
	tiles := textTiles(3000, 100)
	if len(tiles) != 2 || tiles[0].core.Max.X != 1500 || tiles[1].region.Min.X != 1375 || tiles[0].region.Max.X != 1625 {
		t.Fatalf("unexpected tiles %+v", tiles)
	}
	word := func(text string, x, w float64) ocrWord { return ocrWord{text, textBox{x, 40, w, 20}} }
	tiles[0].addWordLines(fuseWordLines(nil, [][]ocrWord{{word("alpha", 1300, 60), word("beta", 1380, 50), word("straddle", 1460, 100), word("gam", 1580, 45)}}))
	tiles[1].addWordLines(fuseWordLines(nil, [][]ocrWord{{word("beta", 1380, 50), word("straddle", 1460, 100), word("gamma", 1580, 60), word("omega", 1660, 60)}}))
	assertLines(t, mergeTextTiles(tiles), "alpha beta straddle gamma omega")
}

func TestImageTextOCRLanguagesPickChineseAndEnglish(t *testing.T) {
	for _, c := range []struct {
		available []string
		zh, en    string
	}{
		{[]string{"en-GB", "zh-Hans-CN"}, "zh-Hans-CN", "en-GB"},
		{[]string{"zh-Hant-TW", "en-US"}, "zh-Hant-TW", "en-US"},
		{[]string{"zh-Hant-TW", "zh-Hans-CN", "fr-FR", "en-US"}, "zh-Hans-CN", "en-US"},
		{[]string{"zh-TW", "zh-CN"}, "zh-CN", ""},
		{[]string{"fr-FR", "en-US"}, "", "en-US"},
		{nil, "", ""},
	} {
		if zh, en := ocrLanguages(c.available); zh != c.zh || en != c.en {
			t.Errorf("ocrLanguages(%q) = %q, %q; want %q, %q", c.available, zh, en, c.zh, c.en)
		}
	}
}

func TestImageTextLanguageNoteTellsHowToAddChinese(t *testing.T) {
	if note := imageTextLanguageNote([]string{"en-US", "zh-Hans-CN"}); note != "" {
		t.Fatal("note with Chinese installed:", note)
	}
	if note := imageTextLanguageNote([]string{"en-US"}); !strings.Contains(note, "未安装中文文字识别") || !strings.Contains(note, "光学字符识别") {
		t.Fatal("missing Chinese note:", note)
	}
	if note := imageTextLanguageNote(nil); !strings.Contains(note, "没有安装文字识别语言") {
		t.Fatal("missing languages note:", note)
	}
}
