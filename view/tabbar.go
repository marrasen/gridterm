package view

import (
	"slices"
	"strconv"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"
)

// The tab bar, in the title bar after the menu button, while the window
// has two tabs or more. A click shows a tab; a middle click or its ×
// closes it; the + opens a terminal in a tab of its own. A tab dragged
// along the bar moves along it, onto another window's bar moves there,
// onto a pane joins it in a split, and let go outside every window
// opens a window of its own. The bar's empty room moves the window, as
// the rest of the title bar does.

// tabBar shows the window's tabs.
type tabBar struct {
	anim.Group
	w     *Window
	tabs  []app.Tab
	front int
	// titles are the tabs' titles, shaped, and more how many other
	// panes each tab holds, shaped too.
	titles []text.Run
	more   []text.Run
	// boxes are where the tabs are, plus where the + is, and end where
	// the tabs and the + end.
	boxes []geom.Rect
	plus  geom.Rect
	end   float32
	// hot is the tab under the pointer, and pressed the one the pointer
	// went down on, at pressAt, on its × with onCross; -1 for none.
	hot, pressed int
	pressAt      geom.Point
	pressedCross bool
	// hotAt is where the pointer was last seen over the bar, and
	// crossHot says it was on the hot tab's ×. plusPressed says the
	// pointer went down on the +.
	hotAt       geom.Point
	crossHot    bool
	plusPressed bool
	// shaped are the titles and the text size the runs were shaped
	// from, to shape them again only when those change.
	shaped     []string
	shapedSize float32
	// carried is the group dragged out of the bar, 0 for none, grabbed
	// at grab from its tab's top left corner.
	carried int
	grab    geom.Point
	// landing is where a tab dragged over the bar would land: before the
	// tab at that index, len(tabs) for last, and -1 while none is over.
	landing int
}

func newTabBar(w *Window) *tabBar { return &tabBar{w: w, hot: -1, pressed: -1, landing: -1} }

// The tabs' measures: the room each side of a title, the widest and
// narrowest tab, the room a tab's icon and × take, and the gap between
// tabs.
const (
	tabPad    = 10
	tabWidest = 220
	tabLeast  = 72
	tabIcon   = 16
	tabCross  = 16
	tabGap    = 2
	// tabPlus is the room the + takes, and captionLeast the room the
	// bar always leaves after it to move the window by.
	tabPlus      = 32
	captionLeast = 64
)

// shown reports whether the bar shows: with two tabs or more.
func (b *tabBar) shown() bool { return len(b.tabs) > 1 }

// show puts the window's tabs on the bar.
func (b *tabBar) show(tabs []app.Tab, focus string, u *gunim.UI) {
	front := 0
	for _, t := range tabs {
		// A group's panes share its arrangement.
		if g := b.w.groups[t.Pane]; g != nil && g == b.w.groups[focus] {
			front = t.Group
		}
	}
	titles := make([]string, 0, 2*len(tabs))
	for _, t := range tabs {
		more := ""
		if t.Panes > 1 {
			more = "+" + strconv.Itoa(t.Panes-1)
		}
		titles = append(titles, b.w.tabTitle(t), more)
	}
	size := smallText.Get(u.Theme()) + 1
	if slices.Equal(tabs, b.tabs) && front == b.front && slices.Equal(titles, b.shaped) && size == b.shapedSize {
		return
	}
	if len(tabs) != len(b.tabs) {
		// What the pointer was on may be another tab now.
		b.hot, b.crossHot = -1, false
		if b.carried == 0 {
			b.pressed = -1
		}
	}
	b.tabs, b.front = tabs, front
	if !slices.Equal(titles, b.shaped) || size != b.shapedSize {
		b.shaped, b.shapedSize = titles, size
		b.titles, b.more = b.titles[:0], b.more[:0]
		for i := 0; i < len(titles); i += 2 {
			b.titles = append(b.titles, text.Default().Shape(titles[i], size))
			b.more = append(b.more, text.Default().Shape(titles[i+1], size-1))
		}
	}
	if !b.shown() {
		// Hidden, nothing on it is pressed or lit; a tab carried away
		// still hears how its drag ends.
		b.hot, b.pressed, b.landing, b.crossHot, b.plusPressed = -1, -1, -1, false, false
	}
	u.Invalidate()
}

