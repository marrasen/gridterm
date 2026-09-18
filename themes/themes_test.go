package themes

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/vt"
)

// sixteen is a full set of named colours, for a test whose point is
// something else.
const sixteen = `"ansi":["#000","#100","#200","#300","#400","#500","#600","#700",` +
	`"#800","#900","#a00","#b00","#c00","#d00","#e00","#f00"]`

// Every theme that comes with gridterm reads back as a palette.
func TestTheBuiltInThemesAreGood(t *testing.T) {
	built := Built()
	if len(built) < 2 {
		t.Fatalf("there are %d themes built in", len(built))
	}
	for _, theme := range built {
		p, err := theme.Palette()
		if err != nil {
			t.Errorf("%s: %v", theme.Name, err)
			continue
		}
		if p.FG == p.BG {
			t.Errorf("%s writes in the colour it writes on", theme.Name)
		}
		// The upper range is the layout every terminal agrees on, so a
		// program asking for colour 196 gets the red it expects.
		if want := (color.RGBA{0xff, 0, 0, 0xff}); p.ANSI[196] != want {
			t.Errorf("%s: colour 196 is %v, want %v", theme.Name, p.ANSI[196], want)
		}
	}
	if _, ok := Named(built, "dark"); !ok {
		t.Error("there is no Dark to open on")
	}
	// The Borland IDE, which is the look the menus already have.
	turbo, ok := Named(built, "turbo")
	if !ok {
		t.Fatal("there is no Turbo")
	}
	if turbo.BG != "#0000aa" || turbo.FG != "#ffff55" {
		t.Errorf("Turbo is %s on %s, want yellow on the Borland blue", turbo.FG, turbo.BG)
	}
}

// A colour is written the way a stylesheet writes one, long or short,
// with or without the hash.
func TestAColourIsReadTheWayItIsWritten(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want color.RGBA
	}{
		{"#14171c", color.RGBA{0x14, 0x17, 0x1c, 0xff}},
		{"14171c", color.RGBA{0x14, 0x17, 0x1c, 0xff}},
		{"#abc", color.RGBA{0xaa, 0xbb, 0xcc, 0xff}},
		{"  #FFFFFF  ", color.RGBA{0xff, 0xff, 0xff, 0xff}},
	} {
		got, err := ParseColour(c.raw)
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q read as %v, want %v", c.raw, got, c.want)
		}
	}
}

// And anything else is turned away with a line saying how to write one.
func TestSomethingThatIsNotAColourIsRefused(t *testing.T) {
	for _, raw := range []string{"", "red", "#12", "#1234567", "#gggggg", "rgb(1,2,3)"} {
		_, err := ParseColour(raw)
		if err == nil {
			t.Errorf("%q was read as a colour", raw)
			continue
		}
		if !strings.Contains(err.Error(), "#rrggbb") {
			t.Errorf("%q says %q, want it to say how to write one", raw, err)
		}
	}
}

// A colour written back reads the same again.
func TestAColourWrittenBackReadsTheSame(t *testing.T) {
	want := color.RGBA{0x14, 0x17, 0x1c, 0xff}

	got, err := ParseColour(Colour(want))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if got != want {
		t.Errorf("it came back as %v", got)
	}
}

// A theme naming the wrong number of colours is turned away, because the
// sixteen are what a program asks for by name.
func TestAThemeWithoutSixteenColoursIsRefused(t *testing.T) {
	theme := Theme{Name: "Short", FG: "#fff", BG: "#000", ANSI: []string{"#111", "#222"}}

	_, err := theme.Palette()

	if err == nil {
		t.Fatal("a theme with two colours was read")
	}
	if !strings.Contains(err.Error(), "Short") || !strings.Contains(err.Error(), "sixteen") {
		t.Errorf("it says %q", err)
	}
}

