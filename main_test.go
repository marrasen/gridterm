package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFonts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	regular := write("regular.ttf", "R")
	italic := write("italic.ttf", "I")

	t.Run("regular only", func(t *testing.T) {
		f, err := loadFonts(regular)
		if err != nil {
			t.Fatalf("loadFonts: %v", err)
		}
		if string(f.Regular) != "R" || f.Bold != nil || f.Italic != nil {
			t.Errorf("got %+v", f)
		}
	})

	t.Run("empty entry is skipped", func(t *testing.T) {
		f, err := loadFonts(regular + ",," + italic)
		if err != nil {
			t.Fatalf("loadFonts: %v", err)
		}
		if f.Bold != nil {
			t.Error("the empty bold entry loaded something")
		}
		if string(f.Italic) != "I" {
			t.Errorf("italic = %q, want %q", f.Italic, "I")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := loadFonts(filepath.Join(dir, "absent.ttf")); err == nil {
			t.Error("a missing file was accepted")
		}
	})

	t.Run("no regular", func(t *testing.T) {
		if _, err := loadFonts("," + italic); err == nil {
			t.Error("a list with no regular font was accepted")
		}
	})

	t.Run("too many files", func(t *testing.T) {
		if _, err := loadFonts(regular + ",a,b,c,d"); err == nil {
			t.Error("five files were accepted")
		}
	})
}

// -mcp-skill prints the skill for Claude Code, whole, so a user can save
// it themselves or pipe it somewhere.
func TestPrintSkillWritesTheClaudeCodeSkill(t *testing.T) {
	var out bytes.Buffer

	if err := printSkill(&out); err != nil {
		t.Fatalf("printSkill: %v", err)
	}

	got := out.String()
	if want := skillFor(hostNamed(hostClaudeCode), exeHere(t)); got != want {
		t.Errorf("it printed\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(got, "claude mcp add gridterm") {
		t.Errorf("it does not say how Claude Code adds the server:\n%s", got)
	}
	if strings.Contains(got, "codex mcp add") || strings.Contains(got, `{"mcpServers"`) {
		t.Errorf("it carries another host's setup:\n%s", got)
	}
}

// A write that fails is returned rather than being printed over.
func TestPrintSkillReportsAWriteThatFailed(t *testing.T) {
	if err := printSkill(brokenWriter{}); err == nil {
		t.Error("a failed write was reported as a print")
	}
}

// brokenWriter is a place nothing can be written to.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("the disk gave up") }
