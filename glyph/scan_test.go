package glyph

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/gofont/gosmallcaps"
)

// fontDir writes the given fonts into a directory of their own and
// returns it. The keys are filenames, so a test can put a font under any
// name and check the scan reads the file rather than trusting it.
func fontDir(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("make %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// goMonoFiles is the bundled family, one file per style.
func goMonoFiles() map[string][]byte {
	return map[string][]byte{
		"gomono.ttf":           gomono.TTF,
		"gomonobold.ttf":       gomonobold.TTF,
		"gomonoitalic.ttf":     gomonoitalic.TTF,
		"gomonobolditalic.ttf": gomonobolditalic.TTF,
	}
}

// mustScan runs the scan and fails the test if reading the directories
// failed, which is the answer every test here expects.
func mustScan(t *testing.T, dirs []string) []Family {
	t.Helper()
	fams, err := monospacedIn(dirs)
	if err != nil {
		t.Fatalf("scan %v: %v", dirs, err)
	}
	return fams
}

func TestScanGroupsAFamilysFourStyles(t *testing.T) {
	dir := fontDir(t, goMonoFiles())

	fams := mustScan(t, []string{dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want one: %+v", len(fams), fams)
	}
	f := fams[0]
	if f.Name != "Go Mono" {
		t.Errorf("family = %q, want %q", f.Name, "Go Mono")
	}
	for s := range numStyles {
		if !f.Has(s) {
			t.Errorf("style %d is missing, want all four in one family", s)
		}
	}
	// Each style must come from its own file, not all from one.
	seen := map[string]bool{}
	for s := range numStyles {
		if seen[f.Src[s].Path] {
			t.Errorf("style %d shares a file with another style", s)
		}
		seen[f.Src[s].Path] = true
	}
}

// TestScanRejectsAProportionalFont checks the whole point of the scan: a
// terminal drawn in a font whose letters are different widths has every
// column in the wrong place.
//
// The proportional faces here are a family called "Go", not "Go
// Regular": name id 1 is the family, and the weight is the subfamily.
// Looking for the wrong name would make this test prove nothing.
func TestScanRejectsAProportionalFont(t *testing.T) {
	dir := fontDir(t, map[string][]byte{
		"goregular.ttf":   goregular.TTF,
		"gobold.ttf":      gobold.TTF,
		"goitalic.ttf":    goitalic.TTF,
		"gomedium.ttf":    gomedium.TTF,
		"gosmallcaps.ttf": gosmallcaps.TTF,
		"gomono.ttf":      gomono.TTF,
	})

	fams := mustScan(t, []string{dir})

	// Exactly one family, so a proportional face slipping through under
	// any name at all fails this.
	if len(fams) != 1 || fams[0].Name != "Go Mono" {
		t.Errorf("families = %+v, want only Go Mono", fams)
	}
}

// TestScanSkipsWhatItCannotRead checks that one bad file among many does
// not cost the user every font on the system, and that nothing is said
// about it.
//
// A file that reads and will not parse is a face this program cannot
// use, not a disk failing. A stock Windows install has several, and a
// dialog naming them at every start is a dialog nobody reads.
func TestScanSkipsWhatItCannotRead(t *testing.T) {
	files := goMonoFiles()
	files["broken.ttf"] = []byte("this is not a font")
	files["empty.ttf"] = nil
	dir := fontDir(t, files)

	fams, err := monospacedIn([]string{dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want the one good family: %+v", len(fams), fams)
	}
	if fams[0].Name != "Go Mono" {
		t.Errorf("family = %q, want %q", fams[0].Name, "Go Mono")
	}
	if err != nil {
		t.Errorf("two files that will not parse were reported: %v", err)
	}
}

// TestScanIgnoresAFileNotNamedLikeAFont checks that the extension is
// what decides, so the scan does not read every file on the machine
// hoping one of them parses.
func TestScanIgnoresAFileNotNamedLikeAFont(t *testing.T) {
	// A real font, under a name that is not a font's.
	dir := fontDir(t, map[string][]byte{"notes.txt": gomono.TTF})

	paths, failed := fontFilesIn([]string{dir})

	if len(failed) != 0 {
		t.Fatalf("reading the directory failed: %v", failed)
	}
	if len(paths) != 0 {
		t.Errorf("scanned %v; a font by content is still not a font file by name", paths)
	}
}

// TestScanPrefersTheSameFileEveryRun checks that two copies of one
// family give the same answer however the directories are listed. The
// files are walked in sorted order and the first found for a style wins,
// so the font a window opens with does not change between runs.
func TestScanPrefersTheSameFileEveryRun(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "gomono.ttf"), gomono.TTF, 0o644); err != nil {
			t.Fatalf("write into %s: %v", dir, err)
		}
	}

	// Given in the opposite order to the one they sort in.
	fams := mustScan(t, []string{filepath.Join(base, "b"), filepath.Join(base, "a")})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want one", len(fams))
	}
	want := filepath.Join(base, "a", "gomono.ttf")
	if got := fams[0].Src[Regular].Path; got != want {
		t.Errorf("regular came from %q, want %q: the answer depends on the order"+
			" the directories were given in", got, want)
	}
}

