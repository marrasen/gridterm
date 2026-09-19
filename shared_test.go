package main

import (
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// aSharedWindow is a window with one pane, drawing frames the way the
// program does.
func aSharedWindow(t *testing.T) (*testApp, *term.Terminal) {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withScreen(t, a)
	return a, onlyPaneWidget(t, a).(*term.Terminal)
}

// handOver gives the window's pane to an agent and clears the notice
// that says so.
func handOver(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	if err := a.handPane(pane); err != nil {
		t.Fatalf("hand it over: %v", err)
	}
	dismissNotice(t, a)
}

// sameHue reports whether two colours are the same but for their alpha.
func sameHue(a, b color.RGBA) bool { return a.R == b.R && a.G == b.G && a.B == b.B }

// A pane handed to an agent is drawn with a border round it.
func TestAPaneHandedToAnAgentGetsABorder(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)

	m := a.shared[pane]
	if m == nil {
		t.Fatal("the pane an agent has was given no border")
	}
	area, shown := a.paneArea(pane)
	if !shown {
		t.Fatal("the pane is not in the layout")
	}
	if got := len(rules(m)); got != 1 {
		t.Fatalf("the pane has %d rules round it, want the one", got)
	}
	if got := rules(m)[0].Colour; !sameHue(got, statusAgentFG(a.colours)) {
		t.Errorf("the border is %v, want the cyan an agent is said in", got)
	}
	// Round the pane's own box, so it marks the pane and takes nothing
	// from it.
	wantsRuleOn(t, ruleBox(t, m), paneBox(t, a, area))
}

// The border is over the pane, not in it: the program keeps every row
// and column it was given.
func TestABorderTakesNoRoomFromTheProgram(t *testing.T) {
	a, pane := aSharedWindow(t)
	frame(t, a)
	was := pane.Size()

	handOver(t, a, pane)
	frame(t, a)

	if got := pane.Size(); got != was {
		t.Errorf("the pane is %v with a border and was %v without one", got, was)
	}
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}
	// Rules and nothing else. A layer with a grid would paint every cell
	// it covers, which is the whole pane.
	if m.layer.Grid != nil {
		t.Error("the border's layer has a grid, so it paints over what the program printed")
	}
	if len(m.layer.Strokes) == 0 {
		t.Error("the border's layer draws no rule")
	}
	if m.layer.Hidden {
		t.Error("the border was placed and is still hidden")
	}
	if !inStack(a.comp, m.layer) {
		t.Error("the border's layer is not on the stack, so nothing draws it")
	}
}

// A pane nobody else is in has no border at all.
func TestAPaneNobodyElseIsInHasNoBorder(t *testing.T) {
	a, pane := aSharedWindow(t)
	frame(t, a)

	if m := a.shared[pane]; m != nil {
		t.Error("a pane the user has to themselves was given a border")
	}
}

// The border goes when the agent does, layer and all.
func TestTheBorderGoesWhenTheAgentDoes(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border to take away")
	}

	if !a.agents.forget(pane) {
		t.Fatal("the window did not take the pane back")
	}
	frame(t, a)

	if a.shared[pane] != nil {
		t.Error("the pane still has a border with no agent in it")
	}
	if inStack(a.comp, m.layer) {
		t.Error("the border's layer is still on the stack")
	}
}

// A pane that is handed over and read from somewhere else gets two
// borders, one inside the other, in the two colours.
func TestAPaneWithAnAgentAndAWatcherGetsTwoBorders(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	watchPane(t, pane)
	frame(t, a)

	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}
	if got := len(rules(m)); got != 2 {
		t.Fatalf("the pane has %d rules round it, want two", got)
	}
	if got := rules(m)[0].Colour; !sameHue(got, statusAgentFG(a.colours)) {
		t.Errorf("the outer border is %v, want the cyan an agent is said in", got)
	}
	if got := rules(m)[1].Colour; !sameHue(got, statusTakenFG(a.colours)) {
		t.Errorf("the inner border is %v, want the red a watcher is said in", got)
	}
	// One inside the other, or the second would be hidden under the first.
	outer, inner := rules(m)[0].Rect, rules(m)[1].Rect
	if inner.Empty() {
		t.Fatalf("the inner border is an empty box at %v", inner)
	}
	if !inner.In(outer) || inner == outer {
		t.Errorf("the inner border is at %v and the outer at %v, want it inside", inner, outer)
	}
	if got := inner.Min.X - outer.Min.X; got != markWidth+markGap {
		t.Errorf("the borders are %d pixels apart, want %d", got, markWidth+markGap)
	}
}

