package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
)

// The window's side of tunnels: the dialog that opens one, and the
// pane that tells of one.

// tunnelDialog asks for a tunnel over the focused pane's server: a
// forwarded port, or with socks a SOCKS proxy.
func (w *window) tunnelDialog(socks bool, u *gunim.UI) {
	machine := w.machineOf(w.focused)
	if machine == "" {
		w.toasts.Show(widget.Toast{Title: "Tunnels run over a server's connection",
			Body: "Open one from a pane on a server."}, u)
		return
	}
	listen, target := widget.NewTextField(), widget.NewTextField()
	listen.Placeholder, target.Placeholder = "[address:]port", "host:port"
	way := widget.NewDropdown("Local — listen here", "Remote — listen on "+machine)
	way.Label = "Direction"
	keep := widget.NewCheckbox("Save this tunnel")
	form := widget.NewForm()
	title := "Tunnel via " + machine
	if socks {
		title = "SOCKS proxy via " + machine
		listen.SetText("1080")
		note := widget.NewLabel("A SOCKS port here. Connections go out from " + machine + ".")
		form.Add("", note).Add("Listen on", listen)
	} else {
		form.Add("Listen on", listen).Add("Forward to", target).Add("Direction", way)
	}
	form.Add("", keep)
	tunnel := func() remote.Tunnel {
		t := remote.Tunnel{Kind: remote.LocalForward, Listen: strings.TrimSpace(listen.Text()), Target: strings.TrimSpace(target.Text())}
		// A port alone is that port on the listening machine only.
		if _, err := strconv.Atoi(t.Listen); err == nil {
			t.Listen = ":" + t.Listen
		}
		switch {
		case socks:
			t.Kind, t.Target = remote.DynamicForward, ""
		case way.Selected == 1:
			t.Kind = remote.RemoteForward
		}
		return t
	}
	d := widget.NewDialog(title)
	d.Body = form
	d.SetButtons("Open", "Cancel")
	d.Check = func() string {
		if err := tunnel().Validate(); err != nil {
			return upperFirst(err.Error()) + "."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return OpenTunnel{Machine: machine, Tunnel: tunnel(), Keep: keep.On} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// machineOf returns the server pane id is on, "" for this computer.
func (w *window) machineOf(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			return p.Machine
		}
	}
	return ""
}

// savedTunnelItems are the palette's lines for the saved tunnels, each
// over the server it was kept for, by that server's name now.
func (w *window) savedTunnelItems() []widget.PaletteItem {
	var out []widget.PaletteItem
	for _, s := range w.savedTunnels {
		via := s.Host
		for _, h := range w.saved {
			if s.HostID != "" && h.ID == s.HostID {
				via = h.Name
			}
		}
		what := s.Kind + " tunnel"
		if t, err := asTunnel(s); err == nil {
			what = "Tunnel " + t.String()
		}
		out = append(out, widget.PaletteItem{Title: "Open " + what + " via " + via})
	}
	return out
}

// setSavedTunnels takes the saved tunnels, and puts them in the
// palette when they changed.
func (w *window) setSavedTunnels(saved []settings.SavedTunnel) {
	if slices.Equal(saved, w.savedTunnels) {
		return
	}
	w.savedTunnels = saved
	w.servers(w.saved)
}

// runSavedTunnel opens saved tunnel at, as the palette lists them.
func (w *window) runSavedTunnel(at string, u *gunim.UI) {
	if i, err := strconv.Atoi(at); err == nil && i >= 0 && i < len(w.savedTunnels) {
		u.Send(w, OpenSavedTunnel{Saved: w.savedTunnels[i]})
	}
}

// tunnelPane tells of a tunnel: its account in a terminal, over a bar
// saying what it is doing, with its controls.
type tunnelPane struct {
	col *widget.Flex
	bar *tunnelBar
}

func newTunnelPane(t *term) *tunnelPane {
	bar := newTunnelBar()
	col := widget.Column(t, bar).Grow(t, 1)
	col.Cross, col.Gap = widget.CrossStretch, noGap
	return &tunnelPane{col: col, bar: bar}
}

// Children implements [gunim.Composite].
func (p *tunnelPane) Children() []gunim.Node { return []gunim.Node{p.col} }

// Layout implements [gunim.Node].
func (p *tunnelPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *tunnelPane) Paint(pt *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(pt)
}

// tunnelBar says what a tunnel is doing, beside the buttons that watch
// its traffic and close it. A stopped tunnel's bar offers to clear
// its row; a closed one's says so.
type tunnelBar struct {
	label        *widget.Label
	watch, close *widget.Button
	// shown are the buttons in the bar now.
	shown []gunim.Node
}

func newTunnelBar() *tunnelBar {
	b := &tunnelBar{label: widget.NewLabel(""), watch: widget.NewButton(""), close: widget.NewButton("")}
	b.label.Size, b.label.Color, b.label.MaxLines = smallText, faint, 1
	return b
}

// show brings the bar up to date with t, which ok says still has a row.
func (b *tunnelBar) show(t Tunnel, ok bool, u *gunim.UI) {
	var want []gunim.Node
	switch {
	case ok && t.Live:
		b.label.SetText(t.Label + " · " + t.Note)
		b.watch.Label, b.watch.On = "Watch the Traffic", WatchTunnel{ID: t.ID, On: true}
		if t.Watching {
			b.watch.Label, b.watch.On = "Stop Watching", WatchTunnel{ID: t.ID}
		}
		b.close.Label, b.close.On = "Close Tunnel", CloseTunnel{ID: t.ID}
		want = []gunim.Node{b.watch, b.close}
	case ok:
		b.label.SetText(t.Label + " · stopped")
		b.close.Label, b.close.On = "Clear", CloseTunnel{ID: t.ID}
		want = []gunim.Node{b.close}
	default:
		b.label.SetText("This tunnel has closed.")
	}
	for _, n := range b.shown {
		if !slices.Contains(want, n) {
			u.Remove(n)
		}
	}
	for _, n := range want {
		if !slices.Contains(b.shown, n) {
			u.Insert(b, n)
		}
	}
	b.shown = want
}

// Children implements [gunim.Composite].
func (b *tunnelBar) Children() []gunim.Node { return append([]gunim.Node{b.label}, b.shown...) }

// Layout implements [gunim.Node]: the label at the start, the buttons
// at the end.
func (b *tunnelBar) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const padX, gap, height = 12, 8, 44
	x := c.Max.W - padX
	for i := kids.Len() - 1; i >= 1; i-- {
		k := kids.At(i)
		s := k.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, height)})
		x -= s.W
		k.Place(geom.Pt(x, (height-s.H)/2))
		x -= gap
	}
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, x-padX), height)})
	k.Place(geom.Pt(padX, (height-s.H)/2))
	return c.Constrain(geom.Sz(c.Max.W, height))
}

// Paint implements [gunim.Node].
func (b *tunnelBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenuFill.Get(f.Theme)))
	p.RRect(geom.Rect{Max: geom.Pt(box.W, 1)}, 0, paint.Solid(widget.MenuBorder.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}