// tabTitle is what tab t says: the title of the pane in it that last
// had the keyboard.
func (w *Window) tabTitle(t app.Tab) string {
	for _, p := range w.panes {
		if p.ID == t.Pane {
			return p.Title
		}
	}
	return ""
}

// tabKind is the kind of the pane tab t shows, for its icon.
func (w *Window) tabKind(t app.Tab) string {
	if t.Panes > 1 {
		return "split"
	}
	return w.kindOf(t.Pane)
}

// Layout implements [gunim.Node]: the tabs side by side, as wide as
// their titles within bounds, narrower all alike when the room runs
// short, then the +.
func (b *tabBar) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	h := widget.MenubarHeight.Get(f.Theme)
	if c.Max.H > 0 {
		h = min(h, c.Max.H)
	}
	b.boxes = b.boxes[:0]
	b.plus, b.end = geom.Rect{}, 0
	if !b.shown() {
		return c.Constrain(geom.Sz(0, h))
	}
	want := make([]float32, len(b.tabs))
	total := float32(0)
	for i := range b.tabs {
		w := 2*tabPad + tabIcon + 6 + b.titles[i].Advance + tabCross + 4
		if b.more[i].Advance > 0 {
			w += b.more[i].Advance + 6
		}
		want[i] = min(max(w, tabLeast), tabWidest)
		total += want[i] + tabGap
	}
	room := c.Max.W - tabPlus - captionLeast
	if c.Max.W > 0 && total > room {
		// Short of room: each tab as wide as the room allows, alike,
		// leaving the + and room to move the window by.
		each := max(room/float32(len(b.tabs))-tabGap, 1)
		for i := range want {
			want[i] = min(want[i], each)
		}
	}
	x := float32(0)
	for i := range b.tabs {
		b.boxes = append(b.boxes, geom.Rc(x, 4, want[i], h-4))
		x += want[i] + tabGap
	}
	b.plus = geom.Rc(x+2, (h-24)/2, 24, 24)
	b.end = x + tabPlus
	return c.Constrain(geom.Sz(max(c.Max.W, b.end), h))
}

// Paint implements [gunim.Node].
func (b *tabBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if !b.shown() {
		return
	}
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(th)))
	ink, faint := widget.Ink.Get(th), look.Faint.Get(th)
	for i, t := range b.tabs {
		r := b.boxes[i]
		lifted := t.Group == b.carried
		radius := look.RowRadius.Get(th)
		switch {
		case t.Group == b.front:
			p.RRect(r, radius, paint.Solid(look.RowActive.Get(th)))
		case i == b.hot && b.carried == 0:
			p.RRect(r, radius, paint.Solid(look.RowHover.Get(th)))
		}
		c := faint
		if t.Group == b.front {
			c = ink
		}
		if lifted {
			c.A /= 3
		}
		mid := r.Min.Y + r.Size().H/2
		if r.Size().W < tabPad+tabIcon+6 {
			// Too narrow for its title: its icon alone, in the middle,
			// where it fits.
			if r.Size().W >= tabIcon+4 {
				paintIcon(p, b.w.tabKind(t), geom.Rc(r.Center().X-tabIcon/2, mid-tabIcon/2, tabIcon, tabIcon), c)
			}
			continue
		}
		paintIcon(p, b.w.tabKind(t), geom.Rc(r.Min.X+tabPad, mid-tabIcon/2, tabIcon, tabIcon), c)
		x := r.Min.X + tabPad + tabIcon + 6
		stop := r.Max.X - tabPad
		if b.crossShown(i) {
			stop -= tabCross
		}
		if m := b.more[i]; m.Advance > 0 {
			mc := faint
			mc.A = uint8(float32(mc.A) * 0.8)
			m.Paint(p, geom.Pt(stop-m.Advance, mid-m.Height()/2), mc)
			stop -= m.Advance + 6
		}
		if stop > x {
			run := b.titles[i]
			func() {
				defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(x, r.Min.Y, stop-x, r.Size().H), Opacity: 1, Clip: true})()
				run.Paint(p, geom.Pt(x, mid-run.Height()/2), c)
			}()
		}
		if b.crossShown(i) {
			cc := faint
			if i == b.hot && b.crossHot {
				cc = ink
			}
			paintCross(p, b.crossOf(i), cc)
		}
	}
	if b.landing >= 0 && len(b.boxes) > 0 && b.landing <= len(b.boxes) {
		x := b.boxes[len(b.boxes)-1].Max.X + tabGap/2
		if b.landing < len(b.boxes) {
			x = b.boxes[b.landing].Min.X - tabGap/2
		}
		p.RRect(geom.Rc(x-1.5, 6, 3, box.H-10), 1.5, paint.Solid(switcherRing.Get(th)))
	}
	drawIcon(p, icon.Plus, b.plus.Inset(geom.Uniform(4)), faint, 1.5)
}

