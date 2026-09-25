package main

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// jobsPane shows the file jobs as cards, newest at the bottom: each
// with its progress, what it is on, and a button to cancel it. The
// finished ones stay, saying how they ended, until they are cleared.
type jobsPane struct {
	head  *widget.Label
	clear *widget.Button
	empty *widget.Label
	list  *widget.List
	body  *widget.Scroll
	// none says there are no jobs, and the empty line shows.
	none bool
}

func newJobsPane() *jobsPane {
	p := &jobsPane{
		head:  widget.NewLabel("Jobs"),
		clear: widget.NewButton("Clear Finished"),
		empty: widget.NewLabel("Copies, moves and deletes show here as they run."),
		list:  widget.NewList(),
		none:  true,
	}
	p.head.Size = widget.DialogTitleSize
	p.empty.Color = faint
	p.clear.On = ClearJobs{}
	p.body = widget.NewScroll(widget.NewPad(p.list))
	return p
}

// show brings the cards up to date with jobs.
func (p *jobsPane) show(jobs []Job, u *gunim.UI) {
	widget.Sync(p.list, u, jobs,
		func(j Job) widget.Key { return widget.Key(j.ID) },
		func(j Job) *jobCard { return newJobCard(j) },
		func(c *jobCard, j Job, u *gunim.UI) { c.show(j, u) })
	p.none = len(jobs) == 0
}

// Children implements [gunim.Composite].
func (p *jobsPane) Children() []gunim.Node {
	return []gunim.Node{p.head, p.clear, p.empty, p.body}
}

// Layout implements [gunim.Node]: the heading and Clear Finished along
// the top, the cards under them.
func (p *jobsPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, top = 20, 56
	head, clear, empty, body := kids.At(0), kids.At(1), kids.At(2), kids.At(3)
	cs := clear.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, top)})
	clear.Place(geom.Pt(c.Max.W-pad-cs.W, (top-cs.H)/2))
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-3*pad-cs.W), top)})
	head.Place(geom.Pt(pad, (top-hs.H)/2))
	empty.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-2*pad), top)})
	empty.Place(geom.Pt(pad, top+8))
	body.Layout(gunim.Tight(geom.Sz(c.Max.W, max(0, c.Max.H-top))))
	body.Place(geom.Pt(0, top))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *jobsPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(pt)
	kids.At(1).Paint(pt)
	if p.none {
		kids.At(2).Paint(pt)
	}
	kids.At(3).Paint(pt)
}

// jobCard is one job: its title and Cancel, its progress, and what it
// is on or how it ended.
type jobCard struct {
	title  *widget.Label
	detail *widget.Label
	bar    *widget.ProgressBar
	cancel *widget.Button
	// running is set while Cancel shows.
	running bool
}

func newJobCard(j Job) *jobCard {
	c := &jobCard{title: widget.NewLabel(""), detail: widget.NewLabel(""), bar: widget.NewProgressBar(), cancel: widget.NewButton("Cancel")}
	c.title.MaxLines, c.detail.MaxLines = 1, 1
	c.detail.Size, c.detail.Color = smallText, faint
	c.running = !j.Done
	c.cancel.On = CancelJob{ID: j.ID}
	c.title.SetText(j.Title)
	c.detail.SetText(j.Detail)
	return c
}

// show brings the card up to date with j.
func (c *jobCard) show(j Job, u *gunim.UI) {
	c.title.SetText(j.Title)
	c.detail.SetText(j.Detail)
	c.bar.Indeterminate = j.Share < 0 && !j.Done
	if j.Share >= 0 {
		c.bar.Set(j.Share, u)
	}
	if j.Failed {
		c.detail.Color = widget.DialogProblem
	}
	if j.Done && c.running {
		c.running = false
		u.Remove(c.cancel)
	}
}

// Children implements [gunim.Composite].
func (c *jobCard) Children() []gunim.Node {
	out := []gunim.Node{c.title, c.detail, c.bar}
	if c.running {
		out = append(out, c.cancel)
	}
	return out
}

// Layout implements [gunim.Node].
func (c *jobCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, gap = 16, 10
	w := cs.Max.W
	right := w - pad
	var bs geom.Size
	if kids.Len() > 3 {
		b := kids.At(3)
		bs = b.Layout(gunim.Constraints{Max: geom.Sz(w, 40)})
		right -= bs.W + gap
	}
	title, detail, bar := kids.At(0), kids.At(1), kids.At(2)
	ts := title.Layout(gunim.Constraints{Max: geom.Sz(max(0, right-pad), 40)})
	line := max(ts.H, bs.H)
	title.Place(geom.Pt(pad, pad+(line-ts.H)/2))
	if kids.Len() > 3 {
		kids.At(3).Place(geom.Pt(w-pad-bs.W, pad+(line-bs.H)/2))
	}
	y := pad + line + gap
	barSize := bar.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 20)})
	bar.Place(geom.Pt(pad, y))
	y += barSize.H + gap
	ds := detail.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 40)})
	detail.Place(geom.Pt(pad, y))
	return cs.Constrain(geom.Sz(w, y+ds.H+pad))
}

// Paint implements [gunim.Node].
func (c *jobCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, widget.CardRadius.Get(f.Theme), paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}
