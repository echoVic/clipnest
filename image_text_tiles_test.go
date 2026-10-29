package main

import (
	"image"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestImageTextTilesCoverEveryPixelOnceWithinSizeLimits(t *testing.T) {
	// The fewest tiles whose regions fit the side and keep the overlap.
	for _, size := range [][3]int{{800, 600, 1}, {1900, 1900, 1}, {1901, 400, 2}, {2560, 1664, 2}, {3550, 1000, 2}, {5120, 2880, 6}, {6016, 3384, 8}, {1600, 9000, 6}, {16000, 3125, 20}} {
		tiles := textTiles(size[0], size[1])
		if len(tiles) != size[2] {
			t.Fatalf("%v: %d tiles", size, len(tiles))
		}
		bounds := image.Rect(0, 0, size[0], size[1])
		// Owned glyphs keep half the overlap from a neighbour's cut; a core
		// reaches its region's edge only at the image's edge.
		margin := func(coreEdge, regionEdge, imageEdge int) bool {
			gap := max(coreEdge-regionEdge, regionEdge-coreEdge)
			if regionEdge == imageEdge {
				return gap == 0
			}
			return gap >= imageTextTileOverlap/2
		}
		for _, tile := range tiles {
			r, c := tile.region, tile.core
			if !r.In(bounds) || r.Dx() > imageTextTileSide || r.Dy() > imageTextTileSide || !c.In(r) ||
				!margin(c.Min.X, r.Min.X, 0) || !margin(c.Max.X, r.Max.X, size[0]) || !margin(c.Min.Y, r.Min.Y, 0) || !margin(c.Max.Y, r.Max.Y, size[1]) {
				t.Fatalf("%v: tile %+v outside limits", size, tile)
			}
		}
		if len(tiles) == 1 && (size[0] > imageTextTileSide || size[1] > imageTextTileSide || tiles[0].region != bounds || tiles[0].core != bounds) {
			t.Fatalf("%v: single tile must be the whole image", size)
		}
		for y := 0; y < size[1]; y += 97 {
			for x := 0; x < size[0]; x += 89 {
				owners := 0
				for _, tile := range tiles {
					if image.Pt(x, y).In(tile.core) {
						owners++
					}
				}
				if owners != 1 {
					t.Fatalf("%v: pixel (%d,%d) owned by %d tiles", size, x, y, owners)
				}
			}
		}
		for i := 1; i < len(tiles); i++ {
			a, b := tiles[i-1].region.Min, tiles[i].region.Min
			if !(b.Y == a.Y && b.X > a.X || b.Y > a.Y && b.X == 0) {
				t.Fatalf("%v: tiles are not in row-major order", size)
			}
		}
	}
}

func TestImageTextTilesAskForGlyphsOnlyForLinesCrossingTheirCore(t *testing.T) {
	tiles := fourTiles()
	for _, c := range []struct {
		tile int
		box  textBox
		want bool
	}{
		{0, textBox{700, 100, 420, 30}, true},
		{1, textBox{880, 100, 840, 30}, true},
		{3, textBox{880, 990, 840, 30}, true},
		{0, textBox{20, 100, 900, 30}, false},  // inside the core
		{1, textBox{880, 100, 100, 30}, false}, // only in the overlap
		{0, textBox{-10, 100, 500, 30}, false}, // the image's edges are not cuts
		{1, textBox{1800, 100, 400, 30}, false},
		{0, textBox{700, 985, 420, 30}, false}, // centred on the edge the row below owns
		{2, textBox{700, 985, 420, 30}, true},
	} {
		if got := tiles[c.tile].needsGlyphs(c.box); got != c.want {
			t.Fatalf("tile %d needsGlyphs(%v) = %v", c.tile, c.box, got)
		}
	}
}

// glyphs lays text out left to right in fixed-width cells; spaces have no box.
func glyphs(text string, x, y, w, h float64) []textChar {
	chars := []textChar{}
	for _, r := range text {
		ch := textChar{text: string(r)}
		if r != ' ' {
			ch.box = textBox{x, y, w, h}
		}
		chars = append(chars, ch)
		x += w
	}
	return chars
}

func line(text string, x, y, w, h float64, withGlyphs bool) textObservation {
	o := textObservation{text: text, box: textBox{x, y, float64(len([]rune(text))) * w, h}}
	if withGlyphs {
		o.chars = glyphs(text, x, y, w, h)
	}
	return o
}

func twoTiles() []textTile {
	// Cores meet at x=1000; both regions overlap 875..1125.
	return []textTile{
		{region: image.Rect(0, 0, 1125, 600), core: image.Rect(0, 0, 1000, 600)},
		{region: image.Rect(875, 0, 2000, 600), core: image.Rect(1000, 0, 2000, 600)},
	}
}

func fourTiles() []textTile {
	// Cores meet at x=1000 and y=1000.
	return []textTile{
		{region: image.Rect(0, 0, 1125, 1125), core: image.Rect(0, 0, 1000, 1000)},
		{region: image.Rect(875, 0, 2000, 1125), core: image.Rect(1000, 0, 2000, 1000)},
		{region: image.Rect(0, 875, 1125, 2000), core: image.Rect(0, 1000, 1000, 2000)},
		{region: image.Rect(875, 875, 2000, 2000), core: image.Rect(1000, 1000, 2000, 2000)},
	}
}

func TestImageTextMergeJoinsLinesCutAtTileEdges(t *testing.T) {
	tiles := twoTiles()
	long := "Recognized text becomes searchable" // 34 cells of 30 px from x=700
	// Each tile misreads glyphs cut by its region edge; neither owns them.
	// 识 is centred on the edge, which the right core owns.
	tiles[0].lines = []textObservation{
		line("sidebar.go", 20, 40, 30, 30, false),
		line(long[:14], 700, 100, 30, 30, true),
		line("both tiles see me", 880, 300, 10, 20, false),
		line("中文跨块识别结杲", 865, 200, 30, 30, true),
		line("foo", 985, 500, 30, 30, true),       // every glyph is the neighbour's
		line("tail lost", 850, 450, 30, 30, true), // the right tile did not return the rest
	}
	tiles[1].lines = []textObservation{
		line(long[6:], 880, 100, 30, 30, true),
		line("both tiles see me", 880, 300, 10, 20, false),
		line("申文跨块识别结果", 865, 200, 30, 30, true),
		line("right panel", 1500, 40, 30, 30, false),
		line("footer", 985, 500, 30, 30, true),
	}
	got := mergeTextTiles(tiles)
	want := []string{"sidebar.go", "Recognized text becomes searchable", "both tiles see me", "中文跨块识别结果", "tail", "right panel", "footer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged lines\n got %q\nwant %q", got, want)
	}
}

