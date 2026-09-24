package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
)

// jobPane is one piece of file work, on screen: what it is on now, how
// far it has got, how fast it is going, and how it ended.
//
// A pane rather than a dialog. A copy takes as long as it takes, and a
// box that has to be dismissed before anything else can be done is the
// wrong shape for something to watch: it covers the window it was
// opened from, it takes the keys, and it cannot be left open beside the
// work that carries on. The rule this follows is that a dialog asks and
// a pane shows.
type jobPane struct {
	app   *app
	job   *jobs.Job
	entry *conns.Entry

	// from and to are the ends the work was started between, taken when
	// the job began. A repeat opens them again, because the filesystems
	// the job ran on may be closed by then.
	from, to jobEnd

	size ui.Size

	// at is the button the keyboard is on, cols where each was last
	// drawn, and drawn what they were, for a click. A job that finished
	// between the drawing and the click changes what is offered, and a
	// click measured against the old row would press the new thing in
	// that place -- Repeat sits where Cancel was.
	at    int
	cols  []int
	drawn []choice

	// row is the row the buttons were last drawn on, for the same
	// reason. Below zero when none have been.
	row int

	// wasDone is whether the job had finished when the pane last drew,
	// so the focus can be moved when it changes.
	wasDone bool
}

// How the pane is laid out: the blank margin each side, and the rows
// that are always there whatever else fits.
const (
	jobPaneMargin = 2

	// jobPaneGraph is how many rows the speed graph is given, and
	// jobPaneBarRow, jobPaneFactsRow and so on are where each part goes
	// under the heading.
	jobPaneGraph = 4

	// jobPaneRun is how many seconds there have to be before a run is
	// worth drawing at all.
	jobPaneRun = 3
)

// newJobPane opens a pane on a piece of file work.
func newJobPane(a *app, j *jobs.Job, e *conns.Entry, from, to jobEnd) *jobPane {
	return &jobPane{app: a, job: j, entry: e, from: from, to: to, row: -1}
}

// Layout takes the room it is given.
func (p *jobPane) Layout(size ui.Size) { p.size = size }

// Draw paints the whole pane, every frame: a job's numbers move on
// their own, so there is nothing to remember between frames.
func (p *jobPane) Draw(v grid.View) {
	p.draw(v, p.job.Progress(), p.app.frameTime())
}

// draw paints one reading of the job, taken apart from Draw so a test
// can hand it a job part way through rather than having to arrange for
// one.
func (p *jobPane) draw(v grid.View, prog jobs.Progress, now time.Time) {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return
	}
	st := p.app.formStyle()
	at := jobPaneMargin
	width := max(cols-2*jobPaneMargin, 1)
	// Nothing above the heading clears this row, and a pane draws over
	// whatever the widget before it left in its place.
	blankRow(v, p.app.colours.BG, 0)
	line := 1

	// The heading: what this is, and how much of it there is.
	head := penOn(v, p.app.colours.BG, line)
	head.skip(at)
	head.write(p.heading(prog), st.TitleFG)
	head.right(p.counted(prog), st.HintFG, jobPaneMargin)
	head.rest()
	blankRow(v, p.app.colours.BG, line+1)
	line += 2

	// The bar, with the share it has done written at the end of it.
	done := shareOf(prog)
	share := fmt.Sprintf("%3d%%", int(done*100))
	barWidth := max(width-len(share)-1, 1)
	bar := penOn(v, p.app.colours.BG, line)
	bar.skip(at)
	drawBar(bar, barWidth, done, p.app.fillBG(), st.HintFG)
	bar.skip(1)
	bar.write(share, st.FG)
	bar.rest()
	blankRow(v, p.app.colours.BG, line+1)
	line += 2

	// The numbers under it: how much, how fast, how long left.
	sayRow(v, p.app.colours.BG, at, line, width, p.facts(prog, now), st.FG)
	blankRow(v, p.app.colours.BG, line+1)
	line += 2

	// And how it ended, which for one that failed is the whole of what
	// there is to know. Wrapped: a disk's reason for refusing a write
	// is a sentence, not a word.
	if prog.Done {
		for _, said := range wrapLines(outcomeOf(prog), width) {
			sayRow(v, p.app.colours.BG, at, line, width, said, st.FG)
			line++
		}
		blankRow(v, p.app.colours.BG, line)
		line++
	}

	// The last seconds of it, drawn as a run rather than a number.
	//
	// Only while it is running. A finished job's run would go on
	// shifting as the seconds are sampled past it, which is a pane that
	// repaints for ever over work that is over -- and "is this going"
	// is not a question about it any more.
	if !prog.Done && rows > line+jobPaneGraph+3 {
		if drawn := p.drawGraph(v, at, line, width, now); drawn {
			blankRow(v, p.app.colours.BG, line+jobPaneGraph)
			line += jobPaneGraph + 1
		}
	}

	// What it is working on, and the names it was given.
	line = p.drawNames(v, at, line, width, rows, prog, st)

	// Everything between what has been drawn and the bottom, so a pane
	// that says less than it did leaves nothing of the old behind.
	row := buttonRow(rows, line)
	blankRest(v, p.app.colours.BG, line, rows, row)

	// And what can be done about it, along the bottom.
	p.settle(prog)
	p.drawButtons(v, prog, row)
}

