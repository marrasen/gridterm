package vt

import (
	"image/color"

	"github.com/marrasen/gridterm/grid"
)

// recolours moves the colours of one theme to the same places in
// another. It holds a table per channel, because a theme's default
// ground is often also one of its named colours and the two have to go
// different ways.
type recolours struct {
	fg, bg *channel
}

// recolour works out how each of the old theme's colours moves to the
// new one.
func recolour(was, now Palette) recolours {
	return recolours{
		fg: newChannel(was, now, was.FG, now.FG),
		bg: newChannel(was, now, was.BG, now.BG),
	}
}

// cell moves a cell's foreground and background.
func (m recolours) cell(c *grid.Cell) {
	c.FG, c.BG = m.fg.of(c.FG), m.bg.of(c.BG)
}

// buffer moves every cell of a buffer and of its scrollback.
func (m recolours) buffer(b *buffer) {
	if b == nil {
		return
	}
	for _, rows := range [][]line{b.lines, b.scrollback} {
		for _, l := range rows {
			for i := range l {
				m.cell(&l[i])
			}
		}
	}
}

// channel moves one of a cell's two colours, and remembers the last
// colour it moved, which nearly every cell shares.
type channel struct {
	to       map[color.RGBA]color.RGBA
	was, now color.RGBA
}

// newChannel builds one channel's table. The 256 named colours go in
// lowest first, so a colour a theme holds twice moves as the lower of
// the two, which is the one output names. The channel's own default goes
// in last and beats them all.
func newChannel(was, now Palette, from, onto color.RGBA) *channel {
	to := make(map[color.RGBA]color.RGBA, len(was.ANSI)+1)
	for i := range was.ANSI {
		if _, in := to[was.ANSI[i]]; !in {
			to[was.ANSI[i]] = now.ANSI[i]
		}
	}
	to[from] = onto
	return &channel{to: to, was: from, now: onto}
}

// of is the colour c moves to. A colour the old theme did not hold is
// left alone.
func (ch *channel) of(c color.RGBA) color.RGBA {
	if c == ch.was {
		return ch.now
	}
	to, in := ch.to[c]
	if !in {
		to = c
	}
	ch.was, ch.now = c, to
	return to
}
