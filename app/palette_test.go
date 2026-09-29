package app

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// A failure is headed with what was being done.
func TestAFailureSaysWhatWasBeingDone(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.handle(PickTheme{Name: "No Such Theme"})
	if n := a.st.Notices; len(n) != 1 || n[0].Title != "Couldn't change the theme" {
		t.Fatalf("it said %+v", n)
	}
}
