package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/kakel/vfs"
)

// copyBuffer is how much is moved at a time. Big enough that a file over
// SFTP is not a thousand round trips, small enough that cancelling is
// noticed quickly.
const copyBuffer = 64 * 1024

// item is one thing to work on, worked out before anything is written.
type item struct {
	// from is where it is, and to where it goes. to is empty for a
	// delete.
	from, to string

	// e is what it is: a file, a directory or a link.
	e vfs.Entry
}

// do runs the job and returns why it stopped.
func (j *Job) do(ctx context.Context) error {
	if err := j.op.check(); err != nil {
		return err
	}

	// A move within one filesystem is a rename, which takes what is
	// inside a directory with it. Walking the tree first would cost a
	// round trip per directory to learn something the job never uses,
	// and would fail on a subtree the user cannot list even though the
	// rename would have worked.
	if j.op.Kind == Move && j.renames() {
		items, err := j.named(ctx)
		if err != nil {
			return err
		}
		if j.mounted(items) {
			// One of them is a volume of its own, as a folder a disk is
			// mounted on: none is renamed, and everything is copied
			// and then taken away, before a question is asked twice.
			j.noRename = true
			return j.do(ctx)
		}
		j.countAll(items)
		err = j.rename(ctx, items)
		var cd *crossDevice
		if !errors.As(err, &cd) {
			return err
		}
		// On another device after all, as under a mount point or a
		// junction: what is left is copied, and then taken away.
		left := items[cd.at:]
		j.update(func(p *Progress) {
			for _, it := range left {
				p.Files--
				p.Bytes -= it.e.Size
			}
		})
		j.op.Names = j.op.Names[cd.at:]
		j.noRename = true
		// What the renames skipped stays where it was, and is not what
		// the copy after them left alone.
		j.skippedBefore = j.Progress().Skipped
	}

	// Everything else is worked out first, so the panel can say how far
	// along it is rather than how long it has been going.
	items, err := j.plan(ctx)
	if err != nil {
		return err
	}
	j.count(items)

	switch j.op.Kind {
	case Copy:
		return j.copy(ctx, items)
	case Move:
		return j.move(ctx, items)
	case Delete:
		return j.remove(ctx, items)
	}
	return fmt.Errorf("jobs: %v is not something this does", j.op.Kind)
}

// countAll records how much there is when each name is one step,
// whatever it is. A rename takes a whole directory in one go, and the
// job never looks inside one, so a directory counts as one of the things
// to do rather than as nothing at all.
func (j *Job) countAll(items []item) {
	j.update(func(p *Progress) {
		for _, it := range items {
			p.Files++
			p.Bytes += it.e.Size
		}
	})
}

// count records how much there is altogether.
func (j *Job) count(items []item) {
	j.update(func(p *Progress) {
		for _, it := range items {
			if it.e.IsDir() {
				continue
			}
			p.Files++
			p.Bytes += it.e.Size
		}
	})
}

// named is the names the job was given, and nothing inside them.
func (j *Job) named(ctx context.Context) ([]item, error) {
	items := make([]item, 0, len(j.op.Names))
	for _, name := range j.op.Names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		from := vfs.Join(j.op.From, j.op.At, name)
		e, err := j.op.From.Stat(from)
		e = e.Stored()
		if err != nil {
			return nil, err
		}
		items = append(items, item{
			from: from, to: vfs.Join(j.op.To, j.op.Into, name), e: e,
		})
	}
	return items, nil
}

// check reports what is wrong with the work itself, before anything is
// read or written.
func (o Op) check() error {
	if o.From == nil {
		return errors.New("jobs: there is nothing to work from")
	}
	if len(o.Names) == 0 {
		return errors.New("jobs: nothing was named")
	}
	for _, name := range o.Names {
		if name == "" || name == "." || name == ".." {
			return fmt.Errorf("jobs: %q is not a name", name)
		}
	}
	if o.Kind == Delete {
		return nil
	}
	if o.To == nil {
		return errors.New("jobs: there is nowhere to put it")
	}
	if vfs.Same(o.From, o.To) && o.At == o.Into {
		return errors.New("jobs: that is where it already is")
	}
	return nil
}