func TestImageTextMergeJoinsCutPartsAsTheyWere(t *testing.T) {
	tiles := twoTiles()
	// Only one part of each line has glyph boxes when the other tile returned
	// its part as a line of its own. One part of a cut line can be far from
	// the cut.
	total := append(glyphs("Total ", 700, 300, 30, 30), glyphs("42", 1050, 300, 30, 30)...)
	size := glyphs("Size ", 880, 400, 30, 30)
	tiles[0].lines = []textObservation{
		{text: "Total 42", box: textBox{700, 300, 410, 30}, chars: total},
		line("width 1920", 800, 350, 25, 30, true), // cut between 19 and 20
		{text: "Size 1", box: textBox{880, 400, 245, 30}, chars: append(size, glyphs("1", 1095, 400, 30, 30)...)},
		line("共3页", 925, 500, 30, 30, true), // 页 is centred on the cut
	}
	tiles[1].lines = []textObservation{
		line("42 MB", 1050, 300, 30, 30, false),
		line("20 px", 1000, 350, 25, 30, false),
		{text: "Size 1.2 GB", box: textBox{880, 400, 395, 30}, chars: append(size, glyphs("1.2 GB", 1095, 400, 30, 30)...)},
		line("共3页", 925, 500, 30, 30, true),
	}
	if got, want := mergeTextTiles(tiles), []string{"Total 42 MB", "width 1920 px", "Size 1.2 GB", "共3页"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cut lines not joined\n got %q\nwant %q", got, want)
	}
}

func TestImageTextMergeJoinsNearEdgeLinesWithASpace(t *testing.T) {
	tiles := twoTiles()
	// Recognition stopped short of the cut on one or both sides. The left tile
	// did not return the glyphs at 950..1000 that it owns. The right tile
	// returned "space" as a line of its own, after the left part kept the space
	// before it. A part of lowercase letters is shorter than the line it
	// continues.
	tiles[0].lines = []textObservation{
		line("ends near", 700, 100, 25, 30, false), // to 925
		line("left part", 700, 200, 25, 30, false),
		line("keep one spa", 790, 300, 25, 30, true),
		line("status", 760, 500, 25, 30, false), // ends exactly three heights before the cut
	}
	tiles[1].lines = []textObservation{
		line("the cut", 1060, 110, 25, 30, false), // from 1060, two thirds level with the left part
		line("~~ and right", 950, 200, 25, 30, true),
		line("space", 1015, 300, 25, 30, false),
		line("other row", 1060, 400, 25, 30, false),
		line("ok", 1042, 508, 25, 14, false), // starts exactly three heights after the cut
	}
	got := mergeTextTiles(tiles)
	if want := []string{"ends near the cut", "left part and right", "keep one space", "status ok", "other row"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("near-edge parts not joined\n got %q\nwant %q", got, want)
	}
}

func TestImageTextMergeJoinsTheNearestPart(t *testing.T) {
	tiles := twoTiles()
	tiles[0].lines = []textObservation{line("ends near", 700, 100, 25, 30, false), line("!", 940, 100, 25, 30, false)}
	tiles[1].lines = []textObservation{line("the cut", 1060, 100, 25, 30, false)}
	if got := mergeTextTiles(tiles); !reflect.DeepEqual(got, []string{"ends near", "! the cut"}) {
		t.Fatalf("continuation not joined to the nearest part: %q", got)
	}
}

