package files

import (
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/syntax"
)

// run is a stretch of one line drawn in one colour, ending at end bytes
// into the line.
type run struct {
	end  int
	col  colour
	attr grid.Attr
}

// colour names one of the handful a reader draws with, rather than a
// colour itself: the reader turns it into one from its own style, so a
// file reads in the window's own scheme. They are the syntax package's
// colours, in the same order.
type colour uint8

const (
	// colourPlain is the reader's ordinary foreground.
	colourPlain = colour(syntax.Plain)
	// colourNote is a comment, or anything else beside the point.
	colourNote = colour(syntax.Note)
	// colourText is a string, a heading's text, or a quote.
	colourText = colour(syntax.Text)
	// colourMark is punctuation that carries meaning: a heading's
	// hashes, a bullet, a number. The log view uses it for a warning.
	colourMark = colour(syntax.Mark)
	// colourBad is a failure: the level of a log line that went wrong.
	//
	// Last, because the order is how loud each one is and the strip
	// beside the file takes the highest of a band.
	colourBad = colour(syntax.Bad)
)

// colourer turns a line into runs, into the runs given, from their
// start. A nil one leaves the file plain.
type colourer func(line string, into []run) []run

// colourerFor picks how a file is drawn, by what it is called, with the
// syntax package, and gives nil for a name it does not know.
func colourerFor(name string) colourer {
	c := syntax.For(name)
	if c == nil {
		return nil
	}
	var buf []syntax.Run
	return func(line string, into []run) []run {
		buf = c(line, buf[:0])
		out := into[:0]
		for _, r := range buf {
			out = append(out, run{end: r.End, col: colour(r.Colour), attr: r.Attr})
		}
		return out
	}
}
