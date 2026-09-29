package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
)

// The tunnel dialog offers the far end listening only over a
// connection of this window's own: through a kakel window, to its own
// machine or one beyond it, a tunnel listens here.
func TestATunnelThroughAWindowListensHere(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Connected: []machines.ID{"srv"}, Windows: []app.RemoteWindow{{Name: "desk"}}})
	directions := func(m machines.ID) bool {
		t.Helper()
		win.tunnelDialogOn(m, false, lastUI)
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
		form, ok := win.dialog.Body.(*widget.Form)
		if !ok {
			t.Fatalf("the dialog's body is a %T", win.dialog.Body)
		}
		return slices.ContainsFunc(form.Children(), func(c gunim.Node) bool {
			d, ok := c.(*widget.Dropdown)
			return ok && d.Label == "Direction"
		})
	}
	if !directions("srv") {
		t.Fatal("over a server's connection, there is no Direction")
	}
	for _, m := range []machines.ID{"desk", machines.FarID("desk", "db")} {
		if directions(m) {
			t.Fatalf("through the window, %q offers a Direction", m)
		}
	}
}