// plan works out everything the job will touch.
//
// Directories come before what is in them, which is the order a copy
// needs; a delete walks the list backwards for the same reason.
func (j *Job) plan(ctx context.Context) ([]item, error) {
	var items []item
	for _, name := range j.op.Names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		from := vfs.Join(j.op.From, j.op.At, name)
		to := ""
		if j.op.Kind != Delete {
			to = vfs.Join(j.op.To, j.op.Into, name)
		}
		e, err := j.op.From.Stat(from)
		e = e.Stored()
		if err != nil {
			return nil, err
		}
		var err2 error
		if items, err2 = j.walk(ctx, items, from, to, e); err2 != nil {
			return nil, err2
		}
	}
	return items, nil
}

// walk adds one name and, when it is a directory, everything under it.
//
// A link is added as a link and not followed. Following one would copy
// what it points at, which may be the directory being copied, and a copy
// that never ends is worse than one that says it cannot do links.
func (j *Job) walk(ctx context.Context, into []item, from, to string, e vfs.Entry) ([]item, error) {
	into = append(into, item{from: from, to: to, e: e})
	if !e.IsDir() || e.IsLink() {
		return into, nil
	}

	j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, from) })
	names, err := j.op.From.ReadDir(from)
	if err != nil {
		return nil, err
	}
	for _, child := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// A zip in the folder is a file to copy, not a directory to
		// walk into, whatever the browser shows it as.
		child = child.Stored()
		childTo := ""
		if to != "" {
			childTo = vfs.Join(j.op.To, to, child.Name)
		}
		if into, err = j.walk(ctx, into,
			vfs.Join(j.op.From, from, child.Name), childTo, child); err != nil {
			return nil, err
		}
	}
	return into, nil
}

// copy writes everything the plan found onto the other filesystem.
func (j *Job) copy(ctx context.Context, items []item) (err error) {
	// The directories are made wide enough to write inside and narrowed
	// again at the end, and that has to happen however the copy ends:
	// cancelled, refused or finished. Left out of a path that returns
	// early, every directory this job made keeps the widened mode with
	// nothing saying which.
	defer func() { err = errors.Join(err, j.narrow()) }()

	// Where a directory ended up, for one the user chose to put beside
	// what was there under another name: everything inside it was
	// planned under the name it had.
	moved := map[string]string{}

	// What the user chose to leave alone. Everything inside it is left
	// alone too: they said not to touch that directory.
	var left []string

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if inside(j.op.From, left, it.from) {
			j.skip(it)
			continue
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		it.to = under(j.op.To, moved, it.to)
		wrote, err := j.put(ctx, it)
		if err != nil {
			return err
		}
		switch {
		case wrote == "":
			// Nothing was written, so nothing inside it can be either:
			// what is there is not the directory this was going into.
			if it.e.IsDir() {
				left = append(left, it.from)
			}
		case wrote != it.to:
			moved[it.to] = wrote
		}
	}
	j.update(func(p *Progress) { p.Current = "" })
	return nil
}

// narrow gives the directories this job made the mode they were asked
// for.
//
// They were made wide enough to write inside; now that whatever was
// going into them has gone in, they get what was asked for, deepest
// first so a parent closing does not shut out a child.
//
// One that fails does not stop the rest: leaving the others wide open
// would be a second fault, and the user is told every directory that
// kept the wrong mode rather than only the first.
func (j *Job) narrow() error {
	var errs []error
	for _, made := range slices.Backward(j.made) {

		if made.e.Mode.Perm() == made.e.Mode.Perm()|0o700 {
			continue
		}
		if err := j.op.To.Chmod(made.to, made.e.Mode); err != nil {
			errs = append(errs, err)
		}
	}
	// Done once: a second call would set the modes again on directories
	// that already have them, and a move calls copy before remove.
	j.made = nil
	return errors.Join(errs...)
}

// inside reports whether a path is one of a set of directories or under
// one of them.
func inside(f vfs.FS, dirs []string, path string) bool {
	sep := string(f.Sep())
	for _, dir := range dirs {
		if path == dir || strings.HasPrefix(path, dir+sep) {
			return true
		}
	}
	return false
}

// under rewrites a destination that sits inside a directory which ended
// up somewhere else.
func under(f vfs.FS, moved map[string]string, to string) string {
	if len(moved) == 0 || to == "" {
		return to
	}
	sep := string(f.Sep())
	for from, at := range moved {
		if to == from {
			return at
		}
		if strings.HasPrefix(to, from+sep) {
			return at + to[len(from):]
		}
	}
	return to
}