// A watcher on their own gets the one border, in their colour, on the
// outside.
func TestAWatchedPaneGetsTheWatcherColourOnTheOutside(t *testing.T) {
	a, pane := aSharedWindow(t)
	watchPane(t, pane)
	frame(t, a)

	m := a.shared[pane]
	if m == nil {
		t.Fatal("the pane somebody is reading was given no border")
	}
	if got := len(rules(m)); got != 1 {
		t.Fatalf("the pane has %d rules round it, and only one window is reading it", got)
	}
	if got := rules(m)[0].Colour; !sameHue(got, statusTakenFG(a.colours)) {
		t.Errorf("the border is %v, want the red a watcher is said in", got)
	}
}

// The border is repainted only on the frames the glow actually moves on,
// which is a small share of them.
//
// The breath is slow and the swing in it is small, so the alpha it works
// out lands on the same byte for several frames together. That is what
// pays for an animation that never stops: most frames have nothing to
// draw.
func TestTheBorderRepaintsOnlyWhenTheGlowMoves(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)

	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}
	// On a period boundary, which is the bottom of a breath.
	now := time.UnixMilli(0)

	moved := 0
	frames := int(glowEvery / frameAt60)
	a.drawShared(now)
	was := append([]render.Stroke(nil), rules(m)...)
	for i := 1; i <= frames; i++ {
		a.drawShared(now.Add(time.Duration(i) * frameAt60))
		if !sameRules(rules(m), was) {
			moved++
			was = append(was[:0], rules(m)...)
		}
	}

	if moved == 0 {
		t.Fatal("the border never moved over a whole breath")
	}
	if moved*2 > frames {
		t.Errorf("the border moved on %d of %d frames, want it still on most of them",
			moved, frames)
	}
}

// frameAt60 is one frame of a sixty-a-second screen, which is the rate
// an animation has to move at to look smooth.
const frameAt60 = 16 * time.Millisecond

// glowSamples is how many moments of a breath a test looks at, enough to
// see it rise and fall.
const glowSamples = 16

// glowRise is how long a border takes to go from the bottom of a breath
// to the top.
const glowRise = glowEvery / 2

// The glow is a tint that rises and falls. It never goes out and never
// reaches full, so the border is always there and never a flat block.
func TestTheGlowRisesAndFalls(t *testing.T) {
	var dim, bright uint8 = 0xff, 0
	const samples = 16
	steps := make([]float64, 0, samples)
	start := time.UnixMilli(0)
	for i := 0; i < samples; i++ {
		step := glowAt(start.Add(time.Duration(i) * glowRise / (samples / 2)))
		steps = append(steps, step)
		at := glow(color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}, step).A
		dim, bright = min(dim, at), max(bright, at)
	}
	up, down := false, false
	for i := 1; i < len(steps); i++ {
		switch {
		case steps[i] > steps[i-1]:
			up = true
		case steps[i] < steps[i-1]:
			down = true
		}
	}
	if !up || !down {
		t.Errorf("the glow went %v over a lap, want it to rise and fall", steps)
	}
	if got := glowAt(start.Add(glowEvery)); got != steps[0] {
		t.Errorf("a lap ended on %v and began on %v, so the glow jumps", got, steps[0])
	}
	if dim >= bright {
		t.Errorf("the glow ran from %#x to %#x, want it to rise and fall", dim, bright)
	}
	if dim == 0 {
		t.Error("the glow goes out altogether, and the border has to stay visible")
	}
	if bright == 0xff {
		t.Error("the glow reaches full, and the border is a tint over the pane")
	}
}

