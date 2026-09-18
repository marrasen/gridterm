// Package appicon draws gridterm's own icon, at whatever size a window
// system or an icon file asks for.
package appicon

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Sizes are the sizes the icon file holds, smallest first. 16 and 32 are
// what Windows asks for in a title bar and a taskbar, 48 in Explorer,
// and 256 for a large-icon view and a high-resolution screen.
var Sizes = []int{16, 24, 32, 48, 64, 128, 256}

// windowSizes are the ones a running window is offered. A window system
// picks the nearest to what it wants and throws the rest away, so the
// large ones are left to the icon file and drawing them at every start
// would cost two frames for nothing.
var windowSizes = []int{16, 24, 32, 48}

// The colours. Three are the Dark theme's own, and the rim is its own
// colour: the ground is nearly black, and an icon with no edge is a hole
// on a dark taskbar.
var (
	ground = color.NRGBA{0x14, 0x17, 0x1c, 0xff}
	edge   = color.NRGBA{0x3a, 0x42, 0x50, 0xff}
	prompt = color.NRGBA{0x8f, 0xd4, 0x6a, 0xff}
	cursor = color.NRGBA{0xc8, 0xd0, 0xda, 0xff}
)

// The mark, in fractions of the icon's side: a chevron and the cursor
// after it, which is what a terminal waiting for a command looks like.
const (
	corner = 0.18
	rim    = 0.04

	chevronX0, chevronY0 = 0.28, 0.32
	chevronX1, chevronY1 = 0.48, 0.50
	chevronX2, chevronY2 = 0.28, 0.68
	chevronWidth         = 0.085

	cursorX0, cursorY0 = 0.54, 0.60
	cursorX1, cursorY1 = 0.76, 0.68
)

// samples is how many times each pixel is tested along each axis, which
// is what smooths the hard-edged shapes.
const samples = 4

// leastStroke is how thin a stroke of the mark may get, in pixels, so a
// sixteen-pixel chevron does not blur into a smudge.
const leastStroke = 2

// Images returns the icon at the sizes a running window is offered.
func Images() []image.Image {
	out := make([]image.Image, 0, len(windowSizes))
	for _, size := range windowSizes {
		out = append(out, Draw(size))
	}
	return out
}

// Draw returns the icon at one size, in pixels a side.
func Draw(size int) *image.NRGBA {
	if size <= 0 {
		return image.NewNRGBA(image.Rectangle{})
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	side := float64(size)
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, pixel(float64(x), float64(y), side))
		}
	}
	return img
}

// pixel is the colour of one pixel, by how much of it each shape covers.
func pixel(px, py, side float64) color.NRGBA {
	var inside, onEdge, onPrompt, onCursor float64
	const step = 1.0 / samples
	stroke := max(chevronWidth, leastStroke/side)
	// The bar takes the same floor, so it does not vanish either.
	bar := max(cursorY1-cursorY0, leastStroke/side)
	barY0 := cursorY0 + (cursorY1-cursorY0-bar)/2
	for sy := range samples {
		for sx := range samples {
			// The middle of each sample, in fractions of the side.
			u := (px + (float64(sx)+0.5)*step) / side
			v := (py + (float64(sy)+0.5)*step) / side
			switch {
			case !inRounded(u, v, corner):
				continue
			case inRounded(u, v, corner) && !inRounded(u, v, corner-rim/2, rim):
				inside++
				onEdge++
				continue
			}
			inside++
			switch {
			case onSegment(u, v, chevronX0, chevronY0, chevronX1, chevronY1, stroke),
				onSegment(u, v, chevronX1, chevronY1, chevronX2, chevronY2, stroke):
				onPrompt++
			case u >= cursorX0 && u <= cursorX1 && v >= barY0 && v <= barY0+bar:
				onCursor++
			}
		}
	}
	const all = samples * samples
	if inside == 0 {
		return color.NRGBA{}
	}
	// No sample is counted twice, so the four shares are weights that add
	// up to one rather than layers to composite.
	out := mix([]share{
		{ground, (inside - onEdge - onPrompt - onCursor) / inside},
		{edge, onEdge / inside},
		{prompt, onPrompt / inside},
		{cursor, onCursor / inside},
	})
	out.A = uint8(math.Round(inside / all * 255))
	return out
}

// share is one colour and how much of a pixel it covers.
type share struct {
	c    color.NRGBA
	part float64
}

// mix adds colours together by how much of the pixel each covers.
func mix(of []share) color.NRGBA {
	var r, g, b float64
	for _, s := range of {
		r += float64(s.c.R) * s.part
		g += float64(s.c.G) * s.part
		b += float64(s.c.B) * s.part
	}
	round := func(n float64) uint8 { return uint8(math.Round(min(max(n, 0), 255))) }
	return color.NRGBA{round(r), round(g), round(b), 0xff}
}

// inRounded reports whether a point is inside a rounded square with the
// given corner radius, shrunk by inset on every side.
func inRounded(u, v, radius float64, inset ...float64) bool {
	in := 0.0
	if len(inset) > 0 {
		in = inset[0]
	}
	lo, hi := in, 1-in
	if u < lo || u > hi || v < lo || v > hi {
		return false
	}
	// Only the corners are round: a point past the straight edges is in
	// only when it is within the radius of the corner's centre.
	cx := clamp(u, lo+radius, hi-radius)
	cy := clamp(v, lo+radius, hi-radius)
	dx, dy := u-cx, v-cy
	return dx*dx+dy*dy <= radius*radius
}

// onSegment reports whether a point is within half a stroke of the line
// from one point to another, ends included.
func onSegment(u, v, x0, y0, x1, y1, stroke float64) bool {
	dx, dy := x1-x0, y1-y0
	length := dx*dx + dy*dy
	t := 0.0
	if length > 0 {
		t = clamp(((u-x0)*dx+(v-y0)*dy)/length, 0, 1)
	}
	px, py := u-(x0+t*dx), v-(y0+t*dy)
	half := stroke / 2
	return px*px+py*py <= half*half
}

// clamp keeps a number between two others.
func clamp(n, lo, hi float64) float64 { return min(max(n, lo), hi) }

// ICO returns the icon as a Windows .ico file holding every size.
//
// Each image is stored as a PNG, which Windows has read inside an .ico
// since Vista and which keeps the file small at 256 pixels.
func ICO() ([]byte, error) {
	var body bytes.Buffer
	type entry struct {
		at, size int
		side     int
	}
	entries := make([]entry, 0, len(Sizes))
	for _, side := range Sizes {
		at := body.Len()
		if err := png.Encode(&body, Draw(side)); err != nil {
			return nil, fmt.Errorf("appicon: encode the %d pixel icon: %w", side, err)
		}
		entries = append(entries, entry{at: at, size: body.Len() - at, side: side})
	}

	var out bytes.Buffer
	// The header: reserved, type 1 for an icon, and how many there are.
	write := func(vals ...any) error {
		for _, v := range vals {
			if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
				return fmt.Errorf("appicon: write the icon file: %w", err)
			}
		}
		return nil
	}
	if err := write(uint16(0), uint16(1), uint16(len(entries))); err != nil {
		return nil, err
	}
	// Every entry sits after the header and the directory.
	start := 6 + 16*len(entries)
	for _, e := range entries {
		// A side of 256 is written as 0, which is what the one byte the
		// format gives it can hold.
		side := uint8(e.side)
		if err := write(side, side, uint8(0), uint8(0),
			uint16(1), uint16(32), uint32(e.size), uint32(start+e.at)); err != nil {
			return nil, err
		}
	}
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
