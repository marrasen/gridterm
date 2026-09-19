package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// aFollowedReader opens a reader on a file, waits for the first read and
// turns following on.
func aFollowedReader(t *testing.T, a *testApp, path, name string) *files.Reader {
	t.Helper()
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	r.Follow(true)
	return r
}

// followRound moves the clock on far enough for the next question, asks
// it, and waits for the answer.
func followRound(t *testing.T, a *testApp, r *files.Reader, at *time.Time) {
	t.Helper()
	*at = at.Add(followEvery)
	a.followReaders(*at)
	if !a.readers[r].checking {
		t.Fatal("no question went out")
	}
	waitUntil(t, "the question to come back", func() bool {
		a.pump.run()
		return !a.readers[r].checking
	})
}

// settled waits for a read to come back.
func settled(t *testing.T, a *testApp, r *files.Reader) {
	t.Helper()
	waitUntil(t, "the read to come back", func() bool {
		a.pump.run()
		return !r.Busy()
	})
}

// A file that can no longer be asked about says so on the pane.
//
// Nothing else would ever say it: a reader that keeps failing the
// question never issues another read, so without this the pane goes on
// showing a file that has been taken away as though it were live.
func TestAFollowedFileThatCannotBeAskedAboutSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	r := aFollowedReader(t, a, path, name)

	if err := os.Remove(path); err != nil {
		t.Fatalf("delete the file: %v", err)
	}
	at := time.Now()
	followRound(t, a, r, &at)

	if r.Err() == nil {
		t.Fatal("the file was deleted and the reader still says all is well")
	}
	if got := a.readerNote(r); !strings.Contains(got, "cannot find") &&
		!strings.Contains(got, "no such file") {
		t.Errorf("the sidebar row says %q, want it to say the file has gone", got)
	}
}

// A read that failed is tried again on the next round, although the file
// has not changed since: one blip would otherwise leave the error on the
// pane until somebody wrote to the file.
func TestAFailedReadIsTriedAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	r := aFollowedReader(t, a, path, name)
	at := time.Now()
	followRound(t, a, r, &at)
	settled(t, a, r)

	reads := 0
	r.Read = func(then func([]string, bool, error)) {
		reads++
		then(nil, false, errors.New("the connection went"))
	}
	// The file grows, so the first round reads and the read fails.
	if err := os.WriteFile(path, []byte("one\ntwo\nthree"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	followRound(t, a, r, &at)
	if reads != 1 {
		t.Fatalf("the file grew and was read %d times, want once", reads)
	}

	// Nothing writes to it now, and it is read again all the same.
	followRound(t, a, r, &at)

	if reads != 2 {
		t.Errorf("a reader showing an error read the file %d times, want it to try again", reads)
	}
}

// A change is not forgotten when a read starts while the question is
// still out.
//
// The window used to write down the size it had just seen before asking
// for the read, so a read dropped because one was already running left
// it thinking it had read that version.
func TestAChangeIsNotForgottenWhileAReadIsOut(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	r := aFollowedReader(t, a, path, name)
	at := time.Now()
	followRound(t, a, r, &at)
	settled(t, a, r)

	// The file grows, and the question about it goes out.
	if err := os.WriteFile(path, []byte("one\ntwo\nthree"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	at = at.Add(followEvery)
	a.followReaders(at)

	// The user asks for a reread before the answer lands, and that read
	// does not answer. The follow's own read is dropped.
	var answer func([]string, bool, error)
	reads := 0
	r.Read = func(then func([]string, bool, error)) {
		reads++
		answer = then
	}
	if !r.Open() {
		t.Fatal("the reread did not go out")
	}
	waitUntil(t, "the question to come back", func() bool {
		a.pump.run()
		return !a.readers[r].checking
	})
	if reads != 1 {
		t.Fatalf("%d reads went out while one was already running", reads)
	}

	// The reread answers with what the file held before it grew, and the
	// change the dropped read missed is picked up on the next round.
	answer([]string{"one", "two"}, false, nil)
	followRound(t, a, r, &at)

	if reads != 2 {
		t.Errorf("the file grew while a read was out and was read %d times, want it read again", reads)
	}
}