// A file that is not there is what the first run looks like: the themes
// that come with gridterm and no complaint.
func TestNoFileLeavesTheBuiltInThemes(t *testing.T) {
	got, err := Load(Path(t.TempDir()))

	if err != nil {
		t.Fatalf("no file: %v", err)
	}
	if !slices.Equal(Names(got), Names(Built())) {
		t.Errorf("it offers %v", Names(got))
	}
}

// A file written before the cursor colour went is still read.
//
// The window never used it: the cursor is drawn in the cell's own
// foreground and the glyph under it in the cell's background, so the
// character is always inverted whatever a theme said.
func TestAThemeStillCarryingACursorColourIsRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[{"name":"Old","fg":"#fff","bg":"#000",`+
		`"cursor":"#ff0000",`+sixteen+`}]}`)

	got, err := Load(Path(dir))

	if err != nil {
		t.Fatalf("a file with a cursor colour in it: %v", err)
	}
	if _, ok := Named(got, "Old"); !ok {
		t.Errorf("it offers %v, want the theme the file holds", Names(got))
	}
}

// The user's own themes come after the ones built in.
func TestThemesFromTheFileComeAfterTheBuiltInOnes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[{"name":"Mine","fg":"#fff","bg":"#000",`+sixteen+`}]}`)

	got, err := Load(Path(dir))

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := append(Names(Built()), "Mine"); !slices.Equal(Names(got), want) {
		t.Errorf("it offers %v, want %v", Names(got), want)
	}
	mine, ok := Named(got, "mine")
	if !ok {
		t.Fatal("the theme from the file is not offered")
	}
	p, err := mine.Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}
	if want := (color.RGBA{0xff, 0, 0, 0xff}); p.ANSI[15] != want {
		t.Errorf("its last colour is %v, want %v", p.ANSI[15], want)
	}
}

// A file with a colour nobody can read is turned away when it is read,
// rather than at the moment the user picks that theme.
func TestAThemeFileWithABadColourIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[{"name":"Mine","fg":"not a colour","bg":"#000",`+
		sixteen+`}]}`)

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a theme with a colour nobody can read was offered")
	}
	// And what it could read is still offered, so a window with a bad
	// file still has somewhere to start.
	if !slices.Equal(Names(got), Names(Built())) {
		t.Errorf("it offers %v", Names(got))
	}
}

// A theme with no name could not be picked, and one that takes a name
// already used would hide it.
func TestAThemeWithNoNameOrATakenOneIsRefused(t *testing.T) {
	for _, c := range []struct{ why, name, says string }{
		{"no name", "", "no name"},
		{"a name already used", "Dark", "two themes"},
	} {
		dir := t.TempDir()
		write(t, dir, `{"version":1,"themes":[{"name":"`+c.name+`","fg":"#fff","bg":"#000",`+
			sixteen+`}]}`)

		_, err := Load(Path(dir))

		if err == nil {
			t.Errorf("a theme with %s was offered", c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("a theme with %s says %q, want it to say %q", c.why, err, c.says)
		}
	}
}

// A file from a version this gridterm does not read is said so rather
// than guessed at.
func TestAThemeFileFromAnotherVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":99,"themes":[]}`)

	_, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a file from another version was read")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("it says %q", err)
	}
}

