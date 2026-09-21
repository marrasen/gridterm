package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// copiesCommand opens the list of copies the user asked to keep.
const copiesCommand = "files.copies"

// copiesTitle names that dialog.
const copiesTitle = "Saved Copies"

// forgetButton is the mark at the end of a remembered copy's row, which
// takes it off the list. The same cross a finished connection carries.
const forgetButton = clearButton

// asSavedCopy is a piece of file work written down as a copy that can be
// kept, and whether it is one that can be. Only a copy can be, the way
// only a copy offers a Repeat.
func asSavedCopy(op jobs.Op, from, to jobEnd) (settings.SavedCopy, bool) {
	if op.Kind != jobs.Copy || len(op.Names) == 0 || op.To == nil {
		return settings.SavedCopy{}, false
	}
	return settings.SavedCopy{
		From:       endMachine(from),
		To:         endMachine(to),
		FromWindow: windowName(from),
		ToWindow:   windowName(to),
		At:         op.At,
		Into:       op.Into,
		Names:      append([]string(nil), op.Names...),
	}, true
}

// endMachine names the machine an end's files are on.
//
// An end behind a window is filed on the panel under that window's name,
// and the machine it reached is the one the files are really on.
func endMachine(end jobEnd) string {
	if end.far.window != nil {
		return end.far.host
	}
	return end.host
}

// windowName names the window an end is reached through, and is empty
// for a machine this window reaches itself.
func windowName(end jobEnd) string {
	if end.far.window == nil {
		return ""
	}
	return end.far.window.name
}

// endOfSaved is the end a saved copy names, found again now: a window is
// looked up by name, because the one a copy ran through is gone by the
// next run.
func (a *app) endOfSaved(host, window string) (jobEnd, error) {
	if window == "" {
		return jobEnd{host: host}, nil
	}
	t := a.windows.named(window)
	if t == nil {
		return jobEnd{}, fmt.Errorf("this window is not connected to %s", window)
	}
	// Filed under the window, opened on the machine, which is how a
	// browser pane on that machine names its own end.
	return jobEnd{host: window, far: remoteHostKey{window: t, host: host}}, nil
}

// runSavedCopy does a remembered copy again, on filesystems opened
// afresh from the machines it names.
func (a *app) runSavedCopy(c settings.SavedCopy) error {
	from, err := a.endOfSaved(c.From, c.FromWindow)
	if err != nil {
		return err
	}
	to, err := a.endOfSaved(c.To, c.ToWindow)
	if err != nil {
		return err
	}
	op := jobs.Op{
		Kind:  jobs.Copy,
		At:    c.At,
		Into:  c.Into,
		Names: append([]string(nil), c.Names...),
	}
	a.repeatSavedCopy(op, from, to)
	return nil
}

// repeatSavedCopy opens both ends of a saved copy and starts it, the
// filesystems being found afresh from the machines it names.
func (a *app) repeatSavedCopy(op jobs.Op, from, to jobEnd) {
	title := "Could not copy it again"
	source, err := a.openEnd(from)
	if err != nil {
		a.reportError(title, err)
		return
	}
	into, err := a.openEnd(to)
	if err != nil {
		a.reportError(title, errors.Join(err, source.Close()))
		return
	}
	op.From, op.To = source, into
	// The names the panel files the rows under, off what was just opened.
	from.host, to.host = a.hostOf(source), a.hostOf(into)
	a.runJob(op, from, to, []vfs.FS{source, into})
}

// openCopies lists the copies the user asked to keep. Taking one runs it
// again, and the cross at the end of a row takes it off the list.
func (a *app) openCopies() error {
	kept := a.copies.all()
	if len(kept) == 0 {
		n := a.newNotice(copiesTitle,
			"None saved. A finished copy has a \"Save this copy\" box.")
		// Nothing in it worth copying: it says there is nothing here.
		n.SetNoCopy()
		a.presentNotice(n)
		return nil
	}
	var hide func()
	c := ui.NewChooser(copiesTitle, func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	c.Button = forgetButton
	c.OnPress = func(i int) error {
		if i < 0 || i >= len(kept) {
			return nil
		}
		if err := a.copies.forget(kept[i]); err != nil {
			return err
		}
		c.Forget(i)
		kept = append(kept[:i:i], kept[i+1:]...)
		a.markDirty()
		return nil
	}
	for _, saved := range kept {
		c.Add(copiedWhat(saved), copiedWhere(saved), func() error { return a.runSavedCopy(saved) })
	}
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to show them")
	}
	a.markDirty()
	return nil
}

// copiedWhat names what a saved copy copies: the files, or how many of
// them when there are more than a row can hold.
func copiedWhat(c settings.SavedCopy) string {
	if len(c.Names) == 0 {
		return "nothing"
	}
	if len(c.Names) == 1 {
		return c.Names[0]
	}
	return fileWord(len(c.Names)) + " from " + copiedDir(c.At)
}

// copiedDir is a directory as a row names it: the last part of the path,
// because the whole of one leaves no room for anything else.
func copiedDir(at string) string {
	trimmed := strings.TrimRight(at, `/\`)
	if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 && i+1 < len(trimmed) {
		return trimmed[i+1:]
	}
	if trimmed == "" {
		return at
	}
	return trimmed
}

// copiedWhere says which way a saved copy goes, for the note at the end
// of its row.
func copiedWhere(c settings.SavedCopy) string {
	return copiedEnd(c.From, c.FromWindow) + " → " + copiedEnd(c.To, c.ToWindow)
}

// copiedEnd names one end of a saved copy.
func copiedEnd(host, window string) string {
	name := groupName(host)
	if window != "" {
		name += " on " + window
	}
	return name
}
