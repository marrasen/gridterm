package screen

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/marrasen/kakel/meter"
)

// fakeSession reads what it holds and takes what is written.
type fakeSession struct {
	io.Reader
	late func(error)
}

func (f *fakeSession) Write(b []byte) (int, error)   { return len(b), nil }
func (f *fakeSession) Close() error                  { return nil }
func (f *fakeSession) Resize(int, int) error         { return nil }
func (f *fakeSession) Wait() error                   { return nil }
func (f *fakeSession) ReportLate(report func(error)) { f.late = report }

// What crosses a session is counted, and a failure it reports late
// still reaches whoever asked for it.
func TestASessionIsCountedAndStillReportsLate(t *testing.T) {
	m := meter.New()
	f := &fakeSession{Reader: strings.NewReader("hello")}
	sess := counted(f, m)
	buf := make([]byte, 16)
	if _, err := sess.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if in, out := m.Totals(); in != 5 || out != 2 {
		t.Fatalf("counted %d in and %d out", in, out)
	}
	var got error
	sess.(interface{ ReportLate(func(error)) }).ReportLate(func(err error) { got = err })
	f.late(errors.New("resize failed"))
	if got == nil {
		t.Fatal("the late failure was lost")
	}
}