// shareOf is how much of the work is done, as a share of one.
//
// A job that stopped part way keeps the share it reached: a copy
// cancelled at a fifth drawing a full bar over "It was cancelled" would
// be the pane contradicting itself.
func shareOf(prog jobs.Progress) float64 {
	if prog.Done && prog.Err == nil {
		return 1
	}
	switch {
	case prog.Bytes > 0:
		return min(float64(prog.BytesDone)/float64(prog.Bytes), 1)
	case prog.Files > 0:
		return min(float64(prog.FilesDone)/float64(prog.Files), 1)
	}
	return 0
}

// heading says what the work is and where it is going.
func (p *jobPane) heading(prog jobs.Progress) string {
	doing := p.doingWord()
	if prog.Done {
		doing = p.doneWord()
	}
	where := p.to.host
	if p.job.Kind() == jobs.Delete || where == "" {
		return doing
	}
	return doing + " to " + groupName(where)
}

// doingWord is the heading of a job that is running: what it is doing,
// rather than what it is.
func (p *jobPane) doingWord() string {
	switch p.job.Kind() {
	case jobs.Copy:
		return "Copying"
	case jobs.Move:
		return "Moving"
	case jobs.Delete:
		return "Deleting"
	}
	return p.job.Kind().String()
}

// doneWord is the heading of a job that has stopped.
func (p *jobPane) doneWord() string {
	prog := p.job.Progress()
	switch {
	case prog.Err != nil:
		return p.job.Kind().String() + " stopped"
	case p.job.Kind() == jobs.Copy:
		return "Copied"
	case p.job.Kind() == jobs.Move:
		return "Moved"
	case p.job.Kind() == jobs.Delete:
		return "Deleted"
	}
	return "Finished"
}

// counted is how many files there are, at the end of the heading.
func (p *jobPane) counted(prog jobs.Progress) string {
	if prog.Files == 0 {
		return "looking at what there is"
	}
	if prog.Done {
		return amountOf(prog, true)
	}
	// The files alone: how many bytes there are is on the line under
	// the bar, and a heading that said it too would say it twice.
	return fmt.Sprintf("%d of %s", prog.FilesDone, fileWord(prog.Files))
}

// facts is the line of numbers: how much has moved, how fast, and how
// long is left at that speed.
func (p *jobPane) facts(prog jobs.Progress, now time.Time) string {
	var said []string
	if prog.Bytes > 0 {
		said = append(said, size(prog.BytesDone)+" of "+size(prog.Bytes))
	} else if prog.BytesDone > 0 {
		said = append(said, size(prog.BytesDone))
	}
	if prog.Done {
		said = append(said, took(prog.Ended.Sub(prog.Started)))
		if speed := p.average(prog); speed != "" {
			said = append(said, speed+" on average")
		}
		return strings.Join(said, "   ")
	}
	if speed := meter.Speed(p.speed(now)); speed != "" {
		said = append(said, speed)
	}
	said = append(said, soFar(now.Sub(prog.Started)))
	if left := p.timeLeft(prog, now); left != "" {
		said = append(said, left)
	}
	return strings.Join(said, "   ")
}