// A border keeps up with a pane that changes size, because a resized
// window gives every pane different room.
func TestABorderFollowsThePaneItIsRound(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}
	was := ruleBox(t, m)

	cellW, cellH := a.renderer.CellSize()
	a.resizeTo(cellW*60, cellH*18)
	frame(t, a)

	now := ruleBox(t, m)
	if now.Dx() >= was.Dx() || now.Dy() >= was.Dy() {
		t.Errorf("the rule is %v in a window shrunk from %v", now, was)
	}
	// Round the pane as it is now, not as it was.
	area, _ := a.paneArea(pane)
	wantsRuleOn(t, now, paneBox(t, a, area))
	if got := rules(m)[0].Colour; got.A == 0 {
		t.Error("the border lost its colour")
	}
}

// The border is drawn at the moment the frame began rather than the
// moment it reached the drawing, so it cannot land a step away from the
// row the same frame drew.
func TestTheBorderGlowsAtTheMomentTheFrameBegan(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}

	began := panelNow
	a.frameAt = began
	// The clock moves on between the update and the draw, the way it
	// does in a window that took a while over its frame.
	a.now = func() time.Time { return began.Add(glowRise / 2) }
	a.Draw(a.screen)

	want := a.sharedHues(a.sharedIn(pane), glowAt(began))[0]
	late := a.sharedHues(a.sharedIn(pane), glowAt(began.Add(glowRise/2)))[0]
	if want == late {
		t.Fatal("the glow did not move over the step, so this proves nothing")
	}
	if got := rules(m)[0].Colour; got != want {
		t.Errorf("the border is %v, want the %v the frame began on", got, want)
	}
}

// The glow reaches the border itself, not just the step count: the
// cells get brighter and dimmer as the lap goes round.
func TestTheBorderItselfBrightensAndDims(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}

	start := time.UnixMilli(0)
	var dim, bright uint8 = 0xff, 0
	for i := 0; i < glowSamples; i++ {
		a.drawShared(start.Add(time.Duration(i) * glowRise / (glowSamples / 2)))
		at := rules(m)[0].Colour.A
		dim, bright = min(dim, at), max(bright, at)
	}
	if dim >= bright {
		t.Errorf("the border's corner ran from %#x to %#x over a lap, and the border is meant to glow", dim, bright)
	}
	if got := rules(m)[0].Colour; !sameHue(got, statusAgentFG(a.colours)) {
		t.Errorf("the glow changed the colour to %v, and only the alpha is meant to move", got)
	}
}

// The border sits over the pane, on the pane's own pixels.
//
// In a window whose width is not a whole number of cells, so the odd
// pixels the window hands to its first and last columns are in play: a
// border that measured itself would land a fraction of a cell inside
// the pane's edge.
func TestTheBorderSitsOnThePanesPixels(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	cellW, cellH := a.renderer.CellSize()
	a.resizeTo(cellW*80+7, cellH*24+5)
	a.applyPads()
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}

	// On the pane's own pixels, padding and all, rather than a fraction
	// of a cell inside them.
	area, _ := a.paneArea(pane)
	wantsRuleOn(t, ruleBox(t, m), paneBox(t, a, area))
}

// The agent's border is the colour the menu bar's own chip says an
// agent in, so the two cannot drift apart.
func TestTheAgentBorderIsTheColourTheChipSays(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	handOver(t, a, onlyPaneWidget(t, a).(*term.Terminal))
	a.updateStatus()

	var chip *ui.Chip
	for i := range a.bar.Chips {
		if a.bar.Chips[i].Do != nil && strings.Contains(a.bar.Chips[i].Text, "agent") {
			chip = &a.bar.Chips[i]
		}
	}
	if chip == nil {
		t.Fatalf("the bar says %q, and an agent has a pane", chipsSay(a))
	}
	if got := a.borderColours().agent; got != chip.FG {
		t.Errorf("the border is %v and the %q chip is %v", got, chip.Text, chip.FG)
	}
}

