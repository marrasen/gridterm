package vt

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
)

// findOutput is what a command that floods a terminal looks like: long
// paths, one per line, and nothing else.
func findOutput(lines int) []byte {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString("/usr/lib/x86_64-linux-gnu/perl-base/unicore/lib/Gc/")
		b.WriteString("Cntrl.pl\r\n")
	}
	return []byte(b.String())
}

// How fast a screenful of ordinary output can be parsed.
//
// A remote find(1) stopped with ctrl+c goes on filling the screen for
// as long as it takes to parse what is already in flight, so this is
// what says how long that is.
func BenchmarkWriteFindOutput(b *testing.B) {
	out := findOutput(2000)
	term := New(80, 24, DefaultPalette(), 1000, Callbacks{})
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := term.Write(out); err != nil {
			b.Fatal(err)
		}
	}
}

// The same, with no scrollback to push lines into.
func BenchmarkWriteFindOutputNoScrollback(b *testing.B) {
	out := findOutput(2000)
	term := New(80, 24, DefaultPalette(), 0, Callbacks{})
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := term.Write(out); err != nil {
			b.Fatal(err)
		}
	}
}

// And with the scrollback a window really keeps.
func BenchmarkWriteFindOutputDeepScrollback(b *testing.B) {
	out := findOutput(2000)
	term := New(80, 24, DefaultPalette(), DefaultScrollback, Callbacks{})
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := term.Write(out); err != nil {
			b.Fatal(err)
		}
	}
}

// Rendering a screenful, which happens once a frame however fast the
// program is talking.
func BenchmarkRenderScreenful(b *testing.B) {
	term := New(80, 24, DefaultPalette(), DefaultScrollback, Callbacks{})
	if _, err := term.Write(findOutput(100)); err != nil {
		b.Fatal(err)
	}
	g := grid.New(80, 24, DefaultPalette().FG, DefaultPalette().BG)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		term.Render(g)
	}
}

// Rendering a screen that has changed, which is what a frame costs while
// a program is talking.
//
// BenchmarkRenderScreenful above measures the other case: a screen
// nothing has written to since the last frame, which is most frames.
func BenchmarkRenderAfterOneLine(b *testing.B) {
	term := New(80, 24, DefaultPalette(), DefaultScrollback, Callbacks{})
	if _, err := term.Write(findOutput(100)); err != nil {
		b.Fatal(err)
	}
	g := grid.New(80, 24, DefaultPalette().FG, DefaultPalette().BG)
	term.Render(g)
	line := []byte("/usr/lib/x86_64-linux-gnu/perl-base/unicore/lib/Gc/Cntrl.pl\r\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := term.Write(line); err != nil {
			b.Fatal(err)
		}
		term.Render(g)
	}
}

// Rendering after one row changed without the screen scrolling, which is
// a prompt being typed at or a progress line counting up.
func BenchmarkRenderAfterOneRow(b *testing.B) {
	term := New(80, 24, DefaultPalette(), DefaultScrollback, Callbacks{})
	if _, err := term.Write(findOutput(100)); err != nil {
		b.Fatal(err)
	}
	g := grid.New(80, 24, DefaultPalette().FG, DefaultPalette().BG)
	term.Render(g)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Back to the top of the screen and over one row, so nothing
		// scrolls.
		if _, err := term.Write([]byte("\x1b[1;1Hworking" + string(rune('a'+i%26)))); err != nil {
			b.Fatal(err)
		}
		term.Render(g)
	}
}
