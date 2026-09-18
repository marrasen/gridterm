package render

import (
	"image/color"
	"testing"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
)

// cellSizes are the ones a real font gives: a small window, the default,
// and a large one. The narrow ones are where a bar can fall between
// pixels.
var cellSizes = []glyph.Metrics{
	{CellW: 5, CellH: 11, Ascent: 9},
	{CellW: 7, CellH: 15, Ascent: 12},
	{CellW: 9, CellH: 19, Ascent: 15},
	{CellW: 12, CellH: 25, Ascent: 20},
	{CellW: 24, CellH: 48, Ascent: 38},
}

// plainGeo measures a grid with no padding, which is what art is drawn
// against unless something asked for room around it.
func plainGeo(m glyph.Metrics) *Geometry {
	geo := &Geometry{}
	geo.Layout(grid.New(40, 20, color.RGBA{}, color.RGBA{}), m)
	return geo
}

// Every bar is a whole pixel wide and they tile the cell exactly.
//
// Quads are not antialiased, so a bar narrower than a pixel is only
// drawn when a pixel centre happens to fall inside it -- and the same
// seconds vanish every time, which is the one thing a graph of a run
// must not do.
func TestGraphBarsAreWholePixelsAndTileTheCell(t *testing.T) {
	heights := make([]int, grid.ArtGraphBars)
	for i := range heights {
		heights[i] = grid.ArtGraphMax
	}
	art := grid.Graph(heights)

	for _, m := range cellSizes {
		for _, at := range [][2]int{{0, 0}, {3, 2}} {
			bars := graphBars(art, at[0], at[1], plainGeo(m))
			if len(bars) == 0 {
				t.Fatalf("%+v: no bars at all", m)
			}
			want := min(grid.ArtGraphBars, m.CellW)
			if len(bars) != want {
				t.Fatalf("%+v: %d bars, want %d", m, len(bars), want)
			}
			left := float32(at[0] * m.CellW)
			at := left
			for i, b := range bars {
				if b.W < 1 {
					t.Fatalf("%+v: bar %d is %v wide, want at least a pixel", m, i, b.W)
				}
				if b.X != at {
					t.Fatalf("%+v: bar %d starts at %v, want %v", m, i, b.X, at)
				}
				at = b.X + b.W
			}
			if right := left + float32(m.CellW); at != right {
				t.Fatalf("%+v: the bars end at %v, want the cell's edge %v", m, at, right)
			}
		}
	}
}

// Only the seconds really measured are drawn, so a run of two is not
// drawn as ten quiet ones and two busy.
func TestGraphDrawsOnlyWhatWasMeasured(t *testing.T) {
	m := glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38}
	for _, n := range []int{1, 2, 5, grid.ArtGraphBars} {
		heights := make([]int, n)
		for i := range heights {
			heights[i] = 8
		}
		if got := len(graphBars(grid.Graph(heights), 0, 0, plainGeo(m))); got != n {
			t.Fatalf("a run of %d seconds drew %d bars", n, got)
		}
	}
	if got := graphBars(grid.Art{}, 0, 0, plainGeo(m)); got != nil {
		t.Fatalf("art that is not a graph drew %v", got)
	}
}

// The bars stay inside the cell, and sit on the baseline.
func TestGraphStaysInsideItsCell(t *testing.T) {
	art := grid.Graph([]int{0, grid.ArtGraphMax, 4})
	for _, m := range cellSizes {
		foot := float32(2*m.CellH + m.Ascent)
		for i, b := range graphBars(art, 1, 2, plainGeo(m)) {
			if b.H < 1 {
				t.Errorf("%+v: bar %d is %v high, want at least a pixel", m, i, b.H)
			}
			if b.Y+b.H != foot {
				t.Errorf("%+v: bar %d ends at %v, want the baseline %v", m, i, b.Y+b.H, foot)
			}
			if b.Y < float32(2*m.CellH) {
				t.Errorf("%+v: bar %d starts at %v, above its own row", m, i, b.Y)
			}
		}
	}
}

