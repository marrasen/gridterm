package main

import (
	"testing"
	"time"

	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
)

// A question names its icon as Lucide does, so the names in askIcons
// must be the icons' own.
func TestAskIconsAreKeyedByTheirLucideNames(t *testing.T) {
	for name, ic := range askIcons {
		if ic.Name != name {
			t.Errorf("askIcons[%q] is %q", name, ic.Name)
		}
	}
}

// Every command in a menu or the palette shows an icon.
func TestEveryCommandInTheMenusAndThePaletteHasAnIcon(t *testing.T) {
	for _, m := range menus {
		for _, it := range m.items {
			if !it.caption && commandIcons[it.id] == nil {
				t.Errorf("%s → %s (%s) has no icon", m.title, it.title, it.id)
			}
		}
	}
	for _, c := range commands {
		if commandIcons[c.id] == nil {
			t.Errorf("the palette's %s (%s) has no icon", c.title, c.id)
		}
	}
}

// A failure shows as an error toast, with its icon.
func TestAFailureShowsAnErrorToast(t *testing.T) {
	_, _, publish := windowStage(t)
	st := twoPanes("p2", nil)
	st.Notices = []Notice{{ID: 1, Title: "Couldn't open the files on web1", Body: "no route to host", Kind: NoticeFailed}}
	publish(st)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	for _, op := range lastWindow.Offscreen().Ops() {
		if m, ok := op.(*paint.MaskOp); ok {
			if s, ok := m.Shape.(icon.Stroke); ok && s.Icon == icon.CircleAlert {
				return
			}
		}
	}
	t.Fatal("the failure's toast shows no error icon")
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
