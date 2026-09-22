package main

import (
	"strings"

	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/internal/update"
	"github.com/marrasen/gridterm/ui"
)

// What the check says along the bottom row, and in the dialog when
// there is a newer build to fetch. The version goes between the halves,
// the way a dialog title built from a name is made.
const (
	msgChecking        = "Checking for updates…"
	msgNewestRelease   = " is the newest release"
	msgThisBuild       = "; this build is "
	msgLaterThanNewest = "This build is later than the newest release, "
)

// The headings for the two ways this fails.
const (
	errCheckUpdates = "Could not check for updates"
	errOpenPage     = "Could not open the download page"
)

// latestRelease is which release of gridterm is the newest.
//
// A variable so a test can answer the question itself. Every other way
// of testing this asks GitHub from the machine running the tests.
var latestRelease = update.Latest

// thisVersion is what this build calls itself.
//
// A variable for the same reason: the tests run from a working tree, so
// every build they see is a development build, and a test about what a
// release says would have nothing to compare.
var thisVersion = build.Version

// checkForUpdates asks which release is the newest and says how this
// build compares to it.
//
// The asking is done away from the goroutine that draws: it is a round
// trip to a machine that may take ten seconds to answer, and the window
// goes on drawing and taking keys while it waits. The answer comes back
// through the pump, which is the only way it may reach the widget tree.
func (a *app) checkForUpdates() {
	if a.checking {
		// One question is already out, and it has one answer however
		// many times the button is pressed while it is on its way.
		return
	}
	a.checking = true
	a.say(msgChecking)
	ctx := a.ctx
	go func() {
		newest, err := latestRelease(ctx)
		a.pump.post(func() { a.sayWhatIsNewest(newest, err) })
	}()
}

// sayWhatIsNewest puts the answer in front of whoever pressed the
// button.
//
// A dialog only where there is something to do about it. The two
// answers that leave nothing to fetch go on the bottom row, over the
// about dialog they were asked from, because a dialog whose only reply
// is OK costs a keypress and gives nothing for it.
func (a *app) sayWhatIsNewest(newest update.Release, err error) {
	a.checking = false
	if err != nil {
		a.reportError(errCheckUpdates, err)
		return
	}
	have := thisVersion()
	switch update.Against(have, newest.Version) {
	case update.Behind:
		a.offerRelease(dlgUpdateAvailable, have, newest)
	case update.Current:
		a.say(newest.Version + msgNewestRelease)
	case update.Ahead:
		a.say(msgLaterThanNewest + newest.Version)
	default:
		// A build from a working tree, which has no order against a
		// release: it may hold work no release has. Both versions are
		// named and the choice is left to whoever is reading.
		a.offerRelease(dlgNewestRelease, have, newest)
	}
}

// offerRelease names both versions and the page the newer one is on.
func (a *app) offerRelease(title, have string, newest update.Release) {
	n := a.newNotice(title, strings.Join([]string{
		newest.Version + msgNewestRelease + msgThisBuild + have + ".",
		newest.Page,
	}, "\n"))
	// The address is one line and is not prose: wrapping it at a space
	// would break the only thing on the dialog worth copying.
	n.Preformatted = true
	n.Action = ui.NoticeAction{Title: btnOpen, Do: func() {
		if err := openInBrowser(newest.Page); err != nil {
			a.reportError(errOpenPage, err)
		}
	}}
	// Enter dismisses the dialog. Opening a browser is a thing to
	// choose, not a thing to land on.
	n.FocusOK()
	a.presentNotice(n)
}
