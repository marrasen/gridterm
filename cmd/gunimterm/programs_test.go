package main

import (
	"testing"
	"time"
)

func TestAProgramsProgressAndMessageGoOnItsRow(t *testing.T) {
	a, _ := localPane(t, "\x1b]9;4;1;42\x07\x1b]9;build done\x07", "/bin/bash")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if text, _ := a.terminal("p1").Notice(); text != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the message never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.notePrograms()
	if got := a.st.Panes[0].Note; got != "42%, build done" {
		t.Fatalf("the pane's note is %q", got)
	}
	rows := sidebarRows(a.st.Panes, nil, Share{}, nil)
	if rows[1].note != "42%, build done" {
		t.Fatalf("the sidebar row says %q", rows[1].note)
	}
	// The message went up once; the same one again goes up no more.
	first := a.lastToast
	a.notePrograms()
	if a.lastToast != first {
		t.Fatal("the same message went up twice")
	}
}
