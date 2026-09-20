package main

import (
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/vfs"
)

// mostFarPaths is how many answers about a machine at the far end are
// kept. Past it nothing new is asked, because a pane printing endless
// path-like words would otherwise fill this with them.
const mostFarPaths = 2048

// pathsFar remembers what a machine at the far end said about a run of
// text a pane printed.
//
// The question is asked on the goroutine that draws, once a frame
// while the pointer is over the text, and the answer is a round trip
// to that machine. So it is never asked twice: the first ask starts a
// look on a goroutine of its own and answers "nothing there", and the
// answer that comes back serves every ask after it.
type pathsFar struct {
	// known is what came back, by machine and path. A path that is on
	// nothing is kept too, or the question would go out again every
	// frame the pointer stayed still.
	known map[string]farPath

	// asking is the looks that have gone out and not come back.
	asking map[string]bool

	// files is one filesystem per machine, for the looking. A reader
	// opened on a file gets one of its own, so closing this one does
	// not take the reader with it.
	files map[string]vfs.FS
}

// farPath is what one machine said about one path.
type farPath struct {
	at    string
	size  int64
	isDir bool
	found bool
}

// newPathsFar builds an empty set of answers.
func newPathsFar() *pathsFar {
	return &pathsFar{
		known:  map[string]farPath{},
		asking: map[string]bool{},
		files:  map[string]vfs.FS{},
	}
}

// findFar answers whether text a pane printed names something on the
// machine that pane is on, from what that machine has already said.
//
// The first ask for a path answers "nothing there" and sends the
// question. The window is marked dirty when the answer lands, so the
// text is underlined a moment after the pointer reaches it.
func (a *app) findFar(host, text, dir string) (string, bool, bool) {
	at, ok := farPathToTry(text, dir)
	if !ok {
		return "", false, false
	}
	key := host + "\x00" + at
	if known, have := a.far.known[key]; have {
		return known.at, known.isDir, known.found
	}
	a.askFar(host, key, at)
	return "", false, false
}

// askFar sends one question to a machine and keeps the answer.
func (a *app) askFar(host, key, at string) {
	if a.far.asking[key] || len(a.far.known) >= mostFarPaths {
		return
	}
	f, err := a.farFiles(host)
	if err != nil {
		// Nothing to ask through. Written down as nothing there, so the
		// question is not asked again on every frame.
		a.far.known[key] = farPath{}
		return
	}
	a.far.asking[key] = true
	go func() {
		e, err := f.Stat(at)
		a.pump.post(func() {
			delete(a.far.asking, key)
			if err != nil {
				a.far.known[key] = farPath{}
			} else {
				a.far.known[key] = farPath{at: at, size: e.Size, isDir: e.IsDir(), found: true}
			}
			a.markDirty()
		})
	}()
}

// farFiles is the filesystem the looking goes through for a machine,
// opened once and kept.
func (a *app) farFiles(host string) (vfs.FS, error) {
	if !a.about(host).held() {
		// The connection has gone. What it said means nothing now, and
		// a machine reconnected under the same name may be a different
		// machine.
		a.dropFar(host)
		return nil, fmt.Errorf("nothing is connected to %s", groupName(host))
	}
	if f := a.far.files[host]; f != nil {
		return f, nil
	}
	f, err := a.filesystem(host)
	if err != nil {
		return nil, err
	}
	a.far.files[host] = f
	return f, nil
}

// dropFar forgets what a machine said and lets go of the filesystem it
// was said through.
func (a *app) dropFar(host string) {
	if f := a.far.files[host]; f != nil {
		// Logged rather than returned: this runs while something else
		// is being answered, and a filesystem on a connection that has
		// already gone has nothing left to fail at.
		if err := f.Close(); err != nil {
			a.logError(fmt.Errorf("let go of the files on %s: %w", groupName(host), err))
		}
		delete(a.far.files, host)
	}
	for key := range a.far.known {
		if strings.HasPrefix(key, host+"\x00") {
			delete(a.far.known, key)
		}
	}
}

// farPathToTry is where a run of text might name something on a
// machine at the far end.
//
// The separator is guessed from the path rather than asked of the
// machine, because this runs on the goroutine that draws and asking
// would be a round trip. A name that is already whole needs no guess,
// and a relative one is joined the way the directory the shell named
// is written.
func farPathToTry(text, dir string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n\x00") {
		return "", false
	}
	if looksAbsolute(text) || winAbsolute(text) {
		return text, true
	}
	if dir == "" {
		// Nowhere to resolve a relative name against. The shell says
		// where it is through OSC 7, which it sends once the pane is
		// set up to.
		return "", false
	}
	sep := "/"
	if winAbsolute(dir) {
		sep = `\`
	}
	return strings.TrimRight(dir, `/\`) + sep + text, true
}

// winAbsolute reports whether a path starts with a drive letter, which
// filepath.IsAbs only answers on the machine it was compiled for.
func winAbsolute(text string) bool {
	if len(text) < 3 || text[1] != ':' {
		return false
	}
	if text[2] != '\\' && text[2] != '/' {
		return false
	}
	c := text[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// openPathFar puts a path on a machine at the far end on screen: a
// directory in the file browser, and a file in the viewer at the line
// it was named at.
func (a *app) openPathFar(host, at string, isDir bool, line int) error {
	if isDir {
		return a.openFilesAt(host, at)
	}
	// A filesystem of the reader's own, so the one the looking goes
	// through can be let go of without taking the reader with it.
	f, err := a.filesystem(host)
	if err != nil {
		return err
	}
	size := a.far.known[host+"\x00"+at].size
	if err := a.openReader(f, host, at, vfs.Base(f, at), false, size); err != nil {
		// The filesystem is ours until the reader has it.
		_ = f.Close()
		return err
	}
	if line > 0 {
		a.goToLineWhenRead(a.readerOn(at), line)
	}
	return nil
}
