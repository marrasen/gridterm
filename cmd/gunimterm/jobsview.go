package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/settings"
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

// jobCard is one job: its title and what can be done with it, its
// progress, a graph of its speed, the names it works on with the ones
// done ticked, and what it is doing or how it ended. Cancel shows while
// it runs; a finished copy offers Repeat and Save this copy.
type jobCard struct {
	title  *widget.Label
	detail *widget.Label
	names  *widget.Label
	bar    *widget.ProgressBar
	graph  *widget.LiveGraph
	// sampled is how many of the job's speed samples the graph has.
	sampled int
	cancel *widget.Button
	repeat *widget.Button
	save   *widget.Checkbox
	// acts are the controls showing now, along the title's row.
	acts []gunim.Node
}

func newJobCard(j Job) *jobCard {
	c := &jobCard{
		title: widget.NewLabel(""), detail: widget.NewLabel(""), names: widget.NewLabel(""),
		bar: widget.NewProgressBar(), graph: widget.NewLiveGraph(sampleEvery, mostSpeeds),
		cancel: widget.NewButton("Cancel"), repeat: widget.NewButton("Repeat"), save: widget.NewCheckbox("Save this copy"),
	}
	c.graph.Label = func(v float64) string { return humanSize(int64(v)) + "/s" }
	c.title.MaxLines, c.detail.MaxLines = 1, 1
	c.detail.Size, c.detail.Color = smallText, faint
	c.names.Size = smallText
	c.cancel.On, c.repeat.On = CancelJob{ID: j.ID}, RepeatJob{ID: j.ID}
	id := j.ID
	c.save.OnChange = func(on bool) gunim.Intent { return SaveCopy{ID: id, On: on} }
	c.acts = c.want(j)
	c.fill(j)
	return c
}

// want are the controls a job's row offers.
func (c *jobCard) want(j Job) []gunim.Node {
	switch {
	case !j.Done:
		return []gunim.Node{c.cancel}
	case j.Repeatable:
		return []gunim.Node{c.save, c.repeat}
	}
	return nil
}

// fill says j on the card.
func (c *jobCard) fill(j Job) {
	c.title.SetText(j.Title)
	c.detail.SetText(j.Detail)
	c.detail.Color = faint
	if j.Failed {
		c.detail.Color = widget.DialogProblem
	}
	// The samples taken since the card last looked, into the graph,
	// which slides on every frame while the job runs.
	fresh := min(j.Sampled-c.sampled, len(j.Speeds))
	for _, s := range j.Speeds[len(j.Speeds)-max(fresh, 0):] {
		c.graph.Add(float64(s))
	}
	c.sampled = j.Sampled
	c.graph.SetRunning(!j.Done)
	c.save.On = j.Saved
	c.names.SetText(namesLines(j))
}

// namesLines lists what a job works on, the one it is on marked and
// the ones done ticked, and at most five of them.
func namesLines(j Job) string {
	if len(j.Names) < 2 && j.Current == "" {
		return ""
	}
	const most = 5
	var out []string
	for i, n := range j.Names {
		if i == most-1 && len(j.Names) > most {
			out = append(out, fmt.Sprintf("   and %d more", len(j.Names)-i))
			break
		}
		mark := "   "
		switch {
		case n == j.Current:
			mark = "›  "
		case j.Done && !j.Failed, i < j.Ticked:
			mark = "✓  "
		}
		out = append(out, mark+n)
	}
	return strings.Join(out, "\n")
}

// show brings the card up to date with j.
func (c *jobCard) show(j Job, u *gunim.UI) {
	c.bar.Indeterminate = j.Share < 0 && !j.Done
	if j.Share >= 0 {
		c.bar.Set(j.Share, u)
	}
	c.fill(j)
	want := c.want(j)
	for _, n := range c.acts {
		if !slices.Contains(want, n) {
			u.Remove(n)
		}
	}
	for _, n := range want {
		if !slices.Contains(c.acts, n) {
			u.Insert(c, n)
		}
	}
	c.acts = want
}

// Children implements [gunim.Composite].
func (c *jobCard) Children() []gunim.Node {
	return append([]gunim.Node{c.title, c.detail, c.bar, c.graph, c.names}, c.acts...)
}