// crossOf is where tab i's × is.
func (b *tabBar) crossOf(i int) geom.Rect {
	r := b.boxes[i]
	const s = 10
	return geom.Rc(r.Max.X-tabPad-s+2, r.Min.Y+(r.Size().H-s)/2, s, s)
}

// crossShown reports whether tab i shows its ×: the tab in front, and
// the one under the pointer, while no tab is dragged, where the tab is
// wide enough for it beside its icon.
func (b *tabBar) crossShown(i int) bool {
	if i < 0 || i >= len(b.tabs) || i >= len(b.boxes) || b.carried != 0 || b.boxes[i].Size().W < tabLeast {
		return false
	}
	return b.tabs[i].Group == b.front || i == b.hot
}

// onCross reports whether p is on tab i's ×, where it shows.
func (b *tabBar) onCross(i int, p geom.Point) bool {
	return b.crossShown(i) && b.crossOf(i).Inset(geom.Uniform(-4)).Contains(p)
}

// tabAt returns the tab at x, y, or -1.
func (b *tabBar) tabAt(p geom.Point) int {
	for i, r := range b.boxes {
		if r.Contains(p) {
			return i
		}
	}
	return -1
}

// CaptionRects implements [gunim.Caption]: the room after the tabs and
// the + moves the window.
func (b *tabBar) CaptionRects(size geom.Size) []geom.Rect {
	if b.end >= size.W {
		return nil
	}
	return []geom.Rect{geom.Rc(b.end, 0, size.W-b.end, size.H)}
}

