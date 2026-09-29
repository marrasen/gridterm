package main

import (
	"strings"
	"sync"
	"testing"
)

// Two panes on stages of their own, jobs and a browser, with focus on
// one of them.
func twoPanes(focus string, jobs []Job) State {
	return State{
		Panes:    []Pane{{ID: "p1", Title: "Jobs", Kind: kindJobs}, {ID: "p2", Title: "gthome", Kind: kindFiles}},
		Stage:    &Box{Pane: focus},
		Focus:    focus,
		Sidebar:  true,
		Jobs:     jobs,
		Browsers: map[string]Browser{"p2": {Path: "/"}},
	}
}

// sizes records the sizes a shell is given.
type sizes struct {
	typed
	mu   sync.Mutex
	seen [][2]int
}

func (s *sizes) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, [2]int{cols, rows})
	return nil
}

func TestClearFinishedClosesEndedPanes(t *testing.T) {
	a, _ := agentApp(t)
	if err := a.open("", placement{}); err != nil {
		t.Fatal(err)
	}
	ended := a.st.Panes[0].ID
	shellEnds(t, a, ended, "0")
	a.handle(ClearFinished{})
	waitFor(t, a, "the ended pane to close", func() bool { return !a.has(ended) })
	if len(a.st.Panes) != 1 {
		t.Fatalf("cleared, the panes are %+v", a.st.Panes)
	}
}

// gridRow is a row of the reader's cells, as text.
func gridRow(rd *reader, y int) string {
	cols, _ := rd.g.Size()
	var b strings.Builder
	for x := range cols {
		if r := rd.g.At(x, y).Rune; r != 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