// A watcher's border is the colour the bar says a window being driven
// in, not the quieter one it says a window that is only listening in.
func TestTheWatcherBorderIsNotTheListeningColour(t *testing.T) {
	a, _ := aSharedWindow(t)
	hues := a.borderColours()
	if hues.watched != statusTakenFG(a.colours) {
		t.Errorf("the watcher's border is %v, want the %v the bar says a window being driven in",
			hues.watched, statusTakenFG(a.colours))
	}
	if hues.watched == statusIdleFG(a.colours) {
		t.Error("the watcher's border is the colour the bar uses for nobody being connected")
	}
	if hues.watched == hues.agent {
		t.Error("both borders are the same colour, and a pane can have both")
	}
}

// A screen drawn on a layer of its own is opaque, so the border has to
// stay above it however late that layer arrives.
func TestTheBorderStaysAboveAScaledScreen(t *testing.T) {
	host, _, hostPane := aHeldScreen(t, 140, 44)
	frame(t, host)
	s := host.scaled[hostPane]
	if s == nil {
		t.Fatal("the screen is not drawn on a layer of its own")
	}
	m := host.shared[hostPane]
	if m == nil {
		t.Fatal("the pane a window is watching was given no border")
	}

	// The scaled layer put on the stack after the border, which is what
	// happens when a watcher takes the size of a pane that is shared
	// already.
	host.comp.Remove(s.layer)
	host.comp.Add(s.layer)
	if drawnAfter(host.comp, m.layer, s.layer) {
		t.Fatal("the border is still on top, so this proves nothing")
	}

	frame(t, host)

	if !drawnAfter(host.comp, m.layer, s.layer) {
		t.Error("the screen is drawn over the border, which hides it")
	}
}

// rowOf is the sidebar row for a pane, after a refresh at the given
// moment.
func rowOf(t *testing.T, a *testApp, pane *term.Terminal, now time.Time) ui.ListRow {
	t.Helper()
	a.refreshPanel(now)
	e := a.panes[pane]
	if e == nil {
		t.Fatal("the pane is not one of the window's")
	}
	row, ok := panelRow(a, any(e))
	if !ok {
		t.Fatalf("no row for the pane among the %d the sidebar drew", len(a.panel.Rows()))
	}
	return row
}

// The sidebar row carries the border's own colours, so the row and the
// pane say the same thing.
func TestTheRowCarriesTheSameMarkAsTheBorder(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	watchPane(t, pane)

	got := rowOf(t, a, pane, panelNow).Edge
	want := a.sharedEdge(pane, panelNow)
	if got != want {
		t.Fatalf("the row's stripe is %v and the pane's border %v", got, want)
	}
	if !sameHue(got[0], statusAgentFG(a.colours)) {
		t.Errorf("the first stripe is %v, want the cyan an agent is said in", got[0])
	}
	if !sameHue(got[1], statusTakenFG(a.colours)) {
		t.Errorf("the second stripe is %v, want the red a watcher is said in", got[1])
	}
}

// A pane nobody else is in has a plain row.
func TestTheRowOfAPaneNobodyElseIsInHasNoMark(t *testing.T) {
	a, pane := aSharedWindow(t)

	if got := rowOf(t, a, pane, panelNow).Edge; got != [2]color.RGBA{} {
		t.Errorf("the row of a pane the user has to themselves is striped %v", got)
	}
}

// The mark goes when the agent does.
func TestTheRowsMarkGoesWithTheAgent(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	if got := rowOf(t, a, pane, panelNow).Edge; got[0].A == 0 {
		t.Fatal("the row of a pane an agent has is not striped")
	}

	if !a.agents.forget(pane) {
		t.Fatal("the window did not take the pane back")
	}
	if got := rowOf(t, a, pane, panelNow).Edge; got != [2]color.RGBA{} {
		t.Errorf("the row is still striped %v with no agent in the pane", got)
	}
}