// Handle implements [gunim.Handler].
//
// The ends of what began while the bar showed, a press let go and a
// drag ended, are heard while it is hidden too.
func (b *tabBar) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		if !b.shown() {
			return false
		}
		b.hotAt = e.Pos
		if b.pressed >= 0 && b.carried == 0 && !b.pressedCross && moved(e.Pos, b.pressAt) {
			b.lift(u)
			return true
		}
		// Over a tab, its × lights under the pointer.
		hot := b.tabAt(e.Pos)
		if hot != b.hot {
			b.hot = hot
			u.Invalidate()
		}
		if cross := b.onCross(hot, e.Pos); cross != b.crossHot {
			b.crossHot = cross
			u.Invalidate()
		}
		return hot >= 0
	case input.PointerLeave:
		if b.hot >= 0 {
			b.hot, b.crossHot = -1, false
			u.Invalidate()
		}
		return false
	case input.PointerDown:
		if !b.shown() {
			return false
		}
		i := b.tabAt(e.Pos)
		switch {
		case e.Button == input.ButtonMiddle && i >= 0:
			u.Send(b.w, app.CloseTab{Group: b.tabs[i].Group})
			return true
		case e.Button != input.ButtonPrimary:
			return false
		case i >= 0:
			b.pressed, b.pressAt = i, e.Pos
			b.pressedCross = b.onCross(i, e.Pos)
			return true
		case b.plus.Inset(geom.Uniform(-2)).Contains(e.Pos):
			b.plusPressed = true
			return true
		}
		return false
	case input.PointerUp:
		if b.plusPressed {
			b.plusPressed = false
			if b.shown() && b.plus.Inset(geom.Uniform(-2)).Contains(e.Pos) {
				u.Send(b.w, app.NewTerminal{})
			}
			return true
		}
		i := b.pressed
		b.pressed = -1
		if i < 0 || i >= len(b.tabs) || b.carried != 0 || b.tabAt(e.Pos) != i || !b.shown() {
			return i >= 0
		}
		if b.pressedCross {
			if b.onCross(i, e.Pos) {
				u.Send(b.w, app.CloseTab{Group: b.tabs[i].Group})
			}
			return true
		}
		u.Send(b.w, app.ShowTab{Group: b.tabs[i].Group})
		return true
	case input.DragEnd:
		b.dragEnded(e, u)
		return true
	case input.DragOver:
		d, ok := e.Data.(app.TabDrag)
		if !ok || !b.shown() {
			return false
		}
		b.landing = b.landingAt(e.Pos, d)
		u.AnswerDrag(movesHere)
		u.Invalidate()
		return true
	case input.DragLeave:
		b.landing = -1
		u.Invalidate()
		return false
	case input.Drop:
		d, ok := e.Data.(app.TabDrag)
		if !ok || !b.shown() {
			return false
		}
		at := b.landingAt(e.Pos, d)
		b.landing = -1
		before := 0
		if at < len(b.tabs) {
			before = b.tabs[at].Group
		}
		u.Send(b.w, app.MoveTab{Group: d.Group, Before: before})
		u.Invalidate()
		return true
	}
	return false
}

// landingAt is where tab d, dragged to p, would land: before the first
// tab whose middle is past p.
func (b *tabBar) landingAt(p geom.Point, d app.TabDrag) int {
	for i, r := range b.boxes {
		if p.X < r.Min.X+r.Size().W/2 {
			return i
		}
	}
	return len(b.boxes)
}

// lift picks the tab pressed up, to follow the pointer.
func (b *tabBar) lift(u *gunim.UI) {
	i := b.pressed
	t := b.tabs[i]
	r := b.boxes[i]
	b.carried = t.Group
	b.grab = b.pressAt.Sub(r.Min)
	g := &tabGhost{title: b.titles[i], kind: b.w.tabKind(t), size: r.Size(), lit: anim.NewFloat(0)}
	g.Add(g.lit)
	u.StartDrag(b, app.TabDrag{Group: t.Group, Window: b.w.winID}, g, b.grab)
	u.Invalidate()
}

// dragEnded hears how the drag of a tab ended: taken, which the next
// state shows; let go outside every window, which opens one for it; or
// neither, and the tab stays where it was.
func (b *tabBar) dragEnded(e input.DragEnd, u *gunim.UI) {
	g := b.carried
	b.carried, b.pressed, b.landing = 0, -1, -1
	if g == 0 {
		return
	}
	if e.Out && !e.Taken {
		// The tab's top left corner where the image's was, and the
		// window as large as this one.
		u.Send(b.w, app.TabToNewWindow{Group: g, At: e.At.Sub(b.grab), Size: b.w.size})
	}
	u.Invalidate()
}

// tabGhost is the image of a tab that follows the pointer while it is
// dragged, ringed while it is over something that takes it.
type tabGhost struct {
	anim.Group
	title text.Run
	kind  string
	size  geom.Size
	lit   *anim.Float
}

