package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
)

// aWindowThatCanServe is a window with its settings in a file the test owns,
// ready to serve on a port of its own.
func aWindowThatCanServe(t *testing.T) (*testApp, *settings.Settings) {
	t.Helper()
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	set, err := withSettings(t, a, filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	return a, set
}

// Serving is written down, so the next window knows to offer.
func TestServingIsWrittenDownForTheNextWindow(t *testing.T) {
	a, set := aWindowThatCanServe(t)

	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	if !set.ServeOn() {
		t.Error("the settings do not say the window was serving")
	}
}

// Stopping from the dialog is written down too, so the next window does
// not offer.
func TestStoppingFromTheDialogIsWrittenDown(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	if err := a.showServing(); err != nil {
		t.Fatalf("open the dialog: %v", err)
	}
	f := awaitModal(t, a, "the serving dialog", byTitle[*ui.Form](dlgServingWindow))
	pressButton(t, a, f, btnStopServing)

	if a.serving.on() {
		t.Fatal("the port is still open")
	}
	if set.ServeOn() {
		t.Error("the settings still say the window was serving")
	}
}

// Closing the window does not forget it: quitting is not the user
// saying they are done serving, and forgetting there would mean the
// offer never appeared.
func TestClosingTheWindowDoesNotForgetThatItServed(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	if err := a.stopServing(); err != nil {
		t.Fatalf("stop it the way closing does: %v", err)
	}

	if !set.ServeOn() {
		t.Error("closing the window forgot that it was serving")
	}
}

// A window that was serving offers to serve again, and says what it
// would open rather than opening it.
func TestAWindowThatWasServingOffersToServeAgain(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := set.PutServe(4242, settings.ReachHere); err != nil {
		t.Fatalf("remember the port: %v", err)
	}
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}

	a.offerToServeAgain()
	a.pump.run()

	f := awaitModal(t, a, "the offer", byTitle[*ui.Form](serveAgainTitle))
	if a.serving.on() {
		t.Error("it opened the port without being asked")
	}
	said := strings.Join(f.Lines, "\n")
	if !strings.Contains(said, "4242") {
		t.Errorf("it does not say which port:\n%s", said)
	}
	// Nothing says the port is still shut: the dialog is a question, and
	// a question that has not been answered has not done anything.
	if a.serving.on() {
		t.Error("the port is open before the question was answered")
	}
}

// Taking the offer opens the port.
func TestTakingTheOfferOpensThePort(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}
	a.offerToServeAgain()
	a.pump.run()
	f := awaitModal(t, a, "the offer", byTitle[*ui.Form](serveAgainTitle))

	pressButton(t, a, f, btnServe)

	if !a.serving.on() {
		t.Fatal("taking the offer did not open a port")
	}
}

// "Not now" leaves the port shut and the offer standing, so the next
// window asks again.
func TestNotNowLeavesTheOfferStanding(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}
	a.offerToServeAgain()
	a.pump.run()
	f := awaitModal(t, a, "the offer", byTitle[*ui.Form](serveAgainTitle))

	pressButton(t, a, f, btnNotNow)

	if a.serving.on() {
		t.Error("declining opened a port")
	}
	if !set.ServeOn() {
		t.Error("declining once stopped the next window asking")
	}
}

// "Don't ask again" leaves the port shut and stops the offer.
func TestForgettingStopsTheOffer(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}
	a.offerToServeAgain()
	a.pump.run()
	f := awaitModal(t, a, "the offer", byTitle[*ui.Form](serveAgainTitle))

	pressButton(t, a, f, btnDontAskAgain)

	if a.serving.on() {
		t.Error("it opened a port")
	}
	if set.ServeOn() {
		t.Error("the next window would still be asked")
	}
}

// A window that was not serving is not asked.
func TestAWindowThatWasNotServingIsNotAsked(t *testing.T) {
	a, _ := aWindowThatCanServe(t)

	a.offerToServeAgain()
	a.pump.run()

	if got := a.root.Modal(); got != nil {
		t.Errorf("it opened %T", got)
	}
}

// Nor is one that is serving already.
func TestAWindowAlreadyServingIsNotAsked(t *testing.T) {
	a, set := aWindowThatCanServe(t)
	if err := set.PutServeOn(true); err != nil {
		t.Fatalf("remember that it served: %v", err)
	}
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	a.offerToServeAgain()
	a.pump.run()

	if got := a.root.Modal(); got != nil {
		t.Errorf("it opened %T", got)
	}
}

// A port of zero is said in words, because zero means whichever one is
// free rather than port zero.
func TestAPortOfZeroIsSaidInWords(t *testing.T) {
	if got := servePortWords(0); !strings.Contains(got, "free") {
		t.Errorf("port 0 is said as %q", got)
	}
	if got := servePortWords(4242); got != "4242" {
		t.Errorf("port 4242 is said as %q", got)
	}
}
