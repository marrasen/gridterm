package main

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
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
	cols, rows := m.g.Size()
	if cols != area.Cols || rows != area.Rows {
		t.Errorf("the border is %dx%d, want the pane's %dx%d", cols, rows, area.Cols, area.Rows)
	}
	if got := m.g.At(0, 0).BG; !sameHue(got, statusAgentFG(a.colours)) {
		t.Errorf("the corner of the border is %v, want the cyan an agent is said in", got)
	}
	if got := m.g.At(cols/2, rows/2).BG; got.A != 0 {
		t.Errorf("the middle of the pane is painted %v, and a border is the edge only", got)
	}
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
	if !m.layer.Transparent {
		t.Error("the border's layer is not transparent, so it hides what the program printed")
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
	if got := m.g.At(0, 0).BG; !sameHue(got, statusAgentFG(a.colours)) {
		t.Errorf("the outer border is %v, want the cyan an agent is said in", got)
	}
	if got := m.g.At(1, 1).BG; !sameHue(got, statusTakenFG(a.colours)) {
		t.Errorf("the inner border is %v, want the red a watcher is said in", got)
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
	if got := m.g.At(0, 0).BG; !sameHue(got, statusTakenFG(a.colours)) {
		t.Errorf("the border is %v, want the red a watcher is said in", got)
	}
	if got := m.g.At(1, 1).BG; got.A != 0 {
		t.Errorf("there is a second border at %v, and only one window is reading the pane", got)
	}
}

// The glow moves with the clock rather than with the frame, so a window
// drawing sixty frames a second repaints the border four times.
func TestTheBorderRepaintsOnlyWhenTheGlowMoves(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	frame(t, a)

	m := a.shared[pane]
	if m == nil {
		t.Fatal("no border")
	}
	// On a step boundary rather than time.Now(), or a run that started a
	// fraction before one would step the glow mid-test.
	now := time.UnixMilli(0)
	a.drawShared(now)
	m.g.ClearDirty()

	a.drawShared(now.Add(glowStep / 10))
	if m.g.AnyDirty() {
		t.Error("the border repainted within one step of the glow")
	}

	a.drawShared(now.Add(glowStep))
	if !m.g.AnyDirty() {
		t.Error("the border did not repaint when the glow moved on")
	}
}

// The glow is a tint that rises and falls. It never goes out and never
// reaches full, so the border is always there and never a flat block.
func TestTheGlowRisesAndFalls(t *testing.T) {
	var dim, bright uint8 = 0xff, 0
	steps := make([]int, 0, glowSteps*2)
	start := time.UnixMilli(0)
	for i := 0; i < glowSteps*2; i++ {
		step := glowAt(start.Add(time.Duration(i) * glowStep))
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
	if got := glowAt(start.Add(time.Duration(len(steps)) * glowStep)); got != steps[0] {
		t.Errorf("a lap ended on step %d and began on %d, so the glow jumps", got, steps[0])
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
	wasCols, wasRows := m.g.Size()

	cellW, cellH := a.renderer.CellSize()
	a.resizeTo(cellW*60, cellH*18)
	frame(t, a)

	cols, rows := m.g.Size()
	if cols >= wasCols || rows >= wasRows {
		t.Errorf("the border is %dx%d in a window shrunk from %dx%d", cols, rows, wasCols, wasRows)
	}
	area, _ := a.paneArea(pane)
	if cols != area.Cols || rows != area.Rows {
		t.Errorf("the border is %dx%d and the pane is %dx%d", cols, rows, area.Cols, area.Rows)
	}
	// The ring itself, not just the room for it: the cell on the new
	// right edge was inside the old border and had nothing drawn on it.
	if got := m.g.At(cols-1, rows/2).BG; got.A == 0 {
		t.Error("the border kept its old edges, so the ring is no longer round the pane")
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
	a.now = func() time.Time { return began.Add(glowStep) }
	a.Draw(a.screen)

	want := a.sharedHues(a.sharedIn(pane), glowAt(began))[0]
	late := a.sharedHues(a.sharedIn(pane), glowAt(began.Add(glowStep)))[0]
	if want == late {
		t.Fatal("the glow did not move over the step, so this proves nothing")
	}
	if got := m.g.At(0, 0).BG; got != want {
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
	for i := 0; i < glowSteps*2; i++ {
		a.drawShared(start.Add(time.Duration(i) * glowStep))
		at := m.g.At(0, 0).BG.A
		dim, bright = min(dim, at), max(bright, at)
	}
	if dim >= bright {
		t.Errorf("the border's corner ran from %#x to %#x over a lap, and the border is meant to glow", dim, bright)
	}
	if got := m.g.At(0, 0).BG; !sameHue(got, statusAgentFG(a.colours)) {
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

	area, _ := a.paneArea(pane)
	left, width := a.geo.ColBox(area.X, area.X+area.Cols)
	top, height := a.geo.RowBox(area.Y, area.Y+area.Rows)
	if m.layer.X != left || m.layer.Y != top {
		t.Errorf("the border is at %d,%d and the pane at %d,%d", m.layer.X, m.layer.Y, left, top)
	}
	if got := m.geo.Width(); got != width {
		t.Errorf("the border is %d pixels wide and the pane %d", got, width)
	}
	if got := m.geo.Height(); got != height {
		t.Errorf("the border is %d pixels tall and the pane %d", got, height)
	}
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
	for i := 0; i < glowSteps*2; i++ {
		at := a.sharedEdge(pane, start.Add(time.Duration(i)*glowStep))[0].A
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
	for i := 0; i < glowSteps*2; i++ {
		now := start.Add(time.Duration(i) * glowStep)
		a.drawShared(now)
		border := m.g.At(0, 0).BG
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

// A shared pane costs nothing between glow steps, and a repaint when
// the glow moves. The glow does not stop while the pane is shared, so
// what one step costs is what it costs for as long as the hand-over
// lasts.
func TestASharedPaneCostsNothingBetweenGlowSteps(t *testing.T) {
	a, pane := aSharedWindow(t)
	handOver(t, a, pane)
	at := panelNow
	a.now = func() time.Time { return at }
	// Three, because the frame the border first appears on moves the
	// layers about and the one after it settles them.
	frame(t, a)
	frame(t, a)

	frame(t, a)
	if got := a.comp.Stats(); !got.Skipped {
		t.Errorf("a second frame in the same step of the glow = %+v, want it skipped entirely", got)
	}

	at = at.Add(glowStep)
	frame(t, a)
	got := a.comp.Stats()
	if got.Skipped {
		t.Fatal("the glow moved on and the window drew nothing")
	}
	if got.Repainted != 2 {
		t.Errorf("a step of the glow repainted %d layers, want the border and the sidebar row", got.Repainted)
	}
}
