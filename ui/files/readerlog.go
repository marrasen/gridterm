package files

import (
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// The columns the log view lays out. Fixed, because a log is read by
// running an eye down one column at a time, and a level that starts in
// a different place on every line cannot be.
const (
	logTimeCols  = 8
	logLevelCols = 5
	logGap       = 2
	logMsgAt     = logTimeCols + logGap + logLevelCols + logGap
)

// logSniff is how many lines are looked at to decide whether a file is
// a log, and logNeeded how many of them have to be objects.
const (
	logSniff  = 20
	logNeeded = 4
)

// mostLogLines is the largest log the view will lay out. Every line is
// parsed on the goroutine that draws, so a file past this is left as
// the JSON it is rather than stopping the window for a second.
const mostLogLines = 200000

// logTimeKeys, logLevelKeys and logMsgKeys are what the field holding
// each thing is called, best first. Every logger spells them
// differently and none of them say which they used.
var (
	logTimeKeys  = []string{"time", "ts", "timestamp", "@timestamp", "t", "date"}
	logLevelKeys = []string{"level", "lvl", "severity", "levelname", "loglevel"}
	logMsgKeys   = []string{"msg", "message", "event", "text", "@message"}
)

// logMark is what the view worked out about one line: how loud it was,
// and where the fields after the message start.
type logMark struct {
	sev colour

	// fields is the byte the " key=value" tail begins at, and -1 for a
	// line with nothing after its message.
	fields int32
}

// logView is a file of JSON lines laid out as a log.
type logView struct{ marks []logMark }

// LooksLikeALog reports whether a file of lines is JSON a log wrote,
// so a caller can offer the view before anybody asks for it.
//
// The name is enough for the ones that say so. For everything else the
// first few lines are read: a file whose lines are JSON objects with
// something message-shaped in them is a log, whatever it is called.
func LooksLikeALog(name string, lines []string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jsonl", ".ndjson":
		return true
	}
	seen, good := 0, 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		seen++
		if _, ok := logFields(line); ok {
			good++
		}
		if seen >= logSniff {
			break
		}
	}
	return good >= logNeeded && good*5 >= seen*4
}

// logFields reads one line as a JSON object, and says whether it was
// one at all.
func logFields(line string) (map[string]any, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
		return nil, false
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		return nil, false
	}
	return got, len(got) > 0
}

// asLog lays a file of JSON lines out as a log, and reports whether it
// could.
func asLog(lines []string) ([]string, *logView, bool) {
	if len(lines) > mostLogLines {
		return nil, nil, false
	}
	out := make([]string, len(lines))
	view := &logView{marks: make([]logMark, len(lines))}
	for i, line := range lines {
		out[i], view.marks[i] = logRow(line)
	}
	return out, view, true
}

// logRow lays one line out, and leaves a line that is not an object as
// it is.
func logRow(line string) (string, logMark) {
	got, ok := logFields(line)
	if !ok {
		return line, logMark{fields: -1}
	}
	when := logClock(pull(got, logTimeKeys))
	level := strings.ToUpper(asText(pull(got, logLevelKeys)))
	msg := asText(pull(got, logMsgKeys))

	var b strings.Builder
	b.WriteString(pad(when, logTimeCols))
	b.WriteString(strings.Repeat(" ", logGap))
	b.WriteString(pad(grid.TrimTail(level, logLevelCols), logLevelCols))
	b.WriteString(strings.Repeat(" ", logGap))
	b.WriteString(msg)
	at := int32(-1)
	if rest := logRest(got); rest != "" {
		if msg != "" {
			b.WriteString("  ")
		}
		at = int32(b.Len())
		b.WriteString(rest)
	}
	return b.String(), logMark{sev: logColour(level), fields: at}
}

// logRest is everything the line said that the columns did not take,
// as "key=value", in the order the keys sort.
//
// Sorted, because a map has no order of its own and a column that
// moved between lines would be worse than no column at all.
func logRest(got map[string]any) string {
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, k+"="+asText(got[k]))
	}
	return strings.Join(out, " ")
}

// pull takes the first of these keys the line has, and takes it out so
// what is left is what the columns did not use.
func pull(got map[string]any, keys []string) any {
	for _, k := range keys {
		if v, have := got[k]; have {
			delete(got, k)
			return v
		}
	}
	return nil
}

// asText writes one value the way a log line reads.
func asText(v any) string {
	switch got := v.(type) {
	case nil:
		return ""
	case string:
		return got
	case bool:
		return strconv.FormatBool(got)
	case float64:
		return strconv.FormatFloat(got, 'f', -1, 64)
	}
	// An object or a list, back as the JSON it came from: a line that
	// carries one is carrying it for a reason.
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return ""
}