// Art follows the same rules a glyph on that cell would: hidden is not
// drawn, and dim is mixed towards the background.
func TestArtColourFollowsTheCell(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{A: 255}
	g := grid.New(4, 2, white, black)
	art := grid.Graph([]int{1})

	plain := grid.Cell{Rune: ' ', FG: white, BG: black, Width: 1, Art: art}
	g.Set(0, 0, plain)
	full, ok := artColour(g, 0, 0, plain)
	if !ok {
		t.Fatal("an ordinary cell's art is not drawn")
	}

	dim := plain
	dim.Attr = grid.AttrDim
	g.Set(1, 0, dim)
	dimmed, ok := artColour(g, 1, 0, dim)
	if !ok {
		t.Fatal("a dim cell's art is not drawn at all")
	}
	if dimmed == full {
		t.Fatal("a dim cell's art is drawn at full brightness")
	}

	hidden := plain
	hidden.Attr = grid.AttrHidden
	g.Set(2, 0, hidden)
	if _, ok := artColour(g, 2, 0, hidden); ok {
		t.Fatal("a hidden cell's art is drawn")
	}
}

// Every icon draws something, inside the cells it is given, with no
// rectangle thinner than a pixel.
//
// Quads are not antialiased, so a rectangle narrower than a pixel is
// drawn only when a pixel centre falls inside it: half an icon would go
// missing at the sizes where an icon is doing the most work.
func TestEveryIconDrawsInsideTheCellsItIsGiven(t *testing.T) {
	kinds := []grid.IconKind{
		grid.IconTerminal, grid.IconCommand, grid.IconFiles, grid.IconTunnel,
	}
	for _, cols := range []int{1, 2} {
		for _, kind := range kinds {
			for _, m := range cellSizes {
				iconInside(t, kind, cols, m)
			}
		}
	}
}

// iconInside checks one icon at one size stays in the cells it is given.
func iconInside(t *testing.T, kind grid.IconKind, cols int, m glyph.Metrics) {
	t.Helper()
	bars := iconBars(grid.Icon(kind), 2, 3, cols, plainGeo(m))
	if len(bars) == 0 {
		t.Fatalf("icon %d at %+v drew nothing", kind, m)
	}
	left := float32(2 * m.CellW)
	right := left + float32(cols*m.CellW)
	top := float32(3 * m.CellH)
	foot := float32(3*m.CellH + m.Ascent)
	for i, b := range bars {
		if b.W < 1 || b.H < 1 {
			t.Fatalf("icon %d at %+v: piece %d is %vx%v", kind, m, i, b.W, b.H)
		}
		if b.X < left || b.X+b.W > right {
			t.Fatalf("icon %d at %+v: piece %d spans %v..%v, outside %v..%v",
				kind, m, i, b.X, b.X+b.W, left, right)
		}
		if b.Y < top || b.Y+b.H > foot {
			t.Fatalf("icon %d at %+v: piece %d spans %v..%v, outside %v..%v",
				kind, m, i, b.Y, b.Y+b.H, top, foot)
		}
	}
}

// Each icon is a different picture: one that looked like another would
// say the wrong thing about the row it is on.
func TestTheIconsAreToldApart(t *testing.T) {
	m := glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38}
	seen := map[string]grid.IconKind{}
	for _, kind := range []grid.IconKind{
		grid.IconTerminal, grid.IconCommand, grid.IconFiles, grid.IconTunnel,
	} {
		var shape string
		for _, b := range iconBars(grid.Icon(kind), 0, 0, 2, plainGeo(m)) {
			shape += string(rune('0'+int(b.X))) + string(rune('0'+int(b.Y))) +
				string(rune('0'+int(b.W))) + string(rune('0'+int(b.H)))
		}
		if was, ok := seen[shape]; ok {
			t.Fatalf("icons %d and %d are the same picture", was, kind)
		}
		seen[shape] = kind
	}
}

