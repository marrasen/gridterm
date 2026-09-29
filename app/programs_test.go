package app

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
	a.notePanes()
	if got := a.st.Panes[0].Note; got != "42%, build done" {
		t.Fatalf("the pane's note is %q", got)
	}
	// The message went up once; the same one again goes up no more.
	first := a.lastToast
	a.notePanes()
	if a.lastToast != first {
		t.Fatal("the same message went up twice")
	}
}

// The notices a failure and a success make are of their kinds.
func TestNoticesKeepTheirKinds(t *testing.T) {
	a := &app{}
	a.failed("Couldn't save", "disk full")
	a.worked("Saved web1", "tester@web1", "")
	a.notify("Disconnected from web1", "", "")
	want := []NoticeKind{NoticeFailed, NoticeWorked, NoticePlain}
	for i, n := range a.st.Notices {
		if n.Kind != want[i] {
			t.Errorf("notice %q is of kind %d, want %d", n.Title, n.Kind, want[i])
		}
	}
}