// logClock is the time of day a timestamp names, and empty for one
// nothing could read.
//
// The day is left out. A log is read a screenful at a time and every
// line on the screen is the same day; the seconds are what tells two
// lines apart.
func logClock(v any) string {
	switch got := v.(type) {
	case float64:
		// Seconds since the epoch, which is how a logger that writes a
		// number writes one.
		return time.Unix(int64(got), 0).Format("15:04:05")
	case string:
		for _, how := range []string{
			time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05",
			"2006-01-02 15:04:05.000", "2006-01-02 15:04:05",
		} {
			if at, err := time.Parse(how, got); err == nil {
				return at.Format("15:04:05")
			}
		}
		// Not a time this knows. The last eight characters are more
		// likely to be the clock than the first eight are.
		if len(got) >= logTimeCols {
			return got[len(got)-logTimeCols:]
		}
		return got
	}
	return ""
}

// logColour is how loud a level is, on the same scale the strip beside
// the file reads: the higher the colour, the worse the line.
func logColour(level string) colour {
	switch {
	case level == "":
		return colourPlain
	case strings.HasPrefix(level, "F"), strings.HasPrefix(level, "PANIC"),
		strings.HasPrefix(level, "CRIT"), strings.HasPrefix(level, "E"):
		return colourBad
	case strings.HasPrefix(level, "W"):
		return colourMark
	case strings.HasPrefix(level, "I"), strings.HasPrefix(level, "N"):
		return colourText
	}
	return colourNote
}

// pad puts a string in a column of its own width.
func pad(s string, cols int) string {
	if n := cols - grid.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// logRunsFor colours one line the log view wrote: the time dim, the
// level in what it means, the message plain and the fields after it
// dim.
func logRunsFor(line string, mark logMark, into []run) []run {
	out := into[:0]
	if mark.fields < 0 && len(line) <= logMsgAt {
		// A line the view left alone, or one with nothing but columns.
		if len(line) == 0 {
			return out
		}
	}
	out = append(out, run{end: min(logTimeCols, len(line)), col: colourNote})
	if len(line) <= logTimeCols {
		return out
	}
	lvl := min(logTimeCols+logGap+logLevelCols, len(line))
	out = append(out, run{end: lvl, col: mark.sev})
	if len(line) <= lvl {
		return out
	}
	msg := len(line)
	if mark.fields > 0 {
		msg = int(mark.fields)
	}
	out = append(out, run{end: min(msg, len(line)), col: colourPlain})
	if msg < len(line) {
		out = append(out, run{end: len(line), col: colourNote})
	}
	return out
}

// Log sets whether a file of JSON lines is laid out as a log.
func (r *Reader) Log(on bool) {
	if on == r.logOn {
		return
	}
	r.logOn = on
	r.top, r.left = 0, 0
	r.stuck = false
	// The lines are not the lines they were, so what was picked out of
	// them is not there any more.
	r.sel = span{}
	r.remake()
}

// Logged reports whether the log view is on.
func (r *Reader) Logged() bool { return r.logOn && r.log != nil }

// IsLog reports whether the file looks like a log, whether or not the
// view is on.
func (r *Reader) IsLog() bool { return r.isLog }

// LogKey is the key that turns the log view off and on. It is on the
// bar only for a file that looks like a log.
func LogKey(on bool) Key {
	title := "Log view"
	if on {
		title = "Raw JSON"
	}
	return Key{Chord: chord(input.KeyJ, input.ModCtrl), Shown: "^J", Title: title}
}

// MapKey is the key that turns the strip beside the file off and on.
func MapKey() Key {
	return Key{Chord: chord(input.KeyM, input.ModCtrl), Shown: "^M", Title: "Map"}
}

// paintLogLine writes one line of the log view, in the stretches the
// view gave it.
func (r *Reader) paintLogLine(v grid.View, y int, line string, mark logMark, cols int) {
	r.runs = r.snap(line, logRunsFor(line, mark, r.runs))
	if len(r.runs) == 0 {
		r.paintPlain(v, y, line, cols)
		return
	}
	at, x := 0, -r.left
	for _, piece := range r.runs {
		end := min(piece.end, len(line))
		if end < at {
			break
		}
		text := line[at:end]
		at = end
		width := grid.StringWidth(text)
		if x+width > 0 && x < cols {
			r.paintPiece(v, y, x, cols, text, piece)
		}
		x += width
		if x >= cols {
			return
		}
	}
}