// took is how long a job that has stopped ran for.
//
// soFar is written for a job that is still going -- "going 12 s" -- and
// reads wrongly of one that has stopped.
func took(d time.Duration) string {
	d = d.Truncate(time.Second)
	switch {
	case d < time.Second:
		return "in under a second"
	case d < time.Minute:
		return fmt.Sprintf("in %d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("in %d min %d s", int(d/time.Minute), int(d/time.Second)%60)
	}
	return fmt.Sprintf("in %d h %d min", int(d/time.Hour), int(d/time.Minute)%60)
}

// average is how fast the whole job went, for one that has finished.
func (p *jobPane) average(prog jobs.Progress) string {
	took := prog.Ended.Sub(prog.Started)
	if took <= 0 || prog.BytesDone <= 0 {
		return ""
	}
	return meter.Speed(uint64(float64(prog.BytesDone) / took.Seconds()))
}

// timeLeft is how long the rest would take at the speed it is going.
//
// Empty until there is a speed and a total to work from: a number made
// up out of nothing is worse than no number, because it is read as one.
func (p *jobPane) timeLeft(prog jobs.Progress, now time.Time) string {
	speed := p.speed(now)
	if speed == 0 || prog.Bytes <= prog.BytesDone {
		return ""
	}
	left := time.Duration(float64(prog.Bytes-prog.BytesDone)/float64(speed)) * time.Second
	return "about " + howLongIs(left) + " left"
}

// howLongIs is a length of time on its own, with no word in front of
// it saying whether it has passed or is still to come.
func howLongIs(d time.Duration) string {
	d = d.Truncate(time.Second)
	switch {
	case d < time.Second:
		return "a second"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d min %d s", int(d/time.Minute), int(d/time.Second)%60)
	}
	return fmt.Sprintf("%d h %d min", int(d/time.Hour), int(d/time.Minute)%60)
}

// speed is how fast the job is going, taken from the row's own rate so
// that the pane and the sidebar say the same number.
func (p *jobPane) speed(now time.Time) uint64 {
	rate := p.app.rates[p.entry]
	if rate == nil || p.entry.Meter == nil {
		return 0
	}
	in, out := rate.Sample(p.entry.Meter, now)
	return max(in, out)
}

// drawGraph paints the last seconds of the run, and reports whether
// there was a run to draw.
func (p *jobPane) drawGraph(v grid.View, at, line, width int, now time.Time) bool {
	rate := p.app.rates[p.entry]
	if rate == nil {
		return false
	}
	p.speed(now)
	past := rate.Past()
	// A run of a second or two has no shape to show, and one bar on its
	// own reads as a mark rather than as a graph.
	if len(past) < jobPaneRun {
		return false
	}
	var most uint64
	for _, speed := range past {
		most = max(most, speed)
	}
	if most == 0 {
		return false
	}
	for y := range jobPaneGraph {
		pen := penOn(v, p.app.colours.BG, line+y)
		pen.skip(at)
		drawRun(pen, width, past, most, y, jobPaneGraph, p.app.fillBG())
		pen.rest()
	}
	return true
}

// drawNames writes what the job is on now and the names it was given,
// and hands back the row after them.
func (p *jobPane) drawNames(v grid.View, at, line, width, rows int,
	prog jobs.Progress, st ui.FormStyle) int {

	names := p.job.Op().Names
	if len(names) == 0 {
		return line
	}
	// Room for the buttons and a blank row above them.
	room := rows - line - 3
	if room < 1 {
		return line
	}
	// One name is the heading's business, not a list's.
	if len(names) == 1 && prog.Current == "" {
		return line
	}
	for i, name := range names {
		// The last row the names have goes to saying how many are not
		// drawn, rather than to one more name.
		if i >= room-1 && len(names)-i > 1 {
			sayRow(v, p.app.colours.BG, at, line, width,
				fmt.Sprintf("and %d more", len(names)-i), st.HintFG)
			line++
			break
		}
		mark, fg := p.markOf(i, name, prog, st)
		pen := penOn(v, p.app.colours.BG, line)
		pen.skip(at)
		pen.write(mark+name, fg)
		pen.rest()
		line++
	}
	// The row under the names, so a list that shrank -- the graph goes
	// when the job finishes, and everything moves up -- leaves none of
	// the old one behind.
	blankRow(v, p.app.colours.BG, line)
	return line + 1
}

// markOf is the character in front of a name and the colour to draw the
// name in.
//
// A tick only where the count can be trusted: the job walks what it was
// given in order, so with one file per name the number done says which
// names are behind it. A directory stands for however many files are
// inside it, and then nothing here knows which name the count has
// reached.
func (p *jobPane) markOf(i int, name string, prog jobs.Progress,
	st ui.FormStyle) (string, color.RGBA) {

	if name == prog.Current {
		return " > ", st.FG
	}
	byName := prog.Files == len(p.job.Op().Names)
	switch {
	case prog.Done && prog.Err == nil:
		return " " + string(tick) + " ", st.HintFG
	case byName && i < prog.FilesDone:
		return " " + string(tick) + " ", st.HintFG
	}
	return "   ", st.HintFG
}

// tick marks a name the job is past.
const tick = '✓'

// settle moves the focus when a job finishes under it.
//
// Cancel and Repeat sit in the same place, so a job that finished while
// the finger was on Cancel would turn the next Enter into a copy the
// user never asked for. The focus goes to Close, which does nothing to
// the work.
func (p *jobPane) settle(prog jobs.Progress) {
	if prog.Done == p.wasDone {
		return
	}
	p.wasDone = prog.Done
	p.at = len(p.choicesFor(prog)) - 1
}

// choices are what this job offers now, left to right.
//
// The same words the dialog used, because they mean the same things: a
// job that is running can be cancelled, and one that has finished can
// be done again. Close takes the pane away and leaves the job alone --
// a copy goes on if it is still going, the way closing any pane does
// not end what is behind it.
func (p *jobPane) choices() []choice { return p.choicesFor(p.job.Progress()) }

// choicesFor is what a job offers at one reading of it.
//
// One reading for the whole frame: the buttons are part of what the
// pane says, and a row drawn from a later reading than the bar above it
// would offer Repeat over a copy the rest of the pane still shows
// running.
func (p *jobPane) choicesFor(prog jobs.Progress) []choice {
	if !prog.Done {
		return []choice{{title: btnCancel}, {title: btnClose}}
	}
	if p.job.Kind() != jobs.Copy {
		return []choice{{title: btnClose}}
	}
	// Only a copy: doing a delete again would take something away
	// without asking, and a move has already taken the original away,
	// so there is nothing at that end to move a second time.
	out := []choice{{title: btnRepeat}}
	if saved, can := asSavedCopy(p.job.Op(), p.from, p.to); can {
		// A box rather than a button that renamed itself between
		// keeping and forgetting, which is two commands wearing one
		// label.
		out = append(out, choice{title: fldSaveCopy, tick: true,
			on: p.app.copies.has(saved)})
	}
	return append(out, choice{title: btnClose})
}

// drawButtons paints the row along the bottom, centred.
func (p *jobPane) drawButtons(v grid.View, prog jobs.Progress, row int) {
	cols, _ := v.Size()
	choices := p.choicesFor(prog)
	p.at = min(max(p.at, 0), len(choices)-1)
	st := p.app.formStyle()
	if row < 0 {
		// Too short to draw them anywhere they would not be written
		// over. Nothing to click, so nothing is remembered as drawn.
		p.row, p.drawn = -1, p.drawn[:0]
		return
	}
	p.row = row
	p.cols = placeChoices(p.cols[:0], choices, cols)
	p.drawn = append(p.drawn[:0], choices...)
	pen := penOn(v, p.app.colours.BG, row)
	for i, at := range p.cols {
		if at < 0 {
			continue
		}
		pen.skip(at - pen.at)
		fg, bg := st.ButtonFG, st.ButtonBG
		if i == p.at {
			fg, bg = st.ActiveFG, st.ActiveBG
		}
		if choices[i].tick {
			// No button ground unless it is the one being looked at: a
			// box is read as one thing to set, not as something to
			// press.
			if i != p.at {
				fg, bg = st.FG, p.app.colours.BG
			}
			pen.writeOn(choices[i].label(), fg, bg)
			continue
		}
		pen.writeOn(" "+choices[i].title+" ", fg, bg)
	}
	pen.rest()
}

// HandleKey moves along the buttons and presses one.
//
// Every other key is left alone: this is a pane, sharing the window
// with the rest of it, so it must not swallow the shortcuts of
// everything around it.
func (p *jobPane) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress {
		return false, nil
	}
	choices := p.choices()
	switch ev.Key {
	case input.KeyLeft:
		p.at = (p.at - 1 + len(choices)) % len(choices)
	case input.KeyRight, input.KeyTab:
		p.at = (p.at + 1) % len(choices)
	case input.KeyEnter, input.KeySpace:
		if !sameChoices(choices, p.drawn) {
			// The job finished since this row was drawn, so what the
			// key was aimed at has moved along it: Repeat is where
			// Cancel was.
			p.app.markDirty()
			return true, nil
		}
		return true, p.press(p.at)
	default:
		return false, nil
	}
	p.app.markDirty()
	return true, nil
}