// write puts a themes file in a directory.
func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "themes.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A file that fails half way offers none of itself: a theme the user
// never wrote would otherwise appear beside the ones they did.
func TestAFileThatFailsHalfWayOffersNoneOfItself(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[`+
		`{"name":"Good","fg":"#fff","bg":"#000",`+sixteen+`},`+
		`{"name":"Bad","fg":"not a colour","bg":"#000",`+sixteen+`}]}`)

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a file with a bad theme in it was read")
	}
	if !slices.Equal(Names(got), Names(Built())) {
		t.Errorf("it offers %v, want only the ones built in", Names(got))
	}
}

// A theme naming no selection gets one off its own ground: selected
// text keeps its colour and is drawn on this, so the two cannot be one.
func TestAThemeWithNoSelectionGetsOneOffItsGround(t *testing.T) {
	theme := Theme{Name: "Bare", FG: "#ffffff", BG: "#000000", ANSI: sixteenColours()}

	p, err := theme.Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}

	if p.Selection == p.FG {
		t.Error("selected text is drawn on its own colour")
	}
}

// The Dark theme is the palette a window falls back to with none chosen
// at all, so the two cannot drift apart.
func TestTheDarkThemeIsTheFallbackPalette(t *testing.T) {
	dark, ok := Named(Built(), "Dark")
	if !ok {
		t.Fatal("there is no Dark")
	}

	got, err := dark.Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}

	if want := vt.DefaultPalette(); got != want {
		t.Error("Dark is not the palette vt falls back to")
	}
}

// A starting file holds the theme it was made from, under a name of its
// own, and reads back.
func TestAStartingFileReadsBack(t *testing.T) {
	dir := t.TempDir()
	from, _ := Named(Built(), "Dark")

	if err := WriteStart(Path(dir), from); err != nil {
		t.Fatalf("write it: %v", err)
	}

	all, err := Load(Path(dir))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(all) != len(Built())+1 {
		t.Fatalf("it offers %v", Names(all))
	}
	mine := all[len(all)-1]
	if mine.Name == from.Name {
		t.Errorf("it wrote the theme under the name it came with, %q", mine.Name)
	}
	got, err := mine.Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}
	want, _ := from.Palette()
	if got != want {
		t.Error("what it wrote is not the theme it was made from")
	}
}

// And a file that is already there is not written over: what is in one
// is the user's, and sixteen colours cannot be got back.
func TestAStartingFileDoesNotWriteOverOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[]}`)
	from, _ := Named(Built(), "Dark")

	err := WriteStart(Path(dir), from)

	if err == nil {
		t.Fatal("it wrote over a file that was there")
	}
	if !strings.Contains(err.Error(), "already there") {
		t.Errorf("it says %q", err)
	}
}

// sixteenColours is a full set of named colours for a test whose point
// is something else.
func sixteenColours() []string {
	out := make([]string, 16)
	for i := range out {
		out[i] = "#" + string(rune('0'+i%10)) + "00"
	}
	return out
}

// A theme with no frame block leaves the window to work its furniture
// out, which is what every theme did before there was a block.
func TestAThemeWithNoFrameBlockSaysNothingAboutTheFurniture(t *testing.T) {
	theme := Theme{Name: "Bare", FG: "#ffffff", BG: "#000000", ANSI: sixteenColours()}

	got, err := theme.Look()

	if err != nil {
		t.Fatalf("its look: %v", err)
	}
	if got.Set {
		t.Errorf("a theme with no frame block gave %+v, want nothing said", got)
	}
}

// A frame block says what the furniture is written in and sits on.
func TestAFrameBlockIsRead(t *testing.T) {
	theme := Theme{Name: "Boxy", FG: "#ffff55", BG: "#0000aa", ANSI: sixteenColours(),
		Frame: &Frame{FG: "#000000", BG: "#aaaaaa", Border: "double"}}

	got, err := theme.Look()

	if err != nil {
		t.Fatalf("its look: %v", err)
	}
	if !got.Set {
		t.Fatal("a theme with a frame block says nothing about the furniture")
	}
	if want := (color.RGBA{0, 0, 0, 0xff}); got.FG != want {
		t.Errorf("the furniture is written in %v, want %v", got.FG, want)
	}
	if want := (color.RGBA{0xaa, 0xaa, 0xaa, 0xff}); got.BG != want {
		t.Errorf("the furniture sits on %v, want %v", got.BG, want)
	}
	if !got.Double {
		t.Error("a double border was asked for and the look says single")
	}
}

