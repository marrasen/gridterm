package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/vfs"
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

	// Everything is worked out first, so the panel can say how far along
	// it is rather than how long it has been going.
	items, err := j.plan(ctx)
	if err != nil {
		return err
	}
	j.update(func(p *Progress) {
		for _, it := range items {
			if it.e.IsDir() {
				continue
			}
			p.Files++
			p.Bytes += it.e.Size
		}
	})

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

	names, err := j.op.From.ReadDir(from)
	if err != nil {
		return nil, err
	}
	for _, child := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
func (j *Job) copy(ctx context.Context, items []item) error {
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		skipped, err := j.put(ctx, it)
		if err != nil {
			return err
		}
		if skipped {
			continue
		}
	}
	j.update(func(p *Progress) { p.Current = "" })
	return nil
}

// put writes one thing, asking about a name that is already taken.
func (j *Job) put(ctx context.Context, it item) (skipped bool, err error) {
	to := it.to
	have, err := j.op.To.Stat(to)
	switch {
	case err == nil:
		// Something is there. A directory over a directory is not a
		// conflict: what goes in it is asked about one name at a time.
		if have.IsDir() && it.e.IsDir() {
			return false, nil
		}
		choice, err := j.decide(ctx, Conflict{
			To: j.op.To, Path: to, Have: have, Want: it.e,
		})
		if err != nil {
			return false, err
		}
		switch choice.What {
		case Skip:
			j.skip(it)
			return true, nil
		case Stop:
			return false, errStopped
		case Rename:
			if choice.Name == "" {
				return false, errors.New("jobs: renaming needs a name")
			}
			to = vfs.Join(j.op.To, vfs.Dir(j.op.To, to), choice.Name)
			if _, err := j.op.To.Stat(to); err == nil {
				return false, fmt.Errorf("jobs: %s is already there too", to)
			}
		}
	case errors.Is(err, fs.ErrNotExist):
		// Nothing there, which is the ordinary case.
	default:
		return false, err
	}

	switch {
	case it.e.IsLink():
		return false, j.op.To.Symlink(it.e.Link, to)
	case it.e.IsDir():
		return false, j.op.To.Mkdir(to, it.e.Mode)
	}
	return false, j.file(ctx, it, to)
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

// file copies one file's contents.
//
// What is half written is taken away. A file that stops part way through
// is not a file: leaving it there would let a later run mistake it for
// one that arrived.
func (j *Job) file(ctx context.Context, it item, to string) error {
	in, err := j.op.From.Open(it.from)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := j.op.To.Create(to, it.e.Mode)
	if err != nil {
		return err
	}

	err = j.stream(ctx, out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// Both: what went wrong, and whether the half-written file could
		// be taken away.
		return errors.Join(err, j.op.To.Remove(to))
	}

	j.update(func(p *Progress) { p.FilesDone++ })
	return nil
}

// stream copies the bytes, counting them and stopping when the job is
// cancelled.
func (j *Job) stream(ctx context.Context, out io.Writer, in io.Reader) error {
	if j.opts.Count != nil {
		out = meter.Writer{W: out, M: j.opts.Count, Out: j.opts.Out}
	}
	buf := make([]byte, copyBuffer)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			wrote, werr := out.Write(buf[:n])
			j.update(func(p *Progress) { p.BytesDone += int64(wrote) })
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

// move copies and then takes the original away.
//
// On one filesystem a rename does both at once and costs nothing, which
// is what makes moving a large directory on one machine instant.
func (j *Job) move(ctx context.Context, items []item) error {
	if vfs.Same(j.op.From, j.op.To) {
		return j.rename(ctx, items)
	}
	if err := j.copy(ctx, items); err != nil {
		return err
	}
	// Only what was really copied goes. Anything the user chose to skip
	// is still only in one place.
	if j.Progress().Skipped > 0 {
		return fmt.Errorf(
			"jobs: %d of them were left alone, so the originals stay where they are",
			j.Progress().Skipped)
	}
	return j.remove(ctx, items)
}

// rename moves names on one filesystem.
func (j *Job) rename(ctx context.Context, items []item) error {
	for _, it := range items {
		// Only the names that were asked for: what is inside a directory
		// goes with the directory.
		if vfs.Dir(j.op.From, it.from) != j.op.At {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		if _, err := j.op.To.Stat(it.to); err == nil {
			choice, err := j.decide(ctx, Conflict{To: j.op.To, Path: it.to, Want: it.e})
			if err != nil {
				return err
			}
			switch choice.What {
			case Skip:
				j.skip(it)
				continue
			case Stop:
				return errStopped
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := j.op.From.Rename(it.from, it.to); err != nil {
			return err
		}
		j.update(func(p *Progress) {
			if !it.e.IsDir() {
				p.FilesDone++
				p.BytesDone += it.e.Size
			}
		})
	}
	j.update(func(p *Progress) { p.Current = "" })
	return nil
}

// remove takes everything away, deepest first: a directory cannot go
// until what is in it has.
func (j *Job) remove(ctx context.Context, items []item) error {
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		j.update(func(p *Progress) { p.Current = vfs.Base(j.op.From, it.from) })

		if err := j.op.From.Remove(it.from); err != nil {
			return err
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
