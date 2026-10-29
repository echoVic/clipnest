package main

import (
	"image"
	"strings"
)

// Vision returns no lines at all when text is shorter than about 1/180 of the
// image's long side, so larger images are recognized in overlapping tiles.
const (
	imageTextTileSide    = 1900
	imageTextTileOverlap = 250
)

// textBox is in image pixels with a top-left origin.
type textBox struct{ X, Y, W, H float64 }

func (b textBox) right() float64   { return b.X + b.W }
func (b textBox) bottom() float64  { return b.Y + b.H }
func (b textBox) centerX() float64 { return b.X + b.W/2 }
func (b textBox) centerY() float64 { return b.Y + b.H/2 }

func (b textBox) union(o textBox) textBox {
	x, y := min(b.X, o.X), min(b.Y, o.Y)
	return textBox{x, y, max(b.right(), o.right()) - x, max(b.bottom(), o.bottom()) - y}
}

type textChar struct {
	text string
	box  textBox // zero for whitespace
}

// textObservation is a recognized line; chars is set only for lines that
// cross an edge where two tiles' cores meet.
type textObservation struct {
	text  string
	box   textBox
	chars []textChar
}

// A tile recognizes its region and owns its core. Neighbouring cores meet in
// the middle of the overlap, so an owned glyph is at least half the overlap
// away from the edge where the neighbour's region cut it.
type textTile struct {
	region, core image.Rectangle
	lines        []textObservation
}

type textSpan struct{ start, end, coreStart, coreEnd int }

// textSpans splits n pixels into the fewest equal spans no longer than the
// tile side, evenly spaced so that neighbours overlap by at least the tile
// overlap. Cores meet in the middle of each overlap.
func textSpans(n int) []textSpan {
	const side, overlap = imageTextTileSide, imageTextTileOverlap
	if n <= side {
		return []textSpan{{0, n, 0, n}}
	}
	count := (n - overlap + side - overlap - 1) / (side - overlap)
	size := (n + (count-1)*overlap + count - 1) / count
	spans := make([]textSpan, count)
	for i := range spans {
		start := i * (n - size) / (count - 1)
		spans[i] = textSpan{start, start + size, 0, n}
		if i > 0 {
			spans[i].coreStart = (start + spans[i-1].end) / 2
			spans[i-1].coreEnd = spans[i].coreStart
		}
	}
	return spans
}

// textTiles returns the tiles in row-major order; an image within the tile
// side is a single tile.
func textTiles(width, height int) []textTile {
	cols := textSpans(width)
	tiles := []textTile{}
	for _, y := range textSpans(height) {
		for _, x := range cols {
			tiles = append(tiles, textTile{
				region: image.Rect(x.start, y.start, x.end, y.end),
				core:   image.Rect(x.coreStart, y.coreStart, x.coreEnd, y.coreEnd),
			})
		}
	}
	return tiles
}

// A row of text belongs to the tile whose core contains the line's centre.
func (t textTile) ownsRow(b textBox) bool {
	return b.centerY() >= float64(t.core.Min.Y) && b.centerY() < float64(t.core.Max.Y)
}

func (t textTile) ownsColumn(x float64) bool {
	return x >= float64(t.core.Min.X) && x < float64(t.core.Max.X)
}

// needsGlyphs reports a line of the tile's rows that crosses an edge where its
// core meets a neighbour's; only these lines need per-glyph boxes to merge.
func (t textTile) needsGlyphs(b textBox) bool {
	crosses := func(edge int) bool { return b.X < float64(edge) && b.right() > float64(edge) }
	return t.ownsRow(b) && (t.core.Min.X > t.region.Min.X && crosses(t.core.Min.X) ||
		t.core.Max.X < t.region.Max.X && crosses(t.core.Max.X))
}

// textPart is the text of a line that one tile owns.
type textPart struct {
	core              image.Rectangle
	text              string
	box               textBox
	cutLeft, cutRight bool
	next              *textPart
	joined            bool
}

// ownedGlyphs keeps the glyphs whose centre lies in the core. A space belongs
// with the glyph before it, so a space owned before a cut survives joining.
func (t textTile) ownedGlyphs(chars []textChar) *textPart {
	p := &textPart{core: t.core}
	var text strings.Builder
	owned := false
	for _, ch := range chars {
		if ch.box != (textBox{}) {
			owned = t.ownsColumn(ch.box.centerX())
			switch {
			case !owned && ch.box.centerX() < float64(t.core.Min.X):
				p.cutLeft = true
			case !owned:
				p.cutRight = true
			case text.Len() == 0:
				p.box = ch.box
			default:
				p.box = p.box.union(ch.box)
			}
		}
		if owned {
			text.WriteString(ch.text)
		}
	}
	p.text = text.String()
	return p
}

// mergeTextTiles keeps the text each tile owns and joins lines cut where
// cores meet. Lines follow the tiles' row-major order and Vision's reading
// order within a tile; a joined line takes the place of its first part.
func mergeTextTiles(tiles []textTile) []string {
	parts := []*textPart{}
	for _, t := range tiles {
		for _, l := range t.lines {
			switch {
			case !t.ownsRow(l.box):
			case len(l.chars) > 0:
				if p := t.ownedGlyphs(l.chars); p.text != "" {
					parts = append(parts, p)
				}
			case t.ownsColumn(l.box.centerX()): // without glyph boxes the centre decides
				parts = append(parts, &textPart{core: t.core, text: l.text, box: l.box})
			}
		}
	}
	// A part cut where cores meet continues in the next column's part on the
	// same text row; the tiles' rows differ when the line straddles their edge.
	// Recognition can stop a little short of a region's edge, so parts within
	// three text heights of the edge join too, with a space. The part nearest
	// the edge continues.
	for _, b := range parts {
		if !b.cutLeft && b.box.X-float64(b.core.Min.X) > 3*b.box.H {
			continue
		}
		var a *textPart
		for _, p := range parts {
			if p.next == nil && p.core.Max.X == b.core.Min.X && (p.cutRight || float64(p.core.Max.X)-p.box.right() <= 3*p.box.H) &&
				verticalOverlap(p.box, b.box) > 0.5 && (a == nil || p.box.right() > a.box.right()) {
				a = p
			}
		}
		if a != nil {
			a.next, b.joined = b, true
		}
	}
	lines := []string{}
	for _, p := range parts {
		if p.joined {
			continue
		}
		text := p.text
		// A cut part ends where its continuation begins.
		for q := p; q.next != nil; q = q.next {
			if !q.cutRight {
				text += " "
			}
			text += q.next.text
		}
		lines = append(lines, strings.TrimSpace(text))
	}
	return lines
}

// verticalOverlap is the fraction of the shorter box's height both share.
func verticalOverlap(a, b textBox) float64 {
	return (min(a.bottom(), b.bottom()) - max(a.Y, b.Y)) / min(a.H, b.H)
}
