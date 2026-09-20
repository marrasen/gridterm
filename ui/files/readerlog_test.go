package files

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/ui"
)

// aJSONLog is a handful of lines the way a logger writes them.
func aJSONLog() []string {
	return []string{
		`{"time":"2026-09-20T15:22:04Z","level":"info","msg":"listening","addr":":5173"}`,
		`{"time":"2026-09-20T15:22:05Z","level":"warn","msg":"slow read","ms":812}`,
		`{"time":"2026-09-20T15:22:06Z","level":"error","msg":"could not open the file","path":"/tmp/x"}`,
		`{"time":"2026-09-20T15:22:07Z","level":"debug","msg":"cache hit"}`,
	}
}

// A file of JSON lines is read as a log.
func TestAFileOfJSONLinesIsALog(t *testing.T) {
	if !LooksLikeALog("app.log", aJSONLog()) {
		t.Error("a file of JSON log lines was not taken for a log")
	}
	// And a name that says so is enough on its own.
	if !LooksLikeALog("app.jsonl", nil) {
		t.Error("a .jsonl file was not taken for a log")
	}
}

// And what is not one is not one, however much punctuation it has.
func TestWhatIsNotALogIsNotOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
	}{
		{"main.go", []string{"package main", "", "func main() {}"}},
		{"notes.txt", []string{"one", "two", "three"}},
		// One object is a configuration file, not a log.
		{"config.json", []string{"{", `  "a": 1`, "}"}},
		{"empty.log", nil},
	} {
		if LooksLikeALog(tc.name, tc.lines) {
			t.Errorf("%s was taken for a log", tc.name)
		}
	}
}

// A log line is laid out in columns: the time, the level, the message,
// and everything else after it.
func TestALogLineIsLaidOutInColumns(t *testing.T) {
	got, mark := logRow(aJSONLog()[2])

	if !strings.HasPrefix(got, "15:22:06") {
		t.Errorf("it starts %q, want the time", got[:min(len(got), 12)])
	}
	if !strings.Contains(got, "ERROR") {
		t.Errorf("the level is not in %q", got)
	}
	if !strings.Contains(got, "could not open the file") {
		t.Errorf("the message is not in %q", got)
	}
	if !strings.Contains(got, "path=/tmp/x") {
		t.Errorf("the rest of the fields are not in %q", got)
	}
	if mark.sev != colourBad {
		t.Errorf("an error came out as colour %d", mark.sev)
	}
	if mark.fields < 0 || !strings.HasPrefix(got[mark.fields:], "path=") {
		t.Errorf("the fields start at %d, which is %q", mark.fields, got[max(int(mark.fields), 0):])
	}
}

// Every line starts its message in the same column, because a log is
// read by running an eye down one column at a time.
func TestEveryMessageStartsInTheSameColumn(t *testing.T) {
	for _, line := range aJSONLog() {
		got, _ := logRow(line)
		if len(got) <= logMsgAt {
			t.Fatalf("%q is shorter than the columns", got)
		}
		if got[logMsgAt-1] != ' ' {
			t.Errorf("%q has no gap before the message", got)
		}
		if got[logMsgAt] == ' ' {
			t.Errorf("%q starts its message late", got)
		}
	}
}

// How loud a line was is the same scale the strip beside the file
// reads, so the worst line in a band is the highest colour in it.
func TestHowLoudALineWasIsAScale(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  colour
	}{
		{"", colourPlain},
		{"TRACE", colourNote},
		{"DEBUG", colourNote},
		{"INFO", colourText},
		{"NOTICE", colourText},
		{"WARN", colourMark},
		{"WARNING", colourMark},
		{"ERROR", colourBad},
		{"FATAL", colourBad},
		{"PANIC", colourBad},
	} {
		if got := logColour(tc.level); got != tc.want {
			t.Errorf("%q came out as %d, want %d", tc.level, got, tc.want)
		}
	}
	if logColour("DEBUG") >= logColour("WARN") || logColour("WARN") >= logColour("ERROR") {
		t.Error("the levels do not read loudest last")
	}
}

// A time is read however the logger wrote it, and what cannot be read
// keeps its last eight characters rather than being thrown away.
func TestATimeIsReadHoweverItWasWritten(t *testing.T) {
	for _, tc := range []struct {
		sent any
		want string
	}{
		{"2026-09-20T15:22:04Z", "15:22:04"},
		{"2026-09-20T15:22:04.123456Z", "15:22:04"},
		{"2026-09-20 15:22:04", "15:22:04"},
		{"whenever", "whenever"},
		{nil, ""},
	} {
		if got := logClock(tc.sent); got != tc.want {
			t.Errorf("%v came out as %q, want %q", tc.sent, got, tc.want)
		}
	}
}

// A line that is not an object is left as it is. A log file often has
// a plain line at the top of it.
func TestALineThatIsNotAnObjectIsLeftAlone(t *testing.T) {
	got, mark := logRow("starting up")

	if got != "starting up" {
		t.Errorf("it came out as %q", got)
	}
	if mark.sev != colourPlain || mark.fields != -1 {
		t.Errorf("it was marked %+v", mark)
	}
}

// The fields come out in one order whatever order the JSON had them
// in, because a column that moved between lines is worse than none.
func TestTheFieldsComeOutInOneOrder(t *testing.T) {
	one, _ := logRow(`{"msg":"x","b":2,"a":1,"c":3}`)
	two, _ := logRow(`{"msg":"x","c":3,"a":1,"b":2}`)

	if one != two {
		t.Errorf("the same line came out two ways:\n%q\n%q", one, two)
	}
	if !strings.Contains(one, "a=1 b=2 c=3") {
		t.Errorf("the fields came out as %q", one)
	}
}

// The view turns itself on for a log and the key turns it off, which
// puts the JSON back.
func TestTheLogViewTurnsItselfOnAndOff(t *testing.T) {
	r := NewReader("app.log", "/tmp/app.log")
	r.Read = func(then func([]string, bool, error)) { then(aJSONLog(), false, nil) }
	r.Layout(ui.Size{Cols: 80, Rows: 20})

	r.Open()

	if !r.IsLog() {
		t.Fatal("the file was not taken for a log")
	}
	if !r.Logged() {
		t.Fatal("the view did not turn itself on")
	}
	if got := r.shown[0]; !strings.HasPrefix(got, "15:22:04") {
		t.Errorf("the first line reads %q", got)
	}

	r.Log(false)

	if r.Logged() {
		t.Error("the view is still on")
	}
	if got := r.shown[0]; !strings.HasPrefix(got, "{") {
		t.Errorf("turning it off left %q rather than the JSON", got)
	}
}

// A very large log is left as the JSON it is: every line is laid out
// on the goroutine that draws.
func TestAVeryLargeLogIsLeftAsJSON(t *testing.T) {
	lines := make([]string, mostLogLines+1)
	for i := range lines {
		lines[i] = `{"level":"info","msg":"x"}`
	}

	if _, _, ok := asLog(lines); ok {
		t.Error("a log past the cap was laid out")
	}
}