func TestImageTextMergeJoinsEachPartOnce(t *testing.T) {
	tiles := twoTiles()
	tiles[0].lines = []textObservation{line("ends near", 700, 100, 25, 30, false), line("!", 940, 100, 25, 30, false)}
	tiles[1].lines = []textObservation{line("|", 1010, 100, 25, 30, false), line("the cut", 1060, 100, 25, 30, false)}
	got := strings.Fields(strings.Join(mergeTextTiles(tiles), " "))
	slices.Sort(got)
	if want := []string{"!", "cut", "ends", "near", "the", "|"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("words lost or repeated: %q", got)
	}
}

func TestImageTextMergeKeepsSeparateColumnsApart(t *testing.T) {
	tiles := twoTiles()
	// Parts more than three text heights from the cut do not join.
	tiles[0].lines = []textObservation{
		line("README.md", 20, 100, 30, 30, false),
		line("beyond", 720, 200, 30, 30, false), // ends 100 px before the cut
		line("close", 810, 250, 30, 30, false),
		line("row one", 750, 300, 30, 30, false),
	}
	tiles[1].lines = []textObservation{
		line("func main() {", 1020, 100, 30, 30, false),
		line("v0.1.8", 1800, 100, 30, 30, false), // ends at the image's edge, not a cut
		line("near", 1020, 200, 30, 30, false),
		line("reach", 1100, 250, 30, 30, false),   // starts 100 px after the cut
		line("row two", 1020, 320, 30, 30, false), // overlaps row one by a third
	}
	want := []string{"README.md", "beyond", "close", "row one", "func main() {", "v0.1.8", "near", "reach", "row two"}
	if got := mergeTextTiles(tiles); !reflect.DeepEqual(got, want) {
		t.Fatalf("separate lines joined\n got %q\nwant %q", got, want)
	}
}

func TestImageTextMergeChainsThroughMiddleTiles(t *testing.T) {
	// Cores meet at x=500 and x=1000.
	tiles := []textTile{
		{region: image.Rect(0, 0, 625, 600), core: image.Rect(0, 0, 500, 600)},
		{region: image.Rect(375, 0, 1125, 600), core: image.Rect(500, 0, 1000, 600)},
		{region: image.Rect(875, 0, 1500, 600), core: image.Rect(1000, 0, 1500, 600)},
	}
	a, b := "beta gamma delta epsilon", "zeta eta theta"
	tiles[0].lines = []textObservation{
		line("alpha", 320, 100, 30, 30, false),
		line("delta", 320, 300, 30, 30, false),
		line("label", 330, 500, 30, 30, false), // the middle tile has nothing on this row
	}
	tiles[1].lines = []textObservation{
		line(a[:20], 530, 100, 30, 30, true), // starts near the cut
		line(b, 700, 300, 30, 30, true),      // starts far from it
	}
	tiles[2].lines = []textObservation{
		line(a[12:], 890, 100, 30, 30, true),
		line(b[6:], 880, 300, 30, 30, true),
		line("value", 1020, 500, 30, 30, false),
	}
	got := mergeTextTiles(tiles)
	if want := []string{"alpha beta gamma delta epsilon", "delta", "label", "zeta eta theta", "value"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chained lines\n got %q\nwant %q", got, want)
	}
}

func TestImageTextMergeWithoutGlyphBoxesKeepsOneCopy(t *testing.T) {
	tiles := twoTiles()
	whole := line("no glyph boxes here at all", 760, 100, 20, 30, false) // centre 1020
	tiles[0].lines = []textObservation{whole}
	tiles[1].lines = []textObservation{whole}
	if got := mergeTextTiles(tiles); !reflect.DeepEqual(got, []string{"no glyph boxes here at all"}) {
		t.Fatalf("crossing line without glyph boxes: %q", got)
	}
}

func TestImageTextMergeOwnsRowsByLineCentre(t *testing.T) {
	tiles := fourTiles()
	tiles[0].lines = []textObservation{
		line("upper", 10, 960, 30, 30, false),
		line("cut at region edge", 10, 1110, 30, 15, false),
		line("straddles the", 700, 984, 20, 30, false), // centre 999
	}
	tiles[2].lines = []textObservation{line("upper", 10, 960, 30, 30, false), line("lower", 10, 1110, 30, 30, false)}
	// Measured a little lower on the right, centred on the edge the row below owns.
	tiles[1].lines = []textObservation{line("row boundary", 1040, 985, 20, 30, false)}
	tiles[3].lines = tiles[1].lines
	got := mergeTextTiles(tiles)
	if want := []string{"upper", "straddles the row boundary", "lower"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows not owned by centre\n got %q\nwant %q", got, want)
	}
}
