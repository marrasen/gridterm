package view

import (
	"slices"
	"strings"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// The command dialog offers every saved command, this machine's
// first. One saved here fills its folder, over none typed; one saved
// elsewhere fills only the command, and is not ticked to be saved.
func TestTheCommandDialogOffersEverySavedCommand(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Saved: []remote.Host{{ID: "s1", Name: "srv", Address: "srv.example"}},
		SavedCommands: []settings.SavedCommand{
			{Line: "make deploy", Dir: "/srv/app", Host: "srv", HostID: "s1"},
			{Line: "top", Dir: "/home/me"},
		}})
	win.commandDialogOn("", lastUI)
	form := win.dialog.Body.(*widget.Form)
	var pick *widget.Dropdown
	var fields []*widget.TextField
	var keep *widget.Checkbox
	for _, f := range form.Children() {
		switch f := f.(type) {
		case *widget.Dropdown:
			pick = f
		case *widget.TextField:
			fields = append(fields, f)
		case *widget.Checkbox:
			keep = f
		}
	}
	if pick == nil || !slices.Equal(pick.Items, []string{"A new one", "top", "make deploy (on srv)"}) {
		t.Fatalf("the saved commands offered are %+v", pick)
	}
	line, dir := fields[0], fields[1]
	choose := func(i int) {
		t.Helper()
		lastUI.Focus(pick)
		press := func(k gi.Key) {
			lastWindow.Input(gi.KeyPress{Key: k, Time: time.Now()})
			lastWindow.Frame(time.Second / 60)
		}
		press(gi.KeyEnter)
		for range i - pick.Selected {
			press(gi.KeyDown)
		}
		for range pick.Selected - i {
			press(gi.KeyUp)
		}
		press(gi.KeyEnter)
		if pick.Selected != i {
			t.Fatalf("picked %d, want %d", pick.Selected, i)
		}
	}
	choose(1)
	if line.Text() != "top" || dir.Text() != "/home/me" || !keep.On {
		t.Fatalf("picked top, the dialog holds %q in %q, kept %v", line.Text(), dir.Text(), keep.On)
	}
	choose(2)
	if line.Text() != "make deploy" || dir.Text() != "/home/me" || keep.On {
		t.Fatalf("picked one from srv, the dialog holds %q in %q, kept %v", line.Text(), dir.Text(), keep.On)
	}
	// Unticked with the spacing changed, the pick is still forgotten.
	choose(1)
	line.SetText("  top ")
	keep.SetOn(false, lastUI)
	if in, ok := win.dialog.OnAccept().(app.RunCommand); !ok || in.Forget != "top" || !strings.Contains(in.Line, "top") {
		t.Fatalf("run, the dialog sent %+v", in)
	}
}
