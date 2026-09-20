package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
)

// aSavedTunnel is one kept for a machine.
func aSavedTunnel(host string) settings.SavedTunnel {
	return settings.SavedTunnel{
		Host: host, Kind: "local", Listen: "127.0.0.1:5432", Target: "127.0.0.1:5432",
	}
}

// A command the user kept is something to run by name, not something
// to find by reopening the dialog that saved it.
func TestASavedCommandIsOnThePalette(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	if err := a.saved.keep(settings.SavedCommand{
		Line: "go test ./...", Dir: `G:\Workspace\gridterm`, Host: conns.Local,
	}); err != nil {
		t.Fatalf("keep it: %v", err)
	}

	a.refreshServers()

	cmd, ok := a.root.Commands.Lookup(savedCommandID(0))
	if !ok {
		t.Fatal("the saved command has no line on the palette")
	}
	if !strings.Contains(cmd.Title, "go test ./...") {
		t.Errorf("the line says %q", cmd.Title)
	}
}

// And a tunnel the user kept is too.
func TestASavedTunnelIsOnThePalette(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	if err := a.savedTuns.keep(aSavedTunnel("margit")); err != nil {
		t.Fatalf("keep it: %v", err)
	}

	a.refreshServers()

	cmd, ok := a.root.Commands.Lookup(savedTunnelID(0))
	if !ok {
		t.Fatal("the saved tunnel has no line on the palette")
	}
	if !strings.Contains(cmd.Title, "5432") || !strings.Contains(cmd.Title, "margit") {
		t.Errorf("the line says %q", cmd.Title)
	}
}

// A tunnel goes in and comes back out the same.
func TestASavedTunnelComesBackTheSame(t *testing.T) {
	a := newTestApp(t, 80, 24)
	want := remote.Tunnel{
		Kind: remote.RemoteForward, Listen: "0.0.0.0:8080", Target: "127.0.0.1:80",
	}

	if err := a.savedTuns.keep(asSaved("margit", want)); err != nil {
		t.Fatalf("keep it: %v", err)
	}

	all := a.savedTuns.all()
	if len(all) != 1 {
		t.Fatalf("%d tunnels kept", len(all))
	}
	got, err := asTunnel(all[0])
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if got != want {
		t.Errorf("it came back as %+v, want %+v", got, want)
	}
	if all[0].Host != "margit" {
		t.Errorf("it came back on %q", all[0].Host)
	}
}

// Keeping the same tunnel twice keeps one.
func TestKeepingTheSameTunnelTwiceKeepsOne(t *testing.T) {
	a := newTestApp(t, 80, 24)

	for range 3 {
		if err := a.savedTuns.keep(aSavedTunnel("margit")); err != nil {
			t.Fatalf("keep it: %v", err)
		}
	}

	if got := len(a.savedTuns.all()); got != 1 {
		t.Errorf("%d copies of one tunnel", got)
	}
}

// Unticking the box on one that was kept forgets it.
func TestUntickingForgetsASavedTunnel(t *testing.T) {
	a := newTestApp(t, 80, 24)
	t1 := remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:5432", Target: "127.0.0.1:5432"}
	if err := a.keepOrForgetTunnel(true, "margit", t1); err != nil {
		t.Fatalf("keep it: %v", err)
	}

	if err := a.keepOrForgetTunnel(false, "margit", t1); err != nil {
		t.Fatalf("forget it: %v", err)
	}

	if got := len(a.savedTuns.all()); got != 0 {
		t.Errorf("%d tunnels are still kept", got)
	}
}

// A tunnel kept in a shape this version does not know is said so
// rather than opened as something else.
func TestATunnelKeptInAnUnknownShapeIsRefused(t *testing.T) {
	a := newTestApp(t, 80, 24)

	err := a.openSavedTunnel(settings.SavedTunnel{Host: "margit", Kind: "sideways"})

	if err == nil {
		t.Fatal("it opened something")
	}
	if !strings.Contains(err.Error(), "sideways") {
		t.Errorf("it said %q", err)
	}
}

// Opening a saved tunnel over a machine nothing is connected to says
// so rather than failing quietly.
func TestASavedTunnelNeedsTheMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)

	err := a.openSavedTunnel(aSavedTunnel("margit"))

	if err == nil || !strings.Contains(err.Error(), "margit") {
		t.Errorf("it said %v", err)
	}
}

// A tunnel's row offers a cross to close it. It is the one thing the
// row can do, and there was nowhere else to press it.
func TestATunnelsRowOffersACross(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	e := &conns.Entry{Host: "margit", Kind: conns.Tunnel, Label: ":5432"}
	e.Close = func() error { return nil }
	a.registry.Add(e)

	pointAtRow(t, a, e)

	row, ok := panelRow(a, e)
	if !ok {
		t.Fatal("the tunnel has no row")
	}
	if row.HoverButton != clearButton {
		t.Errorf("the row offers %q under the pointer, want a cross", row.HoverButton)
	}
}

// The machine's own row does not. Closing it takes every pane, tunnel
// and browser riding on it, which is too much to lose to a cross under
// the pointer.
func TestTheMachinesRowDoesNotOfferACross(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	e := &conns.Entry{Host: "margit", Kind: conns.Server, Label: "margit"}
	e.Close = func() error { return nil }
	a.registry.Add(e)

	pointAtRow(t, a, e)

	row, ok := panelRow(a, e)
	if !ok {
		t.Fatal("the machine has no row")
	}
	if row.HoverButton != 0 {
		t.Errorf("the machine's row offers %q under the pointer", row.HoverButton)
	}
}