// TestFacesInReportsAFileItCannotRead checks the line between a disk
// that will not read and a font this program cannot use.
//
// The first is reported, because an unreadable font directory would
// otherwise look exactly like a machine with no fonts on it. The second
// is skipped in silence: a stock Windows install has font files sfnt
// cannot get a face out of, and naming them at every start would make
// the dialog worthless.
func TestFacesInReportsAFileItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.ttf")

	faces, err := facesIn(missing)

	if err == nil {
		t.Error("a file that cannot be opened was passed over in silence")
	}
	if faces != nil {
		t.Errorf("faces = %v, want none", faces)
	}

	// A file that reads and is not a font: nothing to offer, and nothing
	// to say.
	junk := filepath.Join(t.TempDir(), "junk.ttf")
	if err := os.WriteFile(junk, []byte("not a font"), 0o644); err != nil {
		t.Fatalf("write %s: %v", junk, err)
	}
	faces, err = facesIn(junk)
	if err != nil {
		t.Errorf("a font that will not parse was reported: %v", err)
	}
	if faces != nil {
		t.Errorf("faces = %v, want none", faces)
	}

	// And a directory named like a font file, which is the shape a read
	// failure takes without an ACL to set.
	asDir := filepath.Join(t.TempDir(), "locked.ttf")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", asDir, err)
	}
	if _, err := facesIn(asDir); err == nil {
		t.Error("a font file that could not be read was passed over in silence")
	}
}

// TestScanIsQuietAboutADirectoryThatIsNotThere checks the other half of
// that line. Every platform lists font directories a given machine
// simply does not have, and reporting each one would bury a real
// failure in noise.
func TestScanIsQuietAboutADirectoryThatIsNotThere(t *testing.T) {
	good := fontDir(t, goMonoFiles())
	absent := filepath.Join(t.TempDir(), "no-such-fonts")

	fams, err := monospacedIn([]string{absent, good})

	if err != nil {
		t.Errorf("a font directory that does not exist was reported: %v", err)
	}
	if len(fams) != 1 {
		t.Errorf("found %d families, want the one that is there", len(fams))
	}
}