// Art that is not an icon draws no icon, and a kind that does not exist
// draws nothing rather than reading past the end of the list.
func TestIconBarsRefusesWhatIsNotAnIcon(t *testing.T) {
	m := glyph.Metrics{CellW: 12, CellH: 25, Ascent: 20}
	for _, art := range []grid.Art{
		{},
		grid.Graph([]int{1, 2}),
		{Kind: grid.ArtIcon, Data: 99},
	} {
		if got := iconBars(art, 0, 0, 2, plainGeo(m)); got != nil {
			t.Fatalf("%v drew %v", art, got)
		}
	}
}

// Every kind of art reaches its own shapes. A kind the renderer does not
// know about draws nothing rather than drawing the wrong thing.
func TestArtBarsReachesEveryKind(t *testing.T) {
	m := glyph.Metrics{CellW: 12, CellH: 25, Ascent: 20}
	for _, art := range []grid.Art{
		grid.Graph([]int{1, 2, 3}),
		grid.Icon(grid.IconTerminal),
		grid.Icon(grid.IconFiles),
		grid.Icon(grid.IconTunnel),
		grid.Icon(grid.IconCommand),
	} {
		if got := artBars(art, 0, 0, 2, plainGeo(m)); len(got) == 0 {
			t.Fatalf("%v drew nothing", art)
		}
	}
	if got := artBars(grid.Art{}, 0, 0, 2, plainGeo(m)); got != nil {
		t.Fatalf("no art drew %v", got)
	}
}

// An icon is taller than one cell is wide. A cell is about half as wide
// as it is tall, so one squeezed into a single cell is shorter than the
// letters beside it and its strokes collapse to a pixel.
func TestAnIconIsTallerThanOneCellIsWide(t *testing.T) {
	for _, m := range cellSizes {
		bars := iconBars(grid.Icon(grid.IconTerminal), 2, 3, 2, plainGeo(m))

		var top, foot float32 = 1 << 20, 0
		for _, b := range bars {
			top = min(top, b.Y)
			foot = max(foot, b.Y+b.H)
		}

		got := foot - top
		if want := float32(min(2*m.CellW-m.CellW/2, m.Ascent)); got != want {
			t.Errorf("at %+v the icon is %v tall, want %v", m, got, want)
		}
		if got <= float32(m.CellW) {
			t.Errorf("at %+v the icon is %v tall, no more than the %d a single cell allows",
				m, got, m.CellW)
		}
	}
}

// An icon with one cell to itself fills it. There is no second cell to
// take a gap out of, so taking one anyway would leave a smudge where the
// picture was.
func TestAnIconWithOneCellFillsIt(t *testing.T) {
	for _, m := range cellSizes {
		bars := iconBars(grid.Icon(grid.IconTerminal), 2, 3, 1, plainGeo(m))

		var top, foot float32 = 1 << 20, 0
		for _, b := range bars {
			top = min(top, b.Y)
			foot = max(foot, b.Y+b.H)
		}

		if want := float32(min(m.CellW, m.Ascent)); foot-top != want {
			t.Errorf("at %+v the icon is %v tall, want the %v its one cell allows",
				m, foot-top, want)
		}
	}
}

// And one asked for more cells than the grid has left stays inside it.
func TestAnIconAtTheEndOfARowStaysInTheGrid(t *testing.T) {
	m := glyph.Metrics{CellW: 12, CellH: 25, Ascent: 20}
	geo := plainGeo(m)
	last := geo.Cols() - 1

	bars := iconBars(grid.Icon(grid.IconTerminal), last, 3, 2, geo)

	for i, b := range bars {
		if want := float32(geo.CellX(last) + m.CellW); b.X+b.W > want {
			t.Errorf("piece %d ends at %v, past the %v the grid does", i, b.X+b.W, want)
		}
	}
}

