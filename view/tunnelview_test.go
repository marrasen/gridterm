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
		form, ok := formOf(win.dialog.Body), formOf(win.dialog.Body) != nil
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

// Disconnecting a machine beyond another window asks first, saying
// whose connection it closes.
func TestDisconnectingAFarMachineAsksFirst(t *testing.T) {
	win, _, publish := windowStage(t)
	far := machines.FarID("desk", "k1")
	publish(app.State{Windows: []app.RemoteWindow{{Name: "desk"}}, Machines: []machines.Info{{ID: "desk", Name: "laptop"}, {ID: far, Name: "db"}}})
	win.confirmFarDisconnect(far, lastUI)
	if win.dialog == nil || win.dialog.Title != "Disconnect db on laptop?" || !win.dialog.Danger {
		t.Fatalf("the question is %+v", win.dialog)
	}
	if in, ok := win.dialog.Accept.(app.Disconnect); !ok || in.Machine != far {
		t.Fatalf("yes sends %#v", win.dialog.Accept)
	}
}

// A machine beyond a window offers the folders that window saved for it.
func TestAFarMachineOffersTheFoldersItsWindowSaved(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Windows: []app.RemoteWindow{{Name: "desk", Folders: map[string][]string{"k1": {"/var/log"}}}}})
	if got := win.foldersOn(machines.FarID("desk", "k1")); !slices.Equal(got, []string{"/var/log"}) {
		t.Fatalf("the far machine offers %q", got)
	}
	if got := win.foldersOn(machines.FarID("desk", "k2")); len(got) != 0 {
		t.Fatalf("one the window saved nothing for offers %q", got)
	}
}