func TestScanWalksSubdirectories(t *testing.T) {
	dir := fontDir(t, map[string][]byte{
		filepath.Join("vendor", "go", "gomono.ttf"): gomono.TTF,
	})

	fams := mustScan(t, []string{dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want one below a subdirectory", len(fams))
	}
}

// TestScanCountsAFileOnce checks two directories holding the same file,
// which is what a user font directory shadowing a system one looks like.
func TestScanCountsAFileOnce(t *testing.T) {
	dir := fontDir(t, goMonoFiles())

	fams := mustScan(t, []string{dir, dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want one", len(fams))
	}
	if got := len(fams[0].Styles()); got != 4 {
		t.Errorf("%d styles, want 4", got)
	}
}

// TestScanIgnoresAMissingDirectory checks a system without one of the
// places fonts usually live.
func TestScanIgnoresAMissingDirectory(t *testing.T) {
	dir := fontDir(t, goMonoFiles())

	fams := mustScan(t, []string{filepath.Join(dir, "nope"), dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want the one that is there", len(fams))
	}
}

// TestScanDropsAFamilyWithNoRegular checks a family of only a bold file.
// Every other style borrows regular, so without one the atlas has
// nothing to build from.
func TestScanDropsAFamilyWithNoRegular(t *testing.T) {
	dir := fontDir(t, map[string][]byte{"gomonobold.ttf": gomonobold.TTF})

	fams := mustScan(t, []string{dir})

	if len(fams) != 0 {
		t.Errorf("found %+v, want nothing offered without a regular style", fams)
	}
}

// TestScanSortsByNameIgnoringCase checks the order a menu shows: sorted
// on the raw bytes, every lowercase name would come after every
// uppercase one.
func TestScanSortsByNameIgnoringCase(t *testing.T) {
	fams := []Family{{Name: "Zed"}, {Name: "apple"}, {Name: "Banana"}}

	sortFamilies(fams)

	want := []string{"apple", "Banana", "Zed"}
	for i, f := range fams {
		if f.Name != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, f.Name, want[i])
		}
	}
}

func TestFamilyLoadReadsEveryStyle(t *testing.T) {
	dir := fontDir(t, goMonoFiles())
	fams := mustScan(t, []string{dir})
	if len(fams) != 1 {
		t.Fatalf("found %d families, want one", len(fams))
	}

	fonts, err := fams[0].Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for name, b := range map[string][]byte{
		"regular":     fonts.Regular,
		"bold":        fonts.Bold,
		"italic":      fonts.Italic,
		"bold italic": fonts.BoldItalic,
	} {
		if len(b) == 0 {
			t.Errorf("%s style came back empty", name)
		}
	}
	// And the bytes actually build faces.
	if _, _, err := buildFaces(fonts, 12, 96); err != nil {
		t.Errorf("build faces from the loaded family: %v", err)
	}
}

// TestFamilyLoadReportsAMissingFile checks the rule this codebase holds
// about disk errors: the read failure is returned, not swallowed and
// worked around with whatever styles did load.
func TestFamilyLoadReportsAMissingFile(t *testing.T) {
	f := Family{Name: "Nowhere"}
	f.Src[Regular] = Source{Path: filepath.Join(t.TempDir(), "gone.ttf")}

	if _, err := f.Load(); err == nil {
		t.Error("loading a family whose file is gone reported no error")
	}
}

func TestFamilyLoadRefusesAFamilyWithNoRegular(t *testing.T) {
	dir := fontDir(t, map[string][]byte{"gomonobold.ttf": gomonobold.TTF})
	f := Family{Name: "Half"}
	f.Src[Bold] = Source{Path: filepath.Join(dir, "gomonobold.ttf")}

	if _, err := f.Load(); err == nil {
		t.Error("a family with no regular style loaded")
	}
}

func TestFamilyHasAndStyles(t *testing.T) {
	var f Family
	f.Src[Regular] = Source{Path: "r.ttf"}
	f.Src[Italic] = Source{Path: "i.ttf"}

	if !f.Has(Regular) || !f.Has(Italic) {
		t.Error("a style with a file reported missing")
	}
	if f.Has(Bold) || f.Has(BoldItalic) {
		t.Error("a style with no file reported present")
	}
	if f.Has(numStyles) {
		t.Error("a style that is not one reported present")
	}
	got := f.Styles()
	if len(got) != 2 || got[0] != Regular || got[1] != Italic {
		t.Errorf("Styles = %v, want [Regular Italic]", got)
	}
}

func TestStyleOf(t *testing.T) {
	for _, tc := range []struct {
		in    string
		want  Style
		valid bool
	}{
		{"Regular", Regular, true},
		{"regular", Regular, true},
		{"Book", Regular, true},
		{"Roman", Regular, true},
		{"", Regular, true},
		{"Bold", Bold, true},
		{"BOLD", Bold, true},
		{"Italic", Italic, true},
		{"Oblique", Italic, true},
		{"Bold Italic", BoldItalic, true},
		{"BoldItalic", BoldItalic, true},
		{"Bold-Oblique", BoldItalic, true},
		{"Italic Bold", BoldItalic, true},
		// A weight is a family of its own, not a style of this one.
		{"Light", 0, false},
		{"SemiBold", 0, false},
		{"Medium", 0, false},
		{"ExtraBold Italic", 0, false},
		{"Condensed", 0, false},
	} {
		got, ok := styleOf(tc.in)
		if ok != tc.valid {
			t.Errorf("styleOf(%q) accepted = %v, want %v", tc.in, ok, tc.valid)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("styleOf(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestIsFontFile(t *testing.T) {
	for name, want := range map[string]bool{
		"a.ttf": true, "a.TTF": true, "a.otf": true,
		"a.ttc": true, "a.otc": true, "a.dfont": true,
		"a.txt": false, "a.ttf.bak": false, "ttf": false, "": false,
	} {
		if got := isFontFile(name); got != want {
			t.Errorf("isFontFile(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestFontsIndexReachesTheFace checks that the collection index is used
// rather than ignored. An index past the end of a file holding one font
// must be an error, not silently the only font there is.
func TestFontsIndexReachesTheFace(t *testing.T) {
	fonts := Fonts{Regular: gomono.TTF}
	fonts.Index[Regular] = 3

	if _, _, err := buildFaces(fonts, 12, 96); err == nil {
		t.Error("an index past the end of the file was accepted")
	}

	fonts.Index[Regular] = 0
	if _, _, err := buildFaces(fonts, 12, 96); err != nil {
		t.Errorf("index 0 of an ordinary font file: %v", err)
	}
}

// TestMonospacedUsesTheSystemDirectories checks the exported entry point
// runs against the real system without reporting anything. What it finds
// depends on the machine, so the other claim is only that every family
// it does offer is usable as a terminal font.
//
// Nothing reported is the point: the font files a real machine has that
// this library cannot get a face out of are skipped in silence, and a
// failure here means something was read as a disk failing when it was
// not.
func TestMonospacedUsesTheSystemDirectories(t *testing.T) {
	if testing.Short() {
		t.Skip("reads every font file on the system")
	}
	families, err := Monospaced()
	if err != nil {
		t.Fatalf("scan the system fonts: %v", err)
	}
	if len(families) == 0 {
		t.Skip("no monospace fonts on this machine")
	}
	for _, f := range families {
		if f.Name == "" {
			t.Error("a family has no name")
		}
		if !f.Has(Regular) {
			t.Errorf("family %q has no regular style", f.Name)
		}
		for _, s := range f.Styles() {
			if f.Src[s].Path == "" {
				t.Errorf("family %q style %d has no file", f.Name, s)
			}
			if f.Src[s].Index < 0 {
				t.Errorf("family %q style %d has index %d", f.Name, s, f.Src[s].Index)
			}
		}
	}
}