// A button falls back to the frame turned round, which is what marks one
// out from the dialog it sits on.
func TestAButtonFallsBackToTheFrameTurnedRound(t *testing.T) {
	theme := Theme{Name: "Boxy", FG: "#ffff55", BG: "#0000aa", ANSI: sixteenColours(),
		Frame: &Frame{FG: "#000000", BG: "#aaaaaa"}}

	got, err := theme.Look()

	if err != nil {
		t.Fatalf("its look: %v", err)
	}
	if got.ButtonFG != got.BG || got.ButtonBG != got.FG {
		t.Errorf("a button is %v on %v, want the frame's %v on %v turned round",
			got.ButtonFG, got.ButtonBG, got.FG, got.BG)
	}
	if got.ActiveFG != got.BG || got.ActiveBG != got.FG {
		t.Errorf("the button Enter presses is %v on %v, want the frame turned round",
			got.ActiveFG, got.ActiveBG)
	}
}

// A border nobody can draw is turned away when the file is read, rather
// than leaving the window to guess at it later.
func TestABorderThatIsNeitherSingleNorDoubleIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"themes":[{"name":"Odd","fg":"#fff","bg":"#000",`+
		`"frame":{"fg":"#000","bg":"#aaa","border":"wobbly"},`+sixteen+`}]}`)

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a border called \"wobbly\" was read")
	}
	if !strings.Contains(err.Error(), "wobbly") {
		t.Errorf("it says %q", err)
	}
	if !slices.Equal(Names(got), Names(Built())) {
		t.Errorf("it offers %v, want only the ones built in", Names(got))
	}
}

// A frame block with no colours in it is turned away, and says which
// two it wants. The block is what a window draws its own furniture in,
// so a theme that writes one has to say what those colours are.
func TestAFrameBlockWithNoColoursSaysWhichItWants(t *testing.T) {
	for what, f := range map[string]*Frame{
		"nothing at all":       {},
		"only a border":        {Border: "double"},
		"a ground and no text": {BG: "#aaaaaa"},
	} {
		theme := Theme{Name: "Half", FG: "#fff", BG: "#000", ANSI: sixteenColours(), Frame: f}

		_, err := theme.Look()

		if err == nil {
			t.Errorf("a frame block with %s was read", what)
			continue
		}
		if !strings.Contains(err.Error(), "fg and bg") {
			t.Errorf("a frame block with %s says %q, and it has to say which two it wants", what, err)
		}
	}
}

// A theme that wrote its frame down gets a flat dialog: an opaque box
// with no glass behind it. Glass behind an opaque box is paid for and
// never seen.
func TestAStatedFrameIsAFlatOne(t *testing.T) {
	theme := Theme{Name: "Boxy", FG: "#ffff55", BG: "#0000aa", ANSI: sixteenColours(),
		Frame: &Frame{FG: "#000000", BG: "#aaaaaa"}}

	got, err := theme.Look()

	if err != nil {
		t.Fatalf("its look: %v", err)
	}
	if !got.Set {
		t.Error("a theme that named its frame says nothing about it")
	}
	if got.BG.A != 0xff {
		t.Errorf("the frame's ground is %v, and a flat dialog needs an opaque one", got.BG)
	}
}

// A start file written from a theme that named its frame carries the
// frame too, so the file the user edits shows what a frame block looks
// like rather than leaving them to guess.
func TestAStartFileCarriesTheFrameBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	turbo, ok := Named(Built(), "Turbo")
	if !ok {
		t.Fatal("there is no Turbo to write from")
	}

	if err := WriteStart(path, turbo); err != nil {
		t.Fatalf("write it: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if !strings.Contains(string(raw), `"frame"`) {
		t.Errorf("the file it wrote holds no frame block:\n%s", raw)
	}
	all, err := Load(path)
	if err != nil {
		t.Fatalf("load what it wrote: %v", err)
	}
	mine, ok := Named(all, turbo.Name+" of my own")
	if !ok {
		t.Fatalf("it wrote %v, want a copy of Turbo", Names(all))
	}
	got, err := mine.Look()
	if err != nil {
		t.Fatalf("its look: %v", err)
	}
	want, _ := turbo.Look()
	if got != want {
		t.Errorf("the copy's frame is %+v, want Turbo's %+v", got, want)
	}
}
