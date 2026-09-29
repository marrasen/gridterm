package serve

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/session"
)

// A window opens something new on a machine the other window reaches:
// the machine, the command and its folder cross as asked, and what the
// other window calls it comes back.
func TestAWindowOpensOnAMachineTheOtherReaches(t *testing.T) {
	type asked struct {
		host, command, dir string
		cols, rows         int
	}
	got := make(chan asked, 1)
	_, w := takenOverServing(t, Config{OpenOn: func(host, command, dir string, cols, rows int) (session.Session, Attached, error) {
		got <- asked{host, command, dir, cols, rows}
		return newEchoSession(cols, rows), Attached{ID: "p9", Host: "srv", Kind: "Terminal"}, nil
	}})
	named := make(chan Attached, 1)
	sess, err := w.OpenOn("srv", "make test", "/src", 90, 30, func(n Attached) { named <- n })
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	read(t, sess, "started at 90x30")
	if a := <-got; a != (asked{"srv", "make test", "/src", 90, 30}) {
		t.Fatalf("the other window was asked for %+v", a)
	}
	select {
	case n := <-named:
		if n != (Attached{ID: "p9", Host: "srv", Kind: "Terminal"}) {
			t.Fatalf("it was called %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the other window never said what it opened")
	}
}

// A window that opens nothing on the machines it reaches says so, and
// what it refused, in the pane.
func TestAWindowThatOpensNothingElsewhereSaysSo(t *testing.T) {
	_, w := takenOverServing(t, Config{OpenOn: func(string, string, string, int, int) (session.Session, Attached, error) {
		return nil, Attached{}, errors.New("srv is not connected here")
	}})
	sess, err := w.OpenOn("srv", "", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	read(t, sess, "srv is not connected here")
	if err := waited(t, sess); err == nil {
		t.Fatal("a session refused ended as if it ran")
	}

	_, none := takenOverServing(t, Config{})
	sess, err = none.OpenOn("srv", "", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	read(t, sess, "cannot open anything on the machines it reaches")
}

// statusError is a program's failure that says its exit status.
type statusError int

func (e statusError) Error() string   { return "exit status " + strconv.Itoa(int(e)) }
func (e statusError) ExitStatus() int { return int(e) }

// How a program ended crosses as its exit status, for the other window
// to say it as it would its own.
func TestAProgramsExitStatusCrosses(t *testing.T) {
	_, w := takenOverServing(t, Config{OpenOn: func(_, _, _ string, cols, rows int) (session.Session, Attached, error) {
		s := newEchoSession(cols, rows)
		s.endWith = statusError(3)
		return s, Attached{}, nil
	}})
	sess, err := w.OpenOn("", "false", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	read(t, sess, "started at")
	if _, err := sess.Write([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	var ended *ExitError
	if err := waited(t, sess); !errors.As(err, &ended) || ended.ExitStatus() != 3 {
		t.Fatalf("it ended with %v", err)
	}
}

// A window reads the log the other keeps for a machine it reaches; one
// it keeps none of is refused with its reason, before any pane opens.
func TestAWindowReadsTheOthersLog(t *testing.T) {
	_, w := takenOverServing(t, Config{Log: func(host string) (session.Session, error) {
		if host != "srv" {
			return nil, errors.New("this window keeps no connection log for " + strconv.Quote(host))
		}
		s := newEchoSession(80, 24)
		go func() { _, _ = s.outW.Write([]byte("connected in 20ms\r\n")) }()
		return s, nil
	}})
	sess, err := w.OpenLog("srv")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	read(t, sess, "connected in 20ms")
	if _, err := w.OpenLog("db"); err == nil || !strings.Contains(err.Error(), "keeps no connection log") {
		t.Fatalf("a log it keeps none of said %v", err)
	}
	_, none := takenOverServing(t, Config{})
	if _, err := none.OpenLog("srv"); err == nil || !strings.Contains(err.Error(), "does not show the logs") {
		t.Fatalf("with no logs to show, it said %v", err)
	}
}