// HandleMouse presses the button under the pointer.
func (p *jobPane) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button != input.MouseLeft || p.row < 0 {
		return false, nil
	}
	if ev.Row != p.row {
		return false, nil
	}
	choices := p.choices()
	if !sameChoices(choices, p.drawn) {
		// What is offered has changed since this row was drawn, so
		// where the pointer went is not what it went to. The next frame
		// draws the new row, and a second click presses what it says.
		p.app.markDirty()
		return true, nil
	}
	for i, at := range p.cols {
		if at < 0 || i >= len(choices) {
			continue
		}
		if ev.Col >= at && ev.Col < at+choices[i].width() {
			p.at = i
			return true, p.press(i)
		}
	}
	return false, nil
}

// sameChoices reports whether a row offers what it offered when it was
// drawn. The tick's state is not part of it: a box that was ticked from
// somewhere else is still the same box in the same place.
func sameChoices(now, drawn []choice) bool {
	if len(now) != len(drawn) {
		return false
	}
	for i := range now {
		if now[i].title != drawn[i].title || now[i].tick != drawn[i].tick {
			return false
		}
	}
	return true
}

// press does what the choice at i says.
func (p *jobPane) press(i int) error {
	choices := p.choices()
	if i < 0 || i >= len(choices) {
		return nil
	}
	if choices[i].tick {
		return p.keepCopy(!choices[i].on)
	}
	switch choices[i].title {
	case btnCancel:
		// Not Drop: they asked for the work to stop, not for the row to
		// go, and the row is where the outcome is read.
		p.job.Cancel()
	case btnRepeat:
		op, from, to := p.job.Op(), p.from, p.to
		p.app.repeatJob(op, from, to)
	case btnClose:
		return p.app.closePane(p)
	}
	p.app.markDirty()
	return nil
}