// Layout implements [gunim.Node].
func (g *tabGhost) Layout(gunim.Constraints, gunim.Frame, gunim.Children) geom.Size { return g.size }

// Paint implements [gunim.Node].
func (g *tabGhost) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	r := geom.Rect{Max: box.Point()}
	p.RRect(r, 6, paint.Solid(look.SidebarFill.Get(th)))
	p.RRect(r, 6, paint.Solid(look.RowActive.Get(th)))
	if on := min(max(g.lit.Value(), 0), 1); on > 0.01 {
		c := switcherRing.Get(th)
		c.A = uint8(float32(c.A) * on)
		p.RRectStroke(r.Inset(geom.Uniform(1)), 6, paint.Fill{}, paint.Stroke{Width: 2, Color: c})
	}
	ink := widget.Ink.Get(th)
	mid := box.H / 2
	paintIcon(p, g.kind, geom.Rc(tabPad, mid-tabIcon/2, tabIcon, tabIcon), ink)
	x := float32(tabPad + tabIcon + 6)
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(x, 0, box.W-x-tabPad, box.H), Opacity: 1, Clip: true})()
	g.title.Paint(p, geom.Pt(x, mid-g.title.Height()/2), ink)
}

// Handle implements [gunim.Handler]: the node under the pointer says
// whether it takes the tab.
func (g *tabGhost) Handle(e input.Event, u *gunim.UI) bool {
	if a, ok := e.(input.DragAnswer); ok {
		to := float32(0)
		if a.Answer != nil {
			to = 1
		}
		g.lit.Animate(to, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	}
	return false
}

// showTabs shows the window's tabs, and gives the bar the title bar's
// room while it shows: the menu button keeps its own, and "kakel" makes
// way.
func (w *Window) showTabs(st app.State, u *gunim.UI) {
	was := w.tabs.shown()
	w.tabs.show(st.Tabs, st.Focus, u)
	if w.tabs.shown() {
		// As wide as the menu button, in the theme on now.
		w.barBox.Width = widget.MenubarHeight.Get(u.Theme()) + 12
	}
	if w.tabs.shown() == was {
		return
	}
	if w.tabs.shown() {
		w.bar.Title = ""
		w.titleRow.Grow(w.barBox, 0).Grow(w.tabs, 1)
	} else {
		w.barBox.Width = 0
		w.bar.Title = app.ProgramName
		w.titleRow.Grow(w.tabs, 0).Grow(w.barBox, 1)
	}
	u.Invalidate()
}

// tabDock is where a tab dragged over the stage would go: beside pane,
// on the side it is nearest, which is lit at lit. A zero one is
// nowhere.
type tabDock struct {
	pane            string
	vertical, first bool
	lit             geom.Rect
}

// docksHere is what a pane answers a tab dragged over it: a drop there
// joins it in a split.
const docksHere = "docks here"

// tabDrop takes a tab dragged over the window: onto a pane, where it
// joins it in a split on the side it is nearest, or from another
// window onto anywhere else, where it becomes a tab of this one.
func (w *Window) tabDrop(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.DragOver:
		d, ok := e.Data.(app.TabDrag)
		if !ok {
			return false
		}
		w.dock = w.dockAt(e.Pos, d, u)
		switch {
		case w.dock.pane != "":
			u.AnswerDrag(docksHere)
			w.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
		case d.Window != w.winID:
			u.AnswerDrag(movesHere)
			w.dropLit.Animate(1, widget.Quick.Get(u.Theme()))
		default:
			u.Invalidate()
			return false
		}
		u.Invalidate()
		return true
	case input.DragLeave:
		if w.dock.pane != "" {
			w.dock = tabDock{}
			u.Invalidate()
		}
		return false
	case input.Drop:
		d, ok := e.Data.(app.TabDrag)
		if !ok {
			return false
		}
		dock := w.dockAt(e.Pos, d, u)
		w.dock = tabDock{}
		w.dropLit.Animate(0, widget.Settle.Get(u.Theme()))
		u.Invalidate()
		switch {
		case dock.pane != "":
			u.Send(w, app.DockTab{Group: d.Group, Beside: dock.pane, Vertical: dock.vertical, First: dock.first})
		case d.Window != w.winID:
			u.Send(w, app.MoveTab{Group: d.Group})
		default:
			return false
		}
		return true
	}
	return false
}