// And it leaves a gap before the text, which starts in the cell after
// the one it takes. An icon filling both cells would sit against the
// first letter.
func TestAnIconLeavesAGapBeforeTheText(t *testing.T) {
	for _, m := range cellSizes {
		bars := iconBars(grid.Icon(grid.IconFiles), 2, 3, 2, plainGeo(m))

		var left, right float32 = 1 << 20, 0
		for _, b := range bars {
			left = min(left, b.X)
			right = max(right, b.X+b.W)
		}

		// It starts where the text on a row without one would.
		if want := float32(2 * m.CellW); left != want {
			t.Errorf("at %+v the icon starts at %v, want %v", m, left, want)
		}
		text := float32(4 * m.CellW)
		if gap := text - right; gap < 2 {
			t.Errorf("at %+v the icon ends %v before the text, which reads as touching it", m, gap)
		}
	}
}

// An icon sits on the baseline, so it lines up with the text on its row.
func TestAnIconSitsOnTheBaseline(t *testing.T) {
	m := glyph.Metrics{CellW: 12, CellH: 25, Ascent: 20}

	bars := iconBars(grid.Icon(grid.IconFiles), 2, 3, 2, plainGeo(m))

	var foot float32
	for _, b := range bars {
		foot = max(foot, b.Y+b.H)
	}
	if want := float32(3*m.CellH + m.Ascent); foot != want {
		t.Errorf("the icon rests at %v, want the baseline at %v", foot, want)
	}
}

// A row gives an icon the cell after it when that one is empty, and
// keeps it to one cell when something else is there.
func TestAnIconTakesTheCellAfterItOnlyWhenItIsEmpty(t *testing.T) {
	for _, c := range []struct {
		what string
		next grid.Cell
		want int
	}{
		{"nothing at all", grid.Cell{}, 2},
		{"a space", grid.Cell{Rune: ' ', Width: 1}, 2},
		{"a letter", grid.Cell{Rune: 'A', Width: 1}, 1},
		{"another icon", grid.Cell{Rune: ' ', Width: 1, Art: grid.Icon(grid.IconFiles)}, 1},
		// A blank cell can still have ink on it, and the icon would be
		// drawn under it.
		{"a combining mark", grid.Cell{Rune: ' ', Width: 1, Comb: []rune{0x0301}}, 1},
		{"an underline", grid.Cell{Rune: ' ', Width: 1, Attr: grid.AttrUnderline}, 1},
		{"a strikethrough", grid.Cell{Rune: ' ', Width: 1, Attr: grid.AttrStrike}, 1},
	} {
		g := grid.New(4, 2, color.RGBA{}, color.RGBA{})
		g.Set(1, 0, grid.Cell{Rune: ' ', Width: 1, Art: grid.Icon(grid.IconTerminal)})
		g.Set(2, 0, c.next)

		if got := artCols(g, 1, 0); got != c.want {
			t.Errorf("with %s after it an icon gets %d cells, want %d", c.what, got, c.want)
		}
	}
	// And the last cell of a row has nothing after it to take.
	g := grid.New(2, 1, color.RGBA{}, color.RGBA{})
	g.Set(1, 0, grid.Cell{Rune: ' ', Width: 1, Art: grid.Icon(grid.IconTerminal)})
	if got := artCols(g, 1, 0); got != 1 {
		t.Errorf("an icon in the last cell gets %d cells, want 1", got)
	}
}

// A rule runs along the side it is given and is only as thick as it was
// asked for.
func TestAnEdgeRunsAlongTheSideItIsGiven(t *testing.T) {
	geo := plainGeo(glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38})
	const thick = 2

	bars := edgeBars(grid.Edges(grid.EdgeTop, thick), 3, 4, geo)

	if len(bars) != 1 {
		t.Fatalf("a rule along one side is %d bars, want the one", len(bars))
	}
	if bars[0].H != thick {
		t.Errorf("the rule is %v thick, want %d", bars[0].H, thick)
	}
	if got, want := bars[0].Y, float32(geo.CellY(4)); got != want {
		t.Errorf("the rule is at y %v, want the top of the cell at %v", got, want)
	}
}