// keepCopy puts this copy on the saved list, or takes it off.
//
// The box says what the list holds, so one that could not be written is
// put back rather than left saying what the user asked for and did not
// get.
func (p *jobPane) keepCopy(on bool) error {
	saved, can := asSavedCopy(p.job.Op(), p.from, p.to)
	if !can {
		return nil
	}
	p.app.markDirty()
	if on {
		return p.app.copies.keep(saved)
	}
	return p.app.copies.forget(saved)
}

// showJobPane opens the pane on a piece of file work, or goes to the
// one already open on it.
func (a *app) showJobPane(j *jobs.Job, e *conns.Entry, from, to jobEnd) {
	if pane := a.jobPaneFor(j); pane != nil {
		a.focus(pane)
		return
	}
	pane := newJobPane(a, j, e, from, to)
	a.jobPanes = append(a.jobPanes, pane)
	if err := a.placePane(pane); err != nil {
		a.jobPanes = a.jobPanes[:len(a.jobPanes)-1]
		a.reportError("Could not open a pane on the file work", err)
	}
}

// jobPaneFor is the pane already open on a job, or nil.
func (a *app) jobPaneFor(j *jobs.Job) *jobPane {
	for _, pane := range a.jobPanes {
		if pane.job == j {
			return pane
		}
	}
	return nil
}

// forgetJobPane takes a closed pane off the window's record of them.
func (a *app) forgetJobPane(w ui.Widget) {
	pane, is := w.(*jobPane)
	if !is {
		return
	}
	for i, open := range a.jobPanes {
		if open == pane {
			a.jobPanes = append(a.jobPanes[:i], a.jobPanes[i+1:]...)
			// Let go of the row as well: what the window files a pane
			// under is what says the pane is open at all, and a closed
			// one still filed under a row is one the switcher goes on
			// drawing a tile for. The row itself stays on the sidebar
			// -- it is the job's, not the pane's.
			pane.entry = nil
			return
		}
	}
}