// put writes one thing, asking about a name that is already taken.
//
// It returns where it ended up, which is not where it was planned when
// the user chose to put it beside what was there. An empty answer means
// nothing was written.
func (j *Job) put(ctx context.Context, it item) (wrote string, err error) {
	to := it.to
	over := false
	have, err := j.op.To.Stat(to)
	have = have.Stored()
	switch {
	case err == nil:
		// Something is there. A directory over a directory is not a
		// conflict: what goes in it is asked about one name at a time.
		if have.IsDir() && it.e.IsDir() {
			return to, nil
		}
		choice, err := j.decide(ctx, Conflict{
			To: j.op.To, Path: to, Have: have, Want: it.e,
		})
		if err != nil {
			return "", err
		}
		switch choice.What {
		case Skip:
			j.skip(it)
			return "", nil
		case Stop:
			return "", ErrStopped
		case Rename:
			if to, err = beside(j.op.To, to, choice.Name); err != nil {
				return "", err
			}
		case Replace:
			if err := j.clear(to, have, it.e); err != nil {
				return "", err
			}
			over = true
		}
	case errors.Is(err, fs.ErrNotExist):
		// Nothing there, which is the ordinary case.
	default:
		return "", err
	}

	switch {
	case it.e.IsLink():
		if err := j.op.To.Symlink(it.e.Link, to); err != nil {
			return "", err
		}
		// A link is one of the things there are, so it counts as one of
		// them being done.
		j.update(func(p *Progress) { p.FilesDone++ })
		return to, nil
	case it.e.IsDir():
		// Made wide enough to write inside whatever the source says:
		// a directory copied as 0555 would lock the job out of its own
		// copy. The mode it was asked for is set once it is full.
		if err := j.op.To.Mkdir(to, it.e.Mode|0o700); err != nil {
			return "", err
		}
		j.made = append(j.made, item{to: to, e: it.e})
		return to, nil
	}
	return to, j.file(ctx, it, to, have, over)
}

