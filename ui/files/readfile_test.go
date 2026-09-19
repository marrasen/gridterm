package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/vfs"
)

// fileOf writes a file on this machine and returns its path.
func fileOf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	return path
}

// The shapes a file's ending comes in all give the lines they look like.
func TestReadFileCountsTheLinesItLooksLike(t *testing.T) {
	for what, tc := range map[string]struct {
		body string
		want []string
	}{
		"empty":                  {"", nil},
		"one line, no ending":    {"one", []string{"one"}},
		"one line and an ending": {"one\n", []string{"one"}},
		"two lines":              {"one\ntwo", []string{"one", "two"}},
		"two and an ending":      {"one\ntwo\n", []string{"one", "two"}},
		"a blank in the middle":  {"one\n\ntwo\n", []string{"one", "", "two"}},
		"a blank at the end":     {"one\n\n", []string{"one", ""}},
		"windows endings":        {"one\r\ntwo\r\n", []string{"one", "two"}},
	} {
		got, cut, err := ReadFile(vfs.NewLocal(), fileOf(t, tc.body))
		if err != nil {
			t.Errorf("%s: %v", what, err)
			continue
		}
		if cut {
			t.Errorf("%s: it says the file was cut", what)
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: it read %q, want %q", what, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: line %d is %q, want %q", what, i, got[i], tc.want[i])
			}
		}
	}
}

// A file longer than the reader takes says so, and keeps what it read.
//
// It used to count the bytes it kept rather than the bytes that arrived,
// so a file of long lines was cut down to a fraction of itself and
// reported as whole.
func TestAFileLongerThanTheLimitSaysSo(t *testing.T) {
	// Lines long enough that keeping only part of each would lose the
	// count, and enough of them to go past the limit.
	line := strings.Repeat("x", 200<<10)
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	got, cut, err := ReadFile(vfs.NewLocal(), fileOf(t, b.String()))

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if !cut {
		t.Error("a file of 12MB was read as whole")
	}
	if len(got) == 0 {
		t.Fatal("it read nothing at all")
	}
}

// A line longer than the reader keeps whole is cut, and that counts as
// the file having been cut: what is on screen is not what is in the file.
func TestALineLongerThanTheLimitIsCutAndSaidSo(t *testing.T) {
	body := "short\n" + strings.Repeat("y", MostReadLine+1000) + "\nshort\n"

	got, cut, err := ReadFile(vfs.NewLocal(), fileOf(t, body))

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if !cut {
		t.Error("a line was cut and the file was reported whole")
	}
	if len(got) != 3 {
		t.Fatalf("it read %d lines, want the three in the file", len(got))
	}
	if len(got[1]) != MostReadLine {
		t.Errorf("the long line came out %d long, want it cut to %d", len(got[1]), MostReadLine)
	}
	// And the lines after it are still there, which is what counting the
	// bytes kept rather than the bytes read used to lose.
	if got[2] != "short" {
		t.Errorf("the line after the long one is %q, want the one in the file", got[2])
	}
}

// A file of exactly the limit fits, and is not reported as cut.
//
// The count used to add a byte for the ending of every line, including
// a last line that had none, so a file that fitted exactly lost its last
// line and was reported cut.
func TestAFileExactlyTheLimitFits(t *testing.T) {
	// Lines just under the length a line is kept to, so nothing is cut
	// for being a long line, adding up to exactly the limit.
	const line = 64<<10 - 1
	const lines = MostReadBytes / (line + 1)
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString(strings.Repeat("z", line))
		b.WriteByte('\n')
	}
	if b.Len() != MostReadBytes {
		t.Fatalf("the test wrote %d bytes, want exactly %d", b.Len(), MostReadBytes)
	}

	got, cut, err := ReadFile(vfs.NewLocal(), fileOf(t, b.String()))

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if cut {
		t.Error("a file of exactly the limit was reported as cut")
	}
	if len(got) != lines {
		t.Errorf("it read %d lines, want the %d in the file", len(got), lines)
	}
}

// And one byte more does not fit.
func TestAFileOneByteOverTheLimitIsCut(t *testing.T) {
	const line = 64<<10 - 1
	const lines = MostReadBytes / (line + 1)
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString(strings.Repeat("z", line))
		b.WriteByte('\n')
	}
	b.WriteByte('x')

	_, cut, err := ReadFile(vfs.NewLocal(), fileOf(t, b.String()))

	if err != nil {
		t.Fatalf("read it: %v", err)
	}
	if !cut {
		t.Error("a file one byte over the limit was reported as whole")
	}
}

// A file that is not there comes back as an error rather than as no
// lines, because a reader showing nothing looks like an empty file.
func TestAFileThatIsNotThereIsAnError(t *testing.T) {
	_, _, err := ReadFile(vfs.NewLocal(), filepath.Join(t.TempDir(), "nope"))

	if err == nil {
		t.Fatal("a file that is not there read as an empty one")
	}
}
