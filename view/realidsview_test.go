package view

import (
	"testing"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/machines"
)

// Something kept on a saved server is found in a dialog for it by its
// ID, and something kept on a quick connection by its address.
func TestKeptWorkIsFoundForItsMachine(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Machines: []machines.Info{{ID: "a1", Name: "srv"}, {ID: "quick-1", Name: "me@typed", Quick: true},
		{ID: machines.FarID("a1", "k1"), Name: "db"}, {ID: machines.FarID("quick-1", "k2"), Name: "db"}}})
	beyond := func(window, name string) string { return window + app.KeptFarSep + name }
	for _, c := range []struct {
		host, id string
		machine  machines.ID
		want     bool
	}{
		{"old name", "a1", "a1", true},
		// Kept before servers had IDs, by its name.
		{"srv", "", "a1", true},
		{"me@typed", "", "quick-1", true},
		{"", "", "", true},
		{"", "", "a1", false},
		// Beyond a window: that window, and its name for the machine.
		{beyond("srv", "db"), "a1", machines.FarID("a1", "k1"), true},
		{beyond("srv", "db"), "a1", "a1", false},
		{"srv", "a1", machines.FarID("a1", "k1"), false},
		{beyond("srv", "other"), "a1", machines.FarID("a1", "k1"), false},
		{beyond("me@typed", "db"), "", machines.FarID("quick-1", "k2"), true},
	} {
		if got := win.keptFor(c.host, c.id, c.machine); got != c.want {
			t.Errorf("kept on %q (%q), for %q: %v, want %v", c.host, c.id, c.machine, got, c.want)
		}
	}
}
