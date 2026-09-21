package main

import (
	"errors"
	"os"

	"github.com/marrasen/gridterm/internal/build"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
)

// termProgram is what the window calls itself to the programs it runs.
//
// gridterm's own name is the true one. A user who needs a program that
// has never heard of gridterm to show pictures can put a terminal that
// program knows here, and then that program may send the rest of that
// terminal's sequences, which this one does not read.
type termProgram struct {
	remembered *settings.Settings
}

// remember is where the name is kept between runs.
func (t *termProgram) remember(set *settings.Settings) { t.remembered = set }

// name is what to put in TERM_PROGRAM, and is empty for gridterm's own.
func (t *termProgram) name() string {
	if t == nil || t.remembered == nil {
		return ""
	}
	return t.remembered.TermProgram()
}

// version is what the window answers XTVERSION with: the name and the
// version together, the way that sequence wants them.
func (t *termProgram) version() string {
	called := t.name()
	if called == "" {
		called = build.Name
	}
	return called + " " + build.Version()
}

// paneEnv is what a local pane is started with on top of the window's
// own environment.
//
// It names the terminal, so a program can tell gridterm from whatever
// TERM says, and lists those names in WSLENV for a pane in a WSL
// distribution, which a Windows variable does not otherwise reach.
func paneEnv(argv []string, called string) []string {
	if called == "" {
		called = build.Name
	}
	env := []string{
		"TERM_PROGRAM=" + called,
		"TERM_PROGRAM_VERSION=" + build.Version(),
	}
	if shells.IsWSL(argv) {
		env = append(env, "WSLENV="+shells.CarryIntoWSL(
			os.Getenv("WSLENV"), "TERM_PROGRAM", "TERM_PROGRAM_VERSION"))
	}
	return env
}

// knownTerminals are the names the field cycles through: gridterm's own
// first, then the terminals a program is most likely to have heard of.
var knownTerminals = []string{"", "iTerm.app", "WezTerm", "vscode", "Apple_Terminal"}

// openTermProgram asks what this window should call itself to the
// programs it runs.
func (a *app) openTermProgram() error {
	f := a.newForm(dlgTermProgram)
	called := f.AddField(fldTermProgram, a.newField(build.Name, 0))
	called.Options = knownTerminals
	called.SetText(a.called.name())
	f.Lines = append(f.Lines,
		"Programs read TERM_PROGRAM to identify the terminal.",
		"Blank reports "+build.Name+".",
		"",
		"Another name can enable features such as inline images.",
		"It can also produce sequences that "+build.Name+" shows as text.",
		"",
		"Applies to new panes.")

	f.AddButton(ui.Button{Title: btnSave, Do: func() error {
		return a.called.set(called.Text())
	}})
	f.AddButton(ui.Button{Title: btnCancel})

	a.showForm(f, nil)
	return nil
}

// set writes down what the window calls itself.
func (t *termProgram) set(called string) error {
	if t.remembered == nil {
		return errors.New("this window has no settings to keep the name in")
	}
	return t.remembered.PutTermProgram(called)
}
