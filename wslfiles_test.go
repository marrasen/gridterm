package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
)

// The file browser can open a WSL distribution. Windows serves its
// files on a share, so nothing of gridterm's own is needed to read
// one, and the plus on this machine offers a line for each.
func TestTheBrowserOffersEachWSLDistribution(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	a.registerShells(testShells())
	a.refreshServers()

	folders := a.browseFoldersOn(conns.Local)
	if len(folders) != 1 || folders[0] != `\\wsl.localhost\Ubuntu` {
		t.Fatalf("this machine offers %v, want the one distribution", folders)
	}

	items := a.folderItems(conns.Local)
	if len(items) != 1 {
		t.Fatalf("%d lines on the plus menu, want one", len(items))
	}
	if !strings.Contains(items[0].Title, "Ubuntu") {
		t.Errorf("the line says %q", items[0].Title)
	}
	if _, ok := a.root.Commands.Lookup(items[0].Command); !ok {
		t.Error("the line names a command that is not registered")
	}
}

// A machine at the far end has no distributions of ours to offer: the
// shells that were found are this machine's.
func TestAServerIsOfferedNoDistributions(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	a.registerShells(testShells())

	if got := a.browseFoldersOn("margit"); len(got) != 0 {
		t.Errorf("margit was offered %v", got)
	}
}
