package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/ui"
)

// asRelease makes this build call itself a release, so a test can be
// one without being built as one.
func asRelease(t *testing.T, version string) {
	t.Helper()
	was := thisVersion
	t.Cleanup(func() { thisVersion = was })
	thisVersion = func() string { return version }
}

// answering is GitHub answered by the test rather than over the
// network, and how many times it was asked.
func answering(t *testing.T, newest update.Release, err error) *atomic.Int64 {
	t.Helper()
	var asked atomic.Int64
	was := latestRelease
	t.Cleanup(func() { latestRelease = was })
	latestRelease = func(context.Context) (update.Release, error) {
		asked.Add(1)
		return newest, err
	}
	return &asked
}

// answered runs the pump until the check has put something up.
func answered(t *testing.T, a *testApp) {
	t.Helper()
	waitUntil(t, "the answer", func() bool {
		a.pump.run()
		return a.root.Modal() != nil
	})
}

// theNotice is the dialog on top, or a failure saying what is there
// instead.
func theNotice(t *testing.T, a *testApp) *ui.Notice {
	t.Helper()
	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("it opened %T, want a notice", a.root.Modal())
	}
	return n
}

// The about dialog says which build this is, because that is the first
// thing a bug report needs and nothing else in the window says it.
func TestAboutNamesTheBuild(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "v0.1.0")

	if err := a.showAbout(); err != nil {
		t.Fatalf("show it: %v", err)
	}

	n := theNotice(t, a)
	if !strings.Contains(n.Message(), "v0.1.0") {
		t.Errorf("it says %q, and not which build this is", n.Message())
	}
}

// And it offers to find out whether there is a newer one.
func TestAboutOffersToCheckForUpdates(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.showAbout(); err != nil {
		t.Fatalf("show it: %v", err)
	}

	n := theNotice(t, a)
	if n.Action.Title != btnCheckUpdates {
		t.Errorf("it offers %q, want %q", n.Action.Title, btnCheckUpdates)
	}
	if n.Action.Do == nil {
		t.Fatal("the button does nothing")
	}
}

// A newer release is named, with the page it is on, and that page is
// one press away.
func TestANewerReleaseIsOffered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "v0.1.0")
	const page = "https://github.com/marrasen/gridterm/releases/tag/v0.2.0"
	answering(t, update.Release{Version: "v0.2.0", Page: page}, nil)
	opened := ""
	was := openInBrowser
	t.Cleanup(func() { openInBrowser = was })
	openInBrowser = func(at string) error { opened = at; return nil }

	a.checkForUpdates()
	answered(t, a)

	n := theNotice(t, a)
	if n.Title != dlgUpdateAvailable {
		t.Errorf("it is titled %q, want %q", n.Title, dlgUpdateAvailable)
	}
	for _, want := range []string{"v0.2.0", "v0.1.0", page} {
		if !strings.Contains(n.Message(), want) {
			t.Errorf("it says %q, which does not name %q", n.Message(), want)
		}
	}
	if n.Action.Title != btnOpen {
		t.Fatalf("it offers %q, want %q", n.Action.Title, btnOpen)
	}

	n.Action.Do()

	if opened != page {
		t.Errorf("it opened %q, want the page of the release it named", opened)
	}
}

// A build that is the newest release is told so on the bottom row. A
// dialog whose only answer is OK costs a keypress and gives nothing for
// it.
func TestTheNewestReleaseIsSaidOnTheBottomRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "v0.2.0")
	answering(t, update.Release{Version: "v0.2.0", Page: update.Releases}, nil)

	a.checkForUpdates()
	waitUntil(t, "the answer", func() bool {
		a.pump.run()
		return strings.Contains(a.saying(), "v0.2.0")
	})

	if a.root.Modal() != nil {
		t.Errorf("it put up %T as well", a.root.Modal())
	}
	if got := a.saying(); got != "v0.2.0"+msgNewestRelease {
		t.Errorf("the row says %q", got)
	}
}

// And so is a build later than the newest release, which is what
// building from main gives.
func TestABuildLaterThanTheReleaseIsSaidOnTheBottomRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "v0.3.0")
	answering(t, update.Release{Version: "v0.2.0", Page: update.Releases}, nil)

	a.checkForUpdates()
	waitUntil(t, "the answer", func() bool {
		a.pump.run()
		return strings.Contains(a.saying(), "v0.2.0")
	})

	if a.root.Modal() != nil {
		t.Errorf("it put up %T as well", a.root.Modal())
	}
	if got := a.saying(); got != msgLaterThanNewest+"v0.2.0" {
		t.Errorf("the row says %q", got)
	}
}

// A build from a working tree has no order against a release: both
// versions are named and the choice is left to whoever is reading.
func TestABuildFromAWorkingTreeNamesBothVersions(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "dev-3e62f4547e66")
	answering(t, update.Release{Version: "v0.2.0", Page: update.Releases}, nil)

	a.checkForUpdates()
	answered(t, a)

	n := theNotice(t, a)
	if n.Title != dlgNewestRelease {
		t.Errorf("it is titled %q, want %q", n.Title, dlgNewestRelease)
	}
	for _, want := range []string{"v0.2.0", "dev-3e62f4547e66"} {
		if !strings.Contains(n.Message(), want) {
			t.Errorf("it says %q, which does not name %q", n.Message(), want)
		}
	}
	if n.Action.Title != btnOpen {
		t.Errorf("it offers %q, want %q", n.Action.Title, btnOpen)
	}
}

// A check that did not get through says so, in red, rather than leaving
// the button looking like it did nothing.
func TestACheckThatFailedIsReported(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	answering(t, update.Release{}, errors.New("no route to host"))

	a.checkForUpdates()
	answered(t, a)

	n := theNotice(t, a)
	if !n.Failure {
		t.Errorf("%q is not shown as a failure", n.Title)
	}
	if !strings.Contains(n.Message(), "no route to host") {
		t.Errorf("it says %q, and not what went wrong", n.Message())
	}
}

// The button pressed again while the answer is on its way asks once and
// puts up one dialog.
func TestCheckingTwiceAsksOnce(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	asRelease(t, "v0.2.0")
	var asked atomic.Int64
	held := make(chan struct{})
	was := latestRelease
	t.Cleanup(func() { latestRelease = was })
	latestRelease = func(context.Context) (update.Release, error) {
		asked.Add(1)
		<-held
		return update.Release{Version: "v0.2.0", Page: update.Releases}, nil
	}

	a.checkForUpdates()
	a.checkForUpdates()
	close(held)
	waitUntil(t, "the answer", func() bool {
		a.pump.run()
		return strings.Contains(a.saying(), "v0.2.0")
	})

	if got := asked.Load(); got != 1 {
		t.Errorf("GitHub was asked %d times, want once", got)
	}
	// And the next press asks again, now that the first one is answered.
	a.checkForUpdates()
	waitUntil(t, "the second answer", func() bool {
		a.pump.run()
		return asked.Load() == 2
	})
}
