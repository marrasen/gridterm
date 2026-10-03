//go:build windows

package conpty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// The test binary run with this set is an animation: it writes frames
// as fast as the console takes them, each a synchronized update, and
// each cell of frame n holds the letter frameLetter(n).
const animateEnv = "KAKEL_CONPTY_TEST_ANIMATE"

const (
	animCols, animRows, animFrames = 400, 90, 24
)

func frameLetter(n int) byte { return 'A' + byte(n%10) }

func TestMain(m *testing.M) {
	if os.Getenv(animateEnv) == "1" {
		animate()
		return
	}
	os.Exit(m.Run())
}

// animate writes the frames, as termflix does: both colours of every
// cell, about 1.2 MB a frame, back to back.
func animate() {
	out := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(out, &mode) == nil {
		_ = windows.SetConsoleMode(out, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	}
	var f bytes.Buffer
	for n := range animFrames {
		f.Reset()
		f.WriteString("\x1b[?2026h")
		for y := range animRows {
			fmt.Fprintf(&f, "\x1b[%d;1H", y+1)
			for x := range animCols {
				fmt.Fprintf(&f, "\x1b[38;2;%d;%d;160m\x1b[48;2;40;%d;%dm%c", (x+n)%256, x%256, y%256, (x*y)%256, frameLetter(n))
			}
		}
		f.WriteString("\x1b[?2026l")
		_, _ = os.Stdout.Write(f.Bytes())
	}
}

// runAnimation runs the animation in a console made with h and returns
// all it wrote.
func runAnimation(t *testing.T, h *host) []byte {
	t.Helper()
	c, err := newWith(h, animCols, animRows)
	if err != nil {
		t.Fatalf("make a console with %s: %v", h.name, err)
	}
	defer c.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	proc, err := c.Start(exe, []string{exe, "-test.run=^$"}, "", append(os.Environ(), animateEnv+"=1"))
	if err != nil {
		t.Fatalf("start the animation: %v", err)
	}
	got := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(c)
		got <- b
	}()
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = proc.Kill()
		t.Fatal("the animation did not end")
	}
	// Freed once the program is gone, the console writes what it has
	// left and breaks the pipe, which ends the read.
	c.Release()
	select {
	case b := <-got:
		return b
	case <-time.After(10 * time.Second):
		t.Fatal("the console's output never ended")
	}
	return nil
}

// tornFrames reports where the stream shows a frame other than whole:
// a synchronized update holding more or less than one frame's cells, or
// more than one frame's letter, or a frame's cells outside an update.
func tornFrames(out []byte) []string {
	esc := regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[^\[\]]`)
	update := regexp.MustCompile(`(?s)\x1b\[\?2026h(.*?)\x1b\[\?2026l`)
	letters := func(b []byte) map[byte]int {
		n := map[byte]int{}
		for _, c := range esc.ReplaceAll(b, nil) {
			if c >= 'A' && c <= 'J' {
				n[c]++
			}
		}
		return n
	}
	var torn []string
	outside := update.ReplaceAll(out, nil)
	if n := letters(outside); len(n) > 0 {
		torn = append(torn, fmt.Sprintf("cells of frames outside an update: %v", n))
	}
	updates := update.FindAllSubmatch(out, -1)
	whole := 0
	for i, u := range updates {
		n := letters(u[1])
		if len(n) == 0 {
			continue
		}
		var parts []string
		for c, k := range n {
			parts = append(parts, string(c)+"×"+strconv.Itoa(k))
		}
		if len(n) != 1 || n[frameLetter(whole)] != animCols*animRows {
			torn = append(torn, fmt.Sprintf("update %d holds %s, want %c×%d", i, strings.Join(parts, " "), frameLetter(whole), animCols*animRows))
		}
		whole++
	}
	if whole != animFrames {
		torn = append(torn, fmt.Sprintf("%d updates held frames, want %d", whole, animFrames))
	}
	return torn
}

// A full-screen animation's frames reach the terminal whole, each
// between the marks of its synchronized update, however fast it draws.
// Windows' own ConPTY repaints on a timer, so its frames come cut
// between repaints, and the marks wherever they fall.
func TestFramesComeThroughWhole(t *testing.T) {
	h, err := bundled()
	if err != nil {
		t.Skipf("no OpenConsole on this machine: %v", err)
	}
	if torn := tornFrames(runAnimation(t, h)); len(torn) > 0 {
		t.Fatalf("through %s, frames came torn:\n%s", h.name, strings.Join(torn[:min(len(torn), 8)], "\n"))
	}
}

// What kakel uses is OpenConsole, on the machines it carries it for.
func TestOpenConsoleIsChosen(t *testing.T) {
	if _, err := bundled(); err != nil {
		t.Skipf("no OpenConsole on this machine: %v", err)
	}
	if h := chosen(); h == system {
		t.Fatalf("consoles are made with %s", h.name)
	}
}

// A name set twice keeps its last value, whatever its case; a drive's
// current directory, a name beginning with =, is kept as it is; and
// SYSTEMROOT is there.
func TestTheEnvironmentBlock(t *testing.T) {
	block, err := envBlock([]string{"Path=a", "=C:=C:\\x", "=D:=D:\\y", "PATH=b", "junk"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(utf16.Decode(block)), "\x00"), "\x00")
	want := []string{"PATH=b", "=C:=C:\\x", "=D:=D:\\y", "SYSTEMROOT=" + os.Getenv("SYSTEMROOT")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the block holds %q, want %q", got, want)
	}
	if block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatal("the block does not end with two NULs")
	}
}