// Layout implements [gunim.Node].
func (c *jobCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, gap = 16, 10
	w := cs.Max.W
	right := w - pad
	line := float32(0)
	byNode := map[gunim.Node]gunim.Child{}
	for i := 5; i < kids.Len(); i++ {
		byNode[kids.At(i).Node()] = kids.At(i)
	}
	sizes := map[gunim.Node]geom.Size{}
	for _, n := range c.acts {
		if k, ok := byNode[n]; ok {
			sizes[n] = k.Layout(gunim.Constraints{Max: geom.Sz(w, 40)})
			line = max(line, sizes[n].H)
		}
	}
	for i := len(c.acts) - 1; i >= 0; i-- {
		n := c.acts[i]
		k, ok := byNode[n]
		if !ok {
			continue
		}
		right -= sizes[n].W
		k.Place(geom.Pt(right, pad+(line-sizes[n].H)/2))
		right -= gap
	}
	title, detail, bar, graph, names := kids.At(0), kids.At(1), kids.At(2), kids.At(3), kids.At(4)
	ts := title.Layout(gunim.Constraints{Max: geom.Sz(max(0, right-pad), 40)})
	line = max(line, ts.H)
	title.Place(geom.Pt(pad, pad+(line-ts.H)/2))
	y := pad + line + gap
	bs := bar.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 20)})
	bar.Place(geom.Pt(pad, y))
	y += bs.H + gap/2
	gs := graph.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 80)})
	graph.Place(geom.Pt(pad, y))
	if gs.H > 0 {
		y += gs.H + gap/2
	}
	ds := detail.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 40)})
	detail.Place(geom.Pt(pad, y))
	y += ds.H
	ns := names.Layout(gunim.Constraints{Max: geom.Sz(max(0, w-2*pad), 400)})
	if c.names.Text != "" && ns.H > 0 {
		y += gap
		names.Place(geom.Pt(pad, y))
		y += ns.H
	}
	return cs.Constrain(geom.Sz(w, y+pad))
}

// Paint implements [gunim.Node].
func (c *jobCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, widget.CardRadius.Get(f.Theme), paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

// copiesPane lists the copies kept, to do again: Enter does the one
// the cursor is on, and Delete forgets it.
type copiesPane struct {
	w     *window
	bar   *buttonBar
	table *widget.Table
	col   *widget.Flex
	kept  map[widget.Key]settings.SavedCopy
}

func newCopiesPane(w *window) *copiesPane {
	p := &copiesPane{w: w, bar: newButtonBar(), kept: map[widget.Key]settings.SavedCopy{}}
	p.table = widget.NewTable(widget.TableColumn{Title: "What"}, widget.TableColumn{Title: "From → To", Width: 380})
	p.table.Row = func(k widget.Key) widget.TableRow {
		c := p.kept[k]
		return widget.TableRow{Cells: []string{copiedWhat(c), copiedWhere(c)}}
	}
	p.table.OnActivate = func(k widget.Key, u *gunim.UI) { u.Send(p.table, RunSavedCopy{Saved: p.kept[k]}) }
	p.col = widget.Column(p.table, p.bar).Grow(p.table, 1)
	p.col.Cross, p.col.Gap = widget.CrossStretch, noGap
	return p
}

// show brings the list up to date.
func (p *copiesPane) show(saved []settings.SavedCopy, u *gunim.UI) {
	clear(p.kept)
	keys := make([]widget.Key, 0, len(saved))
	for i, c := range saved {
		k := widget.Key(fmt.Sprintf("%03d", i))
		p.kept[k] = c
		keys = append(keys, k)
	}
	p.table.SetKeys(keys, u)
	if len(saved) == 0 {
		p.bar.set("None saved. A finished copy has a Save this copy box.", u)
		return
	}
	p.bar.set(count(len(saved), "copy")+" saved · Enter does the one selected again, Delete forgets it", u)
}

// Children implements [gunim.Composite].
func (p *copiesPane) Children() []gunim.Node { return []gunim.Node{p.col} }

// Layout implements [gunim.Node].
func (p *copiesPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *copiesPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(pt)
}

// Handle implements [gunim.Handler]: Delete forgets the copy the
// cursor is on.
func (p *copiesPane) Handle(e gi.Event, u *gunim.UI) bool {
	if k, ok := e.(gi.KeyPress); ok && k.Key == gi.KeyDelete && k.Mods == 0 {
		if at, ok := p.table.Cursor(); ok {
			u.Send(p.table, ForgetCopy{Saved: p.kept[at]})
			return true
		}
	}
	return false
}
