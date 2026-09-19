package main

import (
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// openReader puts a pane in the window showing one file.
//
// The filesystem is the one the pane it was opened from is reading, and
// it is not this pane's to close: the browser opened it and the browser
// lets it go. So a reader holds the name of the machine rather than a
// connection to it, and reads through the browser's.
func (a *app) openReader(f vfs.FS, host, path, name string) error {
	if f == nil {
		return errors.New("there is no filesystem to read that file through")
	}
	r := files.NewReader(name, path)
	r.Style = a.paneStyle()
	// Off the drawing goroutine, and back onto it with the answer: a
	// file on a machine with a long way to go would otherwise stop the
	// window while it was read.
	r.Read = func(then func([]string, bool, error)) {
		go func() {
			lines, cut, err := files.ReadFile(f, path)
			a.pump.post(func() { then(lines, cut, err) })
		}()
	}
	r.OnClose = func() {
		a.pump.post(func() {
			if err := a.closePane(r); err != nil {
				a.reportError("Could not close the reader", err)
			}
		})
	}
	if err := a.placePane(r); err != nil {
		return err
	}
	row := &conns.Entry{
		Host:   host,
		Kind:   conns.Reader,
		Label:  name,
		Reveal: func() { a.focus(r) },
		Close:  func() error { return a.closePane(r) },
	}
	if a.readers == nil {
		a.readers = map[*files.Reader]*conns.Entry{}
	}
	a.readers[r] = row
	a.registry.Add(row)
	r.Open()
	return nil
}

// readFileFrom opens a reader on whatever the browser has picked out.
func (a *app) readFileFrom(p *files.Pane, e vfs.Entry) error {
	f := p.FS()
	if f == nil {
		return errors.New("that pane is on no machine")
	}
	at := vfs.Join(f, p.At(), e.Name)
	return a.openReader(f, a.hostOfPane(p), at, e.Name)
}

// hostOfPane is the machine a browser pane is reading, as the sidebar
// calls it, so a reader opened from it sits under the same heading.
func (a *app) hostOfPane(p *files.Pane) string {
	if a.files != nil {
		if row := a.files.rows[p]; row != nil {
			return row.Host
		}
	}
	return conns.Local
}

// dropReader takes a reader's row off the sidebar, for a pane that has
// been closed.
func (a *app) dropReader(r *files.Reader) {
	row := a.readers[r]
	if row == nil {
		return
	}
	a.registry.Drop(row)
	delete(a.readers, r)
}

// readerNote is what a reader's row says beside its name: where in the
// file it is, or why the file would not read.
func (a *app) readerNote(r *files.Reader) string {
	if err := r.Err(); err != nil {
		return err.Error()
	}
	if r.Busy() {
		return "reading"
	}
	if n := r.Lines(); n > 0 {
		of := fmt.Sprintf("%d lines", n)
		if r.Cut() {
			of += "+"
		}
		return of
	}
	return ""
}