// Two cells side by side make one line, with no gap where the grid pads
// between them and no doubled thickness at the join.
//
// The rule runs along the cell's outer box for this. Along the inner box
// it would stop at the glyph and leave the padding bare, which is a line
// in dashes wherever a grid has padding in it.
func TestARuleMeetsItsNeighbours(t *testing.T) {
	g := grid.New(40, 20, color.RGBA{}, color.RGBA{})
	// A pad between the two columns the test looks at.
	g.SetColPad(1, grid.Pad{After: 2})
	geo := &Geometry{}
	geo.Layout(g, glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38})
	if before, after := geo.ColGap(1); before == 0 && after == 0 {
		t.Fatal("the geometry pads no column, so this proves nothing")
	}

	first := edgeBars(grid.Edges(grid.EdgeTop, 2), 1, 0, geo)
	next := edgeBars(grid.Edges(grid.EdgeTop, 2), 2, 0, geo)

	if len(first) != 1 || len(next) != 1 {
		t.Fatalf("the two cells gave %d and %d bars, want one each", len(first), len(next))
	}
	if got, want := next[0].X, first[0].X+first[0].W; got != want {
		t.Errorf("the next cell's rule starts at %v and this one ends at %v, "+
			"so the line is broken", got, want)
	}
}

// A corner is painted once. The border is never opaque, so a corner
// drawn twice reads as a brighter dot on it.
func TestACornerIsPaintedOnce(t *testing.T) {
	geo := plainGeo(glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38})
	const thick = 2

	bars := edgeBars(grid.Edges(grid.EdgeTop|grid.EdgeLeft, thick), 0, 0, geo)

	if len(bars) != 2 {
		t.Fatalf("a corner is %d bars, want the two", len(bars))
	}
	for i, a := range bars {
		for _, b := range bars[i+1:] {
			if overlap(a, b) {
				t.Errorf("the rules at %v and %v overlap, so the corner is painted twice", a, b)
			}
		}
	}
}

// overlap reports whether two bars cover any pixel in common.
func overlap(a, b bar) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// A rule along no side draws nothing.
func TestAnEdgeAlongNoSideDrawsNothing(t *testing.T) {
	m := glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38}
	if got := edgeBars(grid.Edges(0, 2), 0, 0, plainGeo(m)); len(got) != 0 {
		t.Errorf("it drew %d bars, want none", len(got))
	}
}

// A rule thicker than the cell is held to the cell, so it never spills
// into the one beside it.
func TestAnEdgeThickerThanTheCellIsHeldToIt(t *testing.T) {
	geo := plainGeo(glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38})
	w, h := geo.CellW(), geo.CellH()

	bars := edgeBars(grid.Edges(grid.EdgeTop, w+h+99), 0, 0, geo)

	if len(bars) != 1 {
		t.Fatalf("it drew %d bars, want the one", len(bars))
	}
	if bars[0].H > float32(h) {
		t.Errorf("the rule is %v tall, want no more than the cell's %d", bars[0].H, h)
	}
}

// Art that is not an edge says it runs along no side, so a graph read as
// one draws nothing rather than a rule of whatever its bars happen to be.
func TestArtThatIsNotAnEdgeRunsAlongNoSide(t *testing.T) {
	m := glyph.Metrics{CellW: 24, CellH: 48, Ascent: 38}
	if got := edgeBars(grid.Graph([]int{200, 200, 200}), 0, 0, plainGeo(m)); len(got) != 0 {
		t.Errorf("a graph read as an edge drew %d bars, want none", len(got))
	}
}