// The row's stripe glows in step with the border, off the same clock.
func TestTheRowsMarkGlowsWithTheBorder(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)

	start := time.UnixMilli(0)
	var dim, bright uint8 = 0xff, 0
	for i := 0; i < glowSamples; i++ {
		at := a.sharedEdge(pane, start.Add(time.Duration(i)*glowRise/(glowSamples/2)))[0].A
		dim, bright = min(dim, at), max(bright, at)
	}
	if dim >= bright {
		t.Errorf("the row's stripe ran from %#x to %#x over a lap, and it is meant to glow", dim, bright)
	}
}

// The row's stripe and the pane's border are the same colour at the
// same moment: one glow, drawn in two places.
func TestTheRowAndTheBorderGlowTogether(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)
	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}

	start := time.UnixMilli(0)
	var dim, bright uint8 = 0xff, 0
	for i := 0; i < glowSamples; i++ {
		now := start.Add(time.Duration(i) * glowRise / (glowSamples / 2))
		a.drawShared(now)
		border := rules(m)[0].Colour
		row := rowOf(t, a, pane, now).Edge[0]
		if border != row {
			t.Fatalf("at step %d the border is %v and the row %v", i, border, row)
		}
		dim, bright = min(dim, border.A), max(bright, border.A)
	}
	if dim >= bright {
		t.Errorf("the two agreed on one colour for a whole lap, %#x throughout", dim)
	}
}

// A pane only somebody else's window is reading gets its stripe in the
// first cell, the same place its one border ring is.
func TestAWatchedPaneStripesTheFirstCell(t *testing.T) {
	a, pane := aSharedWindow(t)
	watchPane(t, pane)

	got := rowOf(t, a, pane, panelNow).Edge
	if !sameHue(got[0], statusTakenFG(a.colours)) {
		t.Errorf("the first cell is %v, want the red a watcher is said in", got[0])
	}
	if got[1].A != 0 {
		t.Errorf("the second cell is %v, and only one window is reading the pane", got[1])
	}
}

