package glyph

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
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

func findFamily(fams []Family, name string) (Family, bool) {
	for _, f := range fams {
		if f.Name == name {
			return f, true
		}
	}
	return Family{}, false
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
	for s := Style(0); s < numStyles; s++ {
		if !f.Has(s) {
			t.Errorf("style %d is missing, want all four in one family", s)
		}
	}
	// Each style must come from its own file, not all from one.
	seen := map[string]bool{}
	for s := Style(0); s < numStyles; s++ {
		if seen[f.Src[s].Path] {
			t.Errorf("style %d shares a file with another style", s)
		}
		seen[f.Src[s].Path] = true
	}
}

// TestScanRejectsAProportionalFont checks the whole point of the scan: a
// terminal drawn in a font whose letters are different widths has every
// column in the wrong place.
func TestScanRejectsAProportionalFont(t *testing.T) {
	dir := fontDir(t, map[string][]byte{
		"goregular.ttf": goregular.TTF,
		"gomono.ttf":    gomono.TTF,
	})

	fams := mustScan(t, []string{dir})

	if _, ok := findFamily(fams, "Go Regular"); ok {
		t.Error("a proportional family was offered")
	}
	if _, ok := findFamily(fams, "Go Mono"); !ok {
		t.Error("the monospace family was not found alongside it")
	}
}

// TestScanSkipsWhatItCannotRead checks that one bad file among many does
// not cost the user every font on the system.
func TestScanSkipsWhatItCannotRead(t *testing.T) {
	files := goMonoFiles()
	files["broken.ttf"] = []byte("this is not a font")
	files["empty.ttf"] = nil
	files["notes.txt"] = gomono.TTF // a font by content, not by name
	dir := fontDir(t, files)

	fams := mustScan(t, []string{dir})

	if len(fams) != 1 {
		t.Fatalf("found %d families, want the one good family: %+v", len(fams), fams)
	}
	if fams[0].Name != "Go Mono" {
		t.Errorf("family = %q, want %q", fams[0].Name, "Go Mono")
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
	if _, err := buildFaces(fonts, 12, 96); err != nil {
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

	if _, err := buildFaces(fonts, 12, 96); err == nil {
		t.Error("an index past the end of the file was accepted")
	}

	fonts.Index[Regular] = 0
	if _, err := buildFaces(fonts, 12, 96); err != nil {
		t.Errorf("index 0 of an ordinary font file: %v", err)
	}
}

// TestMonospacedUsesTheSystemDirectories checks the exported entry point
// runs against the real system without failing. What it finds depends on
// the machine, so the only claim is that every family it does offer is
// usable as a terminal font.
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
