package app

import (
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/text"

	"github.com/marrasen/kakel/fonts"
	"github.com/marrasen/kakel/glyph"
)

// The terminals' typeface: Go Mono, compiled in; the IBM VGA face,
// compiled in for the look of a DOS program; or a monospaced family
// installed on this machine, found by a scan as the window opens. The
// one picked from the Font menu is kept for next time.

// bundledFamily and dosFamily are what the Font menu calls the faces
// compiled in.
const (
	bundledFamily = "Go Mono (bundled)"
	dosFamily     = "PxPlus IBM VGA8"
)

// Font is the terminals' typeface as the window uses it.
type Font struct {
	// Name is the family, empty for Go Mono.
	Name string
	// Faces are its regular, bold, italic and bold italic faces; nil
	// ones are drawn from the regular face, and all nil is Go Mono.
	Faces [4]*text.Face
}

// Intents for fonts.
type (
	// PickFont draws the terminals in the family Name, from the Font
	// menu; empty is Go Mono.
	PickFont struct{ Name string }
)

// scanFonts looks for the monospaced families installed here, off the
// program's goroutine: it takes a second or two.
func (a *app) scanFonts() {
	go func() {
		found, err := glyph.Monospaced()
		a.events <- func() {
			a.families = found
			a.st.Fonts = []string{bundledFamily, dosFamily}
			for _, f := range found {
				if !strings.EqualFold(f.Name, dosFamily) {
					a.st.Fonts = append(a.st.Fonts, f.Name)
				}
			}
			if err != nil {
				a.failed("Couldn't read some fonts", err.Error())
			}
			// A family on disk, named on the command line or picked last
			// time, can only be had now.
			if a.fixedFont != "" {
				if err := a.setFont(a.fixedFont); err != nil {
					a.failed("Couldn't draw in -font-family "+a.fixedFont, err.Error())
				}
			}
			a.useKeptFont(true)
		}
	}()
}

// useKeptFont draws in the face picked from the Font menu last time: a
// compiled-in one at once, one on disk once the fonts are found.
func (a *app) useKeptFont(found bool) {
	kept := a.keptFont
	if _, compiled := compiledIn(kept); kept == "" || !compiled && !found {
		return
	}
	a.keptFont = ""
	if !a.haveFont(kept) {
		log.Printf("the font %q picked last time is not here, so this window is in %s", kept, bundledFamily)
		return
	}
	if err := a.setFont(kept); err != nil {
		a.failed("Couldn't draw in the font picked last time", err.Error())
	}
}

// pickFont is the Font menu's route to setFont, kept for next time.
func (a *app) pickFont(name string) error {
	// A pick made before the fonts are found is not undone by the one
	// kept from last time.
	a.keptFont = ""
	if err := a.setFont(name); err != nil {
		return err
	}
	if a.settings != nil {
		if err := a.settings.PutFontFamily(name); err != nil {
			a.failed("Couldn't keep the font for next time", err.Error())
		}
	}
	return nil
}

// haveFont reports whether a family is here to draw in.
func (a *app) haveFont(name string) bool {
	if _, ok := compiledIn(name); ok {
		return true
	}
	_, ok := a.familyNamed(name)
	return ok
}

// familyNamed finds an installed family, whatever the case of the name.
func (a *app) familyNamed(name string) (glyph.Family, bool) {
	for _, f := range a.families {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return glyph.Family{}, false
}

// compiledIn is the name a compiled-in face is kept under, and whether
// name is one: Go Mono answers to the empty name and to its menu line.
func compiledIn(name string) (string, bool) {
	switch {
	case name == "", strings.EqualFold(name, bundledFamily):
		return "", true
	case strings.EqualFold(name, dosFamily):
		return dosFamily, true
	}
	return "", false
}

// setFont draws the terminals in a family.
func (a *app) setFont(name string) error {
	var files glyph.Fonts
	if settled, ok := compiledIn(name); ok {
		name = settled
		if name == dosFamily {
			files = glyph.Fonts{Regular: fonts.DOS}
		}
	} else {
		family, ok := a.familyNamed(name)
		if !ok {
			return fmt.Errorf("there is no font family %q here", name)
		}
		loaded, err := family.Load()
		if err != nil {
			return err
		}
		files, name = loaded, family.Name
	}
	if strings.EqualFold(name, a.st.Font.Name) {
		return nil
	}
	faces, err := facesOf(files)
	if err != nil {
		return fmt.Errorf("font %q: %w", name, err)
	}
	a.st.Font = Font{Name: name, Faces: faces}
	return nil
}

// facesOf parses a family's files into faces, leaving nil the styles it
// has no file for.
func facesOf(files glyph.Fonts) ([4]*text.Face, error) {
	var faces [4]*text.Face
	for i, data := range [4][]byte{files.Regular, files.Bold, files.Italic, files.BoldItalic} {
		if data == nil {
			continue
		}
		face, err := parseFace(data, files.Index[i])
		if err != nil {
			return faces, err
		}
		faces[i] = face
	}
	return faces, nil
}

// parseFace reads one face: a font file, or the index'th face in a
// collection.
func parseFace(data []byte, index int) (*text.Face, error) {
	if face, err := text.Parse(data); err == nil && index == 0 {
		return face, nil
	}
	all, err := text.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	if index >= len(all) {
		return nil, fmt.Errorf("the collection has %d faces, not %d", len(all), index+1)
	}
	return all[index], nil
}

// FontCommandID names the command that draws in a family, as gridterm
// names it: lowercase, with dashes for spaces, and Go Mono "bundled".
func FontCommandID(family string) string {
	if _, ok := compiledIn(family); ok && !strings.EqualFold(family, dosFamily) {
		return "font.use.bundled"
	}
	return "font.use." + strings.ToLower(strings.ReplaceAll(family, " ", "-"))
}

// openCols and openRows are the terminal a new window opens with.
const openCols, openRows = 80, 30

// frameW and frameH are the window around the terminal: the room
// round its cells, twice the default of the view's kakel.term.padding,
// and the title bar and the pane's title above.
const frameW, frameH = 12, 64

// firstSize is how big a new window opens: room for a terminal of
// openCols by openRows at font size, in Go Mono.
func firstSize(size float32) geom.Size {
	face := text.GoMono(false, false)
	ascent, descent, gap := face.Metrics(size)
	_, advance, ok := face.Glyph('M', size)
	if !ok {
		advance = size * 0.6
	}
	w := float32(math.Round(float64(advance)))
	h := float32(math.Round(float64(ascent + descent + gap)))
	return geom.Sz(frameW+openCols*w, frameH+openRows*h)
}