// Two shared panes carry their own marks, not one another's.
func TestTwoSharedPanesCarryTheirOwnMarks(t *testing.T) {
	a, first := aSharedWindow(t)
	if err := a.openPane(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	second := newestPane(t, a)
	if second == first {
		t.Fatal("the window opened no second pane")
	}
	handOver(t, a, first)
	watchPane(t, second)

	one := rowOf(t, a, first, panelNow).Edge
	two := rowOf(t, a, second, panelNow).Edge
	if !sameHue(one[0], statusAgentFG(a.colours)) {
		t.Errorf("the pane an agent has is striped %v, want the agent's cyan", one[0])
	}
	if !sameHue(two[0], statusTakenFG(a.colours)) {
		t.Errorf("the pane a window is reading is striped %v, want the watcher's red", two[0])
	}
	if one[1].A != 0 || two[1].A != 0 {
		t.Errorf("a pane one thing has is striped twice: %v and %v", one, two)
	}
}

// A file browser's pane is on the sidebar too, and it has no terminal
// for anybody to share. Its row carries no stripe and keeps the cross
// that closes it.
func TestAFileBrowsersRowHasNoMarkAndKeepsItsCross(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	p := openFilesFromThePlus(t, a, conns.Local)
	waitFor(t, a, "the pane to land somewhere", func() bool { return p.At() != "" })

	e := a.files.rows[p]
	if e == nil {
		t.Fatal("the file pane has no row")
	}
	pointAtRow(t, a, e)
	a.refreshPanel(panelNow)
	row, ok := panelRow(a, any(e))
	if !ok {
		t.Fatal("the file pane's row is not on the sidebar")
	}
	if row.Edge != [2]color.RGBA{} {
		t.Errorf("the file browser's row is striped %v, and nothing is sharing it", row.Edge)
	}
	if row.HoverButton == 0 {
		t.Error("the file browser's row lost the cross that closes it")
	}
}

// A step of the glow costs one repaint and one rule, and that is what a
// shared pane costs for as long as the hand-over lasts.
func TestAGlowStepCostsOneRepaint(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	at := panelNow
	a.now = func() time.Time { return at }
	// Three, because the frame the border first appears on moves the
	// layers about and the one after it settles them.
	frame(t, a)
	frame(t, a)
	frame(t, a)

	// On to the next frame the glow actually moves on. The breath is
	// slow, so that is not the very next one.
	drew := false
	for i := 0; i < int(glowEvery/frameAt60); i++ {
		at = at.Add(frameAt60)
		frame(t, a)
		if !a.comp.Stats().Skipped {
			drew = true
			break
		}
	}
	if !drew {
		t.Fatal("the window drew nothing over a whole breath")
	}

	got := a.comp.Stats()
	// One: the sidebar row. The border itself is a rule drawn straight
	// to the screen, so a step of its glow repaints no grid at all.
	if got.Repainted != 1 {
		t.Errorf("a step of the glow repainted %d layers, want the sidebar row alone", got.Repainted)
	}
	if got.Strokes != 1 {
		t.Errorf("it drew %d rules, want the border's", got.Strokes)
	}
}

// rules are the border's rules, outermost first.
func rules(m *sharedMark) []render.Stroke { return m.layer.Strokes }

// sameRules reports whether two sets of rules would draw the same thing.
func sameRules(a, b []render.Stroke) bool {
	return slices.Equal(a, b)
}

// ruleBox is where the outermost rule runs, in the window's pixels.
func ruleBox(t *testing.T, m *sharedMark) image.Rectangle {
	t.Helper()
	got := rules(m)
	if len(got) == 0 {
		t.Fatal("the pane has no rule round it")
	}
	box := got[0].Rect.Add(image.Pt(m.layer.X, m.layer.Y))
	if box.Empty() {
		t.Fatalf("the rule round the pane is an empty box at %v", box)
	}
	return box
}

// wantsRuleOn checks that a rule runs round the pane on all four sides,
// no further in than half its own width. Rectangle.In is true for an
// empty box, so it cannot be the only check.
func wantsRuleOn(t *testing.T, rule, pane image.Rectangle) {
	t.Helper()
	if !rule.In(pane) {
		t.Errorf("the rule is at %v, which is outside the pane at %v", rule, pane)
		return
	}
	for _, side := range []struct {
		what  string
		slack int
	}{
		{"left", rule.Min.X - pane.Min.X},
		{"top", rule.Min.Y - pane.Min.Y},
		{"right", pane.Max.X - rule.Max.X},
		{"bottom", pane.Max.Y - rule.Max.Y},
	} {
		if side.slack > markWidth {
			t.Errorf("the rule stops %d pixels inside the pane's %s edge, want no more than %d",
				side.slack, side.what, markWidth)
		}
	}
}

// paneBox is the pixels a pane covers, which is what its border marks.
func paneBox(t *testing.T, a *testApp, area ui.Rect) image.Rectangle {
	t.Helper()
	a.renderer.Measure(a.g, &a.geo)
	left, width := a.geo.ColBox(area.X, area.X+area.Cols)
	top, height := a.geo.RowBox(area.Y, area.Y+area.Rows)
	return image.Rect(left, top, left+width, top+height)
}

// A pane too small for the second ring gets the outer one only.
//
// image.Rect turns an inside-out box the right way round rather than
// refusing it, so a ring that does not fit would come back as a small
// box in the middle of the pane and be drawn as a blob on the program's
// own text.
func TestATinyPaneGetsOnlyTheRingsItCanHold(t *testing.T) {
	// A cell small enough that the second ring, five pixels in from the
	// first, has nowhere to go. A real font is larger, so this is a
	// window zoomed right out.
	met := glyph.Metrics{CellW: 5, CellH: 5, Ascent: 4}
	var geo render.Geometry
	geo.Layout(grid.New(4, 4, color.RGBA{}, color.RGBA{}), met)

	m := newSharedMark()
	m.place(ui.Rect{Cols: 1, Rows: 1}, &geo, met)
	m.draw([2]color.RGBA{{A: 0xff}, {A: 0xff}})

	if got := len(rules(m)); got != 1 {
		t.Errorf("a one-cell pane has %d rules round it, want the outer one alone", got)
	}
	pane := image.Rect(0, 0, geo.Width(), geo.Height())
	for i, rule := range rules(m) {
		if rule.Rect.Empty() {
			t.Errorf("rule %d is an empty box at %v", i, rule.Rect)
			continue
		}
		if !rule.Rect.In(pane) {
			t.Errorf("rule %d is at %v and the pane at %v", i, rule.Rect, pane)
		}
	}
}

// The breath rises and falls without a corner anywhere, and comes back
// to where it started.
//
// No corner, because a border that snapped would read as a flash, which
// is the thing this animation is not allowed to be.
func TestTheGlowBreathesWithoutACorner(t *testing.T) {
	start := time.UnixMilli(0)

	// The biggest step between two frames is a small share of the whole
	// swing. A spike would show up here as one big step.
	was := glowAt(start)
	most := 0.0
	for at := frameAt60; at <= glowEvery; at += frameAt60 {
		now := glowAt(start.Add(at))
		most = max(most, math.Abs(now-was))
		was = now
	}
	if most > 0.05 {
		t.Errorf("the glow moved %.3f of its swing in one frame, want it smooth", most)
	}
	if got := glowAt(start.Add(glowEvery)); got != glowAt(start) {
		t.Errorf("a breath ended on %v and began on %v, so the glow jumps", got, glowAt(start))
	}
	// And it starts at the bottom, so nothing jumps when a border appears.
	if got := glowAt(start); got > 0.001 {
		t.Errorf("a breath begins at %v, want it to begin at rest", got)
	}
}

// The swing in the glow is small. A border that went from barely there
// to nearly solid once a second was a blink, and a blink beside the text
// somebody is reading is the sort of thing that gives people headaches.
func TestTheGlowIsGentle(t *testing.T) {
	var dim, bright uint8 = 0xff, 0
	start := time.UnixMilli(0)
	for at := time.Duration(0); at <= glowEvery; at += frameAt60 {
		a := glow(color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}, glowAt(start.Add(at))).A
		dim, bright = min(dim, a), max(bright, a)
	}

	if swing := int(bright) - int(dim); swing > 0x30 {
		t.Errorf("the glow swings %#x, from %#x to %#x, want a good deal less", swing, dim, bright)
	}
	if dim < 0x40 {
		t.Errorf("the glow falls to %#x, and the border has to stay plainly there", dim)
	}
}

// A window with a shared pane skips most of the frames of a breath.
//
// That is what pays for an animation that never stops. The breath is
// slow and the swing in it is small, so the colour it works out lands on
// the same byte for several frames together, and a cell that did not
// change is not drawn.
func TestASharedPaneSkipsMostFramesOfABreath(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	at := time.UnixMilli(0)
	a.now = func() time.Time { return at }
	// The border appearing moves the layers about, so the first frames
	// draw whatever the glow is doing.
	frame(t, a)
	frame(t, a)
	frame(t, a)

	frames := int(glowEvery / frameAt60)
	drew := 0
	for i := 0; i < frames; i++ {
		at = at.Add(frameAt60)
		frame(t, a)
		if !a.comp.Stats().Skipped {
			drew++
		}
	}

	if drew == 0 {
		t.Fatal("the window drew nothing over a whole breath, so the glow is not moving")
	}
	if drew*2 > frames {
		t.Errorf("the window drew %d of %d frames of a breath, want most of them skipped",
			drew, frames)
	}
}
