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
	publish(app.State{Machines: []machines.Info{{ID: "a1", Name: "srv"}, {ID: "quick-1", Name: "me@typed", Quick: true}}})
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
	} {
		if got := win.keptFor(c.host, c.id, c.machine); got != c.want {
			t.Errorf("kept on %q (%q), for %q: %v, want %v", c.host, c.id, c.machine, got, c.want)
		}
	}
}
