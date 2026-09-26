package main

import (
	"strings"
	"testing"

	gi "github.com/marrasen/gunim/input"
)

func TestTheCommandLineIsReadAsGridtermReadsIt(t *testing.T) {
	o, err := parseOptions([]string{"-font-size", "18", "-e", "top -d 1", "-scrollback", "900", "-shot", "until:$ key:ctrl+k shot:a.png"})
	if err != nil {
		t.Fatal(err)
	}
	if o.fontSize != 18 || !o.sizeSet || o.command != "top -d 1" || o.scrollback != 900 {
		t.Fatalf("read %+v", o)
	}
	for _, bad := range [][]string{
		{"-scrollback", "-1"},
		{"-shot", "until"},
		{"-shot", "key:ctrl+nosuchkey"},
		{"-shot", "require:ok"},
	} {
		if _, err := parseOptions(bad); err == nil {
			t.Errorf("%q was taken", strings.Join(bad, " "))
		}
	}
}

func TestAChordIsPressedAsTheWindowHearsIt(t *testing.T) {
	press, err := chordPress("ctrl+shift+k")
	if err != nil {
		t.Fatal(err)
	}
	if press.Key != gi.KeyK || press.Mods != gi.ModControl|gi.ModShift {
		t.Fatalf("pressed %+v", press)
	}
}

func TestDashEOpensTheCommandInsteadOfAShell(t *testing.T) {
	a, _ := agentApp(t)
	for len(a.st.Panes) > 0 {
		a.remove(a.st.Panes[0].ID)
	}
	a.opts.command = "echo from-dash-e"
	if err := a.openFirst(); err != nil {
		t.Fatal(err)
	}
	if len(a.st.Panes) != 1 || !a.st.Panes[0].Command || a.st.Panes[0].Title != "echo from-dash-e" {
		t.Fatalf("the first pane is %+v", a.st.Panes)
	}
}