// beside works out where something goes when the user chose to put it
// beside what was already there.
func beside(f vfs.FS, to, name string) (string, error) {
	switch {
	case name == "" || name == "." || name == "..":
		return "", fmt.Errorf("jobs: %q is not a name to use", name)
	case strings.ContainsAny(name, `/\`), strings.ContainsRune(name, rune(f.Sep())):
		// A name, not a path: it goes beside what is there, and nowhere
		// else on the machine.
		//
		// Both separators are refused wherever this runs, rather than
		// only the one this filesystem uses. A name typed here can be
		// carried to a machine that reads the other one as a path, and
		// a rule that changes with the machine is one the user cannot
		// learn.
		return "", fmt.Errorf("jobs: %q is a path, and a name is wanted", name)
	}
	at := vfs.Join(f, vfs.Dir(f, to), name)
	switch _, err := f.Stat(at); {
	case err == nil:
		return "", fmt.Errorf("jobs: %s is already there too", at)
	case !errors.Is(err, fs.ErrNotExist):
		// Only "it is not there" makes the name free. Any other failure
		// leaves it unknown, and writing over an unknown name is how a
		// file the user still wanted goes.
		return "", err
	}
	return at, nil
}

// clear takes away what is in the way, for a user who said to replace
// it.
//
// A file is left for Create to empty, which keeps the mode it has. A
// directory with anything in it is refused: emptying it is a job of its
// own, and doing it quietly inside another one would take away files
// nobody named.
func (j *Job) clear(to string, have, want vfs.Entry) error {
	if have.IsDir() && !have.IsLink() {
		if names, err := j.op.To.ReadDir(to); err != nil {
			return err
		} else if len(names) > 0 {
			return fmt.Errorf(
				"jobs: %s is a directory with %d things in it, so it has to go first",
				to, len(names))
		}
	}
	if !have.IsDir() && !have.IsLink() && !want.IsDir() && !want.IsLink() {
		// A file over a file: Create empties it, and the mode it has
		// stays its own.
		return nil
	}
	return j.op.To.Remove(to)
}

// skip records that the user chose not to write something.
func (j *Job) skip(it item) {
	j.update(func(p *Progress) {
		p.Skipped++
		if !it.e.IsDir() {
			p.FilesDone++
			p.BytesDone += it.e.Size
		}
	})
}

// partSuffix marks a file that is still being written. A name nothing
// else would choose, so what is left after a crash can be recognised.
const partSuffix = ".kakel-part"

// file copies one file's contents.
//
// It is written beside its name and moved onto it at the end, so the
// file at that name is either the one that was there before or the whole
// of the new one, and never half of either. Writing straight onto the
// name would empty the user's file first, and a copy that then failed --
// or was cancelled -- would leave them with nothing.
func (j *Job) file(ctx context.Context, it item, to string, have vfs.Entry, over bool) error {
	in, err := j.op.From.Open(it.from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	// The mode a file already there keeps is its own: a copy over it
	// changes what is in it, not who may read it.
	mode := it.e.Mode
	if over && !have.IsDir() {
		mode = have.Mode
	}

	part := j.partName(to)
	out, err := j.op.To.Create(part, mode)
	if err != nil {
		return err
	}
	err = j.stream(ctx, out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// Onto the name, in one step. Only now does what was there stop
		// being what is there.
		err = j.op.To.Rename(part, to)
	}
	if err != nil {
		// Both: what went wrong, and whether the part that was written
		// could be taken away.
		return errors.Join(err, j.op.To.Remove(part))
	}

	j.update(func(p *Progress) { p.FilesDone++ })
	return nil
}

// partName is where a file is written while it is being written.
func (j *Job) partName(to string) string {
	j.mu.Lock()
	j.parts++
	n := j.parts
	j.mu.Unlock()
	return vfs.Join(j.op.To, vfs.Dir(j.op.To, to),
		fmt.Sprintf(".%s.%d%s", vfs.Base(j.op.To, to), n, partSuffix))
}

// stream copies the bytes, counting them and stopping when the job is
// cancelled.
//
// Over SFTP a read or a write is a round trip to the server, and one at a
// time a copy goes no faster than a round trip lets it: 64 KB each, which
// at 50 ms is about 1.3 MB a second, whatever the line can carry. pkg/sftp
// keeps many requests on their way at once, but only in its own copies: a
// file read from a server writes itself out with WriteTo, and a file
// written to one reads itself in with ReadFromWithConcurrency. Each is
// used where the file is one of those; between two servers both are, and
// a pipe joins them. The order the parts land in does not matter: the
// copy goes to a part file, renamed onto the name only once it is whole.
func (j *Job) stream(ctx context.Context, out io.Writer, in io.Reader) error {
	counted := func(n int) {
		if n <= 0 {
			return
		}
		j.update(func(p *Progress) { p.BytesDone += int64(n) })
		if m := j.opts.Count; m != nil {
			if j.opts.Out {
				m.Moved(0, n, time.Now())
			} else {
				m.Moved(n, 0, time.Now())
			}
		}
	}
	if to, ok := out.(concurrentWriter); ok {
		from := in
		if wt, ok := in.(io.WriterTo); ok {
			// Between two servers: the reads go on all at once as well,
			// into a pipe the writes take from.
			pr, pw := io.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := wt.WriteTo(pw)
				pw.CloseWithError(err)
			}()
			defer func() {
				_ = pr.Close()
				<-done
			}()
			from = pr
		}
		_, err := to.ReadFromWithConcurrency(cancelReader{ctx: ctx, r: from, counted: counted}, 0)
		return err
	}
	if wt, ok := in.(io.WriterTo); ok {
		_, err := wt.WriteTo(cancelWriter{ctx: ctx, w: out, counted: counted})
		return err
	}
	// Neither end a server's file: the bytes go across as they are read.
	buf := make([]byte, copyBuffer)
	w := cancelWriter{ctx: ctx, w: out, counted: counted}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			wrote, werr := w.Write(buf[:n])
			if werr != nil {
				return werr
			}
			if wrote != n {
				return io.ErrShortWrite
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// concurrentWriter is a file that reads itself in with many writes on
// their way at once: pkg/sftp's.
type concurrentWriter interface {
	ReadFromWithConcurrency(r io.Reader, concurrency int) (int64, error)
}

// cancelWriter passes writes on to w, counting them, and fails once ctx
// has ended, which stops a copy that does its own writing.
type cancelWriter struct {
	ctx     context.Context
	w       io.Writer
	counted func(int)
}

func (c cancelWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(p)
	c.counted(n)
	return n, err
}

// cancelReader passes reads on from r, counting them, and fails once ctx
// has ended, which stops a copy that does its own reading.
type cancelReader struct {
	ctx     context.Context
	r       io.Reader
	counted func(int)
}

func (c cancelReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	c.counted(n)
	return n, err
}

// renames reports whether the job's move is a rename: within one
// filesystem, and on one volume of it. Between two drives of one
// machine it is a copy and a delete, as between two machines.
func (j *Job) renames() bool {
	return !j.noRename && vfs.Same(j.op.From, j.op.To) && vfs.OneVolume(j.op.From, j.op.At, j.op.Into)
}

// mounted reports whether one of the items is a volume of its own, on
// another than the folder it goes into, so renaming it would fail.
func (j *Job) mounted(items []item) bool {
	into := vfs.VolumeOf(j.op.From, j.op.Into)
	for _, it := range items {
		if it.e.IsDir() && vfs.VolumeOf(j.op.From, it.from) != into {
			return true
		}
	}
	return false
}

// crossDevice is a rename that failed as its two ends are on two
// devices, at the item numbered at, which a move then copies and
// deletes.
type crossDevice struct {
	at  int
	err error
}

func (c *crossDevice) Error() string { return c.err.Error() }
func (c *crossDevice) Unwrap() error { return c.err }

// move copies and then takes the original away.
//
// On one filesystem a rename does both at once and costs nothing, which
// is what makes moving a large directory on one machine instant.
func (j *Job) move(ctx context.Context, items []item) error {
	if j.renames() {
		return j.rename(ctx, items)
	}
	if err := j.copy(ctx, items); err != nil {
		return err
	}
	// Only what was really copied goes. Anything the user chose to skip
	// is still only in one place.
	if n := j.Progress().Skipped - j.skippedBefore; n > 0 {
		return fmt.Errorf(
			"jobs: %d of them were left alone, so the originals stay where they are",
			n)
	}
	if err := j.remove(ctx, items); err != nil {
		// The copy finished, so everything is on the other machine. Some
		// of the originals have gone and some have not, and only saying
		// so lets the user work out which.
		return fmt.Errorf(
			"jobs: everything was copied, but taking the originals away stopped part way: %w",
			err)
	}
	return nil
}

// rename moves names on one filesystem.
func (j *Job) rename(ctx context.Context, items []item) error {
	for i, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		to := it.to
		have, err := j.op.To.Stat(to)
		have = have.Stored()
		switch {
		case err == nil:
			choice, err := j.decide(ctx, Conflict{
				To: j.op.To, Path: to, Have: have, Want: it.e,
			})
			if err != nil {
				return err
			}
			switch choice.What {
			case Skip:
				j.countDone(it)
				j.update(func(p *Progress) { p.Skipped++ })
				continue
			case Stop:
				return ErrStopped
			case Rename:
				if to, err = beside(j.op.To, to, choice.Name); err != nil {
					return err
				}
			case Replace:
				// Renaming replaces a name that is taken on both
				// filesystems, but not a directory with things in it,
				// and not a name that is the wrong kind of thing.
				if err := j.clearForRename(to, have, it.e); err != nil {
					return err
				}
			}
		case errors.Is(err, fs.ErrNotExist):
			// Nothing there, which is the ordinary case.
		default:
			return err
		}

		if err := j.op.From.Rename(it.from, to); err != nil {
			if acrossDevices(err) {
				return &crossDevice{at: i, err: err}
			}
			return err
		}
		// Everything inside it went with it, so all of it is done.
		j.countDone(it)
	}
	j.update(func(p *Progress) { p.Current = "" })
	return nil
}

// clearForRename takes away what is in the way of a rename.
//
// A rename replaces a file with a file by itself, but nothing else: a
// directory in the way has to go first, and it only goes when it is
// empty.
func (j *Job) clearForRename(to string, have, want vfs.Entry) error {
	if !have.IsDir() && !want.IsDir() {
		return nil
	}
	if have.IsDir() && !have.IsLink() {
		if names, err := j.op.To.ReadDir(to); err != nil {
			return err
		} else if len(names) > 0 {
			return fmt.Errorf(
				"jobs: %s is a directory with %d things in it, so it has to go first",
				to, len(names))
		}
	}
	return j.op.To.Remove(to)
}

// countDone counts one name as finished. A renamed directory took
// everything inside it along, and the job never counted those
// separately: it never looked inside.
func (j *Job) countDone(of item) {
	j.update(func(p *Progress) {
		p.FilesDone++
		p.BytesDone += of.e.Size
	})
}

// remove takes everything away, deepest first: a directory cannot go
// until what is in it has.
func (j *Job) remove(ctx context.Context, items []item) error {
	for _, it := range slices.Backward(items) {

		if err := ctx.Err(); err != nil {
			return err
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		if err := j.op.From.Remove(it.from); err != nil {
			// Something else got there first, which is where this was
			// going anyway. Only a name that is still there and will not
			// go is a failure.
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if j.op.Kind == Delete && !it.e.IsDir() {
			j.update(func(p *Progress) {
				p.FilesDone++
				p.BytesDone += it.e.Size
			})
		}
	}
	j.update(func(p *Progress) { p.Current = "" })
	return nil
}