// dockAt is where tab d, dragged to p, would join a split: beside the
// pane on stage under p, on the side of it p is nearest. Not beside a
// pane of the tab itself.
func (w *Window) dockAt(p geom.Point, d app.TabDrag, u *gunim.UI) tabDock {
	own := ""
	if d.Window == w.winID {
		for _, t := range w.tabs.tabs {
			if t.Group == d.Group {
				own = t.Pane
			}
		}
		if own == "" {
			// A tab of this window not on the bar: its only one.
			return tabDock{}
		}
	}
	for _, id := range boxLeaves(w.stageBox, nil) {
		r, ok := u.Bounds(w.paneNode(id))
		if !ok || !r.Contains(p) {
			continue
		}
		if own != "" && w.groups[own] == w.groups[id] {
			return tabDock{}
		}
		s := r.Size()
		if s.W <= 0 || s.H <= 0 {
			return tabDock{}
		}
		fx, fy := (p.X-r.Min.X)/s.W, (p.Y-r.Min.Y)/s.H
		dk := tabDock{pane: id}
		switch min(fx, 1-fx, fy, 1-fy) {
		case fx:
			dk.first, dk.lit = true, geom.Rc(r.Min.X, r.Min.Y, s.W/2, s.H)
		case 1 - fx:
			dk.lit = geom.Rc(r.Min.X+s.W/2, r.Min.Y, s.W/2, s.H)
		case fy:
			dk.vertical, dk.first, dk.lit = true, true, geom.Rc(r.Min.X, r.Min.Y, s.W, s.H/2)
		default:
			dk.vertical, dk.lit = true, geom.Rc(r.Min.X, r.Min.Y+s.H/2, s.W, s.H/2)
		}
		return dk
	}
	return tabDock{}
}

// paintDock lights the half of a pane a tab dragged over it would take.
func (w *Window) paintDock(p *paint.Painter, f gunim.Frame) {
	if w.dock.pane == "" {
		return
	}
	c := switcherRing.Get(f.Theme)
	tint := c
	tint.A = 0x33
	r := w.dock.lit.Inset(geom.Uniform(4))
	p.RRect(r, 6, paint.Solid(tint))
	p.RRectStroke(r, 6, paint.Fill{}, paint.Stroke{Width: 2, Color: c})
}

// Access implements [gunim.Accessible]: a tab list with a tab for each
// of the window's tabs, named as its title.
func (b *tabBar) Access() access.Info {
	info := access.Info{Role: access.RoleTabList}
	if !b.shown() {
		return info
	}
	for i, t := range b.tabs {
		name := b.w.tabTitle(t)
		if t.Panes > 1 {
			name += ", " + strconv.Itoa(t.Panes) + " panes"
		}
		part := access.Info{Role: access.RoleTab, Name: name, Actions: []string{access.ActionPress}}
		if t.Group == b.front {
			part.State = access.StateSelected
			info.Active = i + 1
		}
		if i < len(b.boxes) {
			part.Bounds = b.boxes[i]
		}
		info.Parts = append(info.Parts, part)
	}
	return info
}

// AccessAct implements [gunim.AccessActor]: pressing a tab shows it.
func (b *tabBar) AccessAct(r access.Request, u *gunim.UI) bool {
	if r.Action != access.ActionPress || r.Part < 0 || r.Part >= len(b.tabs) {
		return false
	}
	u.Send(b.w, app.ShowTab{Group: b.tabs[r.Part].Group})
	return true
}
