package main

import (
	"errors"
	"github.com/marrasen/gridterm/ui/term"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/ui"
)

// The programs the harness's machine has, written out here so an
// assertion about an argv names the whole thing.
const (
	cmdPath  = `C:\Windows\system32\cmd.exe`
	pwshPath = `C:\Program Files\PowerShell\7\pwsh.exe`
	wslPath  = `C:\Windows\system32\wsl.exe`
)

// testShells is the machine most tests run on: three shells, so there is
// a choice to make, and one of them takes arguments.
func testShells() []shells.Shell {
	return []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: cmdPath},
		{ID: "pwsh", Title: "PowerShell", Path: pwshPath},
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
	}
}

// argvOf is the argv a pane on one of those shells runs, written out
// rather than asked of the shell: an assertion that called Command
// would be comparing the code with itself.
func argvOf(t *testing.T, id string) []string {
	t.Helper()
	switch id {
	case "cmd":
		return []string{cmdPath}
	case "pwsh":
		return []string{pwshPath}
	case "wsl:Ubuntu":
		return []string{wslPath, "-d", "Ubuntu"}
	}
	t.Fatalf("no shell %q on this machine", id)
	return nil
}

// onMachine gives the window a machine with these shells on it.
func onMachine(a *testApp, list []shells.Shell) {
	a.shellPick.findShells = func() ([]shells.Shell, error) { return list, nil }
	a.shellPick.namedShell = func(id string) (shells.Shell, bool) {
		return shells.Lookup(list, id)
	}
}

// scanShells looks for the shells the way the window does, on a
// goroutine of its own, and lets the draw loop pick the result up.
//
// It waits for the answer rather than for a shell to be found, so a
// machine with none does not wait the budget out, and a second scan
// waits for its own answer rather than seeing the first one.
func scanShells(t *testing.T, a *testApp) {
	t.Helper()
	a.startShellScan()
	scan := a.shellPick.scan
	waitFor(t, a, "the shell scan to answer", func() bool { return len(scan) > 0 })
	a.reapShellScan()
	if !a.shellPick.landed() {
		t.Fatal("the scan answered and the window did not take it")
	}
}

// lastDir is the directory the window last started a shell in.
func (ta *testApp) lastDir(t *testing.T) string {
	t.Helper()
	ta.shellsMu.Lock()
	defer ta.shellsMu.Unlock()
	if len(ta.dirs) == 0 {
		t.Fatal("no shell has been started")
	}
	return ta.dirs[len(ta.dirs)-1]
}

// lastArgv is the argv the window last started a shell on.
func (ta *testApp) lastArgv(t *testing.T) []string {
	t.Helper()
	ta.shellsMu.Lock()
	defer ta.shellsMu.Unlock()
	if len(ta.argvs) == 0 {
		t.Fatal("no shell has been started")
	}
	return ta.argvs[len(ta.argvs)-1]
}

// shellLines are the shell commands a menu is offering.
func shellLines(ids []string) []string {
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, shellCommandPrefix) {
			out = append(out, id)
		}
	}
	return out
}

// openFileMenu drops the File menu down, the way pressing its title
// does, and hands back the menu itself.
func openFileMenu(t *testing.T, a *testApp, bar *ui.Menubar) *ui.Menu {
	t.Helper()
	if !bar.Open(menuTitled(t, bar, "File")) {
		t.Fatal("the File menu would not open")
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}
	return menu
}

// fileMenuLines opens the File menu and returns the command ids on it.
func fileMenuLines(t *testing.T, a *testApp, bar *ui.Menubar) []string {
	t.Helper()
	ids := menuCommands(openFileMenu(t, a, bar))
	bar.Close()
	return ids
}

// lineTitle is what a menu's line for a command says, which is the empty
// string when the line leaves the naming to the command.
func lineTitle(t *testing.T, m *ui.Menu, id string) string {
	t.Helper()
	for _, item := range m.Items() {
		if item.Command == id {
			return item.Title
		}
	}
	t.Fatalf("%s is not on the menu: %v", id, menuCommands(m))
	return ""
}

func TestShellCommandID(t *testing.T) {
	for id, want := range map[string]string{
		"cmd":                  shellCommandPrefix + "cmd",
		"powershell":           shellCommandPrefix + "powershell",
		"wsl:Ubuntu":           shellCommandPrefix + "wsl-ubuntu",
		"wsl:Ubuntu 22.04 LTS": shellCommandPrefix + "wsl-ubuntu-22-04-lts",
	} {
		if got := shellCommandID(id); got != want {
			t.Errorf("shellCommandID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestChoosingAShellFromThePlusOpensAPaneOnIt drives the whole thing the
// way a user does: the plus on this machine's row, and the line for the
// shell they want.
func TestChoosingAShellFromThePlusOpensAPaneOnIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	menu := clickPlus(t, a, conns.Local)
	chooseMenuItem(t, menu, shellCommandID("pwsh"))

	want := argvOf(t, "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
	checkTree(t, a)
}

// A pane's row says the name of the shell it runs, not the path of the
// program. cmd.exe and PowerShell both call their window by their own
// path, and the row used to show it.
func TestAShellPaneRowSaysTheShellsName(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	menu := clickPlus(t, a, conns.Local)
	chooseMenuItem(t, menu, shellCommandID("pwsh"))
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("the shell opened no pane")
	}
	a.setTitle(t, len(a.shells)-1, pane, pwshPath)

	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "PowerShell" {
		t.Errorf("the row says %q, want the name this machine has for that shell", row.Text)
	}
}

// A pane on the shell nobody picked says the same. Nothing was handed to
// the session, so what it runs is whatever this machine opens by
// default: COMSPEC on Windows, and the login shell everywhere else.
//
// The machine is asked which that is rather than being told. Setting the
// variable here would not work anyway: the default is worked out once
// and remembered, so a test that arrives second gets the first one's
// answer.
func TestThePaneOnTheDefaultShellSaysItsName(t *testing.T) {
	argv, err := session.DefaultShell()
	if err != nil {
		t.Skipf("this machine opens no shell by default: %v", err)
	}
	defaultPath := argv[0]

	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	// A machine whose shell list holds the default one, so the row has a
	// name to find for it. Which shell that is differs by platform, and
	// what is being tested is that the default is named at all.
	onMachine(a, []shells.Shell{{ID: "default", Title: "The Default Shell", Path: defaultPath}})
	scanShells(t, a)

	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	if got := a.lastArgv(t); len(got) != 0 {
		t.Fatalf("the first pane was started on %v, want the default shell", got)
	}
	a.setTitle(t, 0, pane, defaultPath)

	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "The Default Shell" {
		t.Errorf("the row says %q, want the name of the shell this machine opens by default", row.Text)
	}
}

// A program that names the window something of its own is still what the
// row says. Only a name that is the program's own path is passed over.
func TestAPaneRowSaysWhatTheProgramCalledTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	pane, ok := onlyPaneWidget(t, a).(*term.Terminal)
	if !ok {
		t.Fatal("the window opened on something that is not a terminal")
	}
	a.setTitle(t, 0, pane, "make deploy")

	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "make deploy" {
		t.Errorf("the row says %q, want what the program called the window", row.Text)
	}
}

// A WSL pane's row says which distribution it runs. Every distribution
// runs wsl.exe, so only the arguments tell them apart.
func TestAWslPaneRowSaysWhichDistribution(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	onMachine(a, append(testShells(), shells.Shell{
		ID: "wsl:Debian", Title: "Debian (WSL)", Path: wslPath,
		Args: []string{"-d", "Debian"}, Distro: "Debian",
	}))
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("wsl:Debian"))
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("the shell opened no pane")
	}

	a.refreshPanel(time.Now())
	row, ok := panelRow(a, a.panes[pane])
	if !ok {
		t.Fatalf("the pane has no row: %v", panelText(a, time.Now()))
	}
	if row.Text != "Debian (WSL)" {
		t.Errorf("the row says %q, want the distribution the pane runs", row.Text)
	}
}

// TestChoosingAWslShellRunsTheDistributionItNames checks the whole argv
// of the one shell that takes arguments: wsl.exe on its own opens the
// default distribution, which is the thing this feature is here to stop.
func TestChoosingAWslShellRunsTheDistributionItNames(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("wsl:Ubuntu"))

	want := argvOf(t, "wsl:Ubuntu")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
	if len(a.panes) != 2 {
		t.Errorf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
}

// TestTheCommandTheWindowWasStartedWithBeatsTheShellPick checks what -e
// means: one program for the whole window, whatever was picked before.
func TestTheCommandTheWindowWasStartedWithBeatsTheShellPick(t *testing.T) {
	a := newTestApp(t, 80, 24, startedWith(startup{command: []string{"top"}}))
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)
	if err := a.shellPick.choose("pwsh"); err != nil {
		t.Fatalf("remember a shell: %v", err)
	}

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	if got, want := a.lastArgv(t), []string{"top"}; !slices.Equal(got, want) {
		t.Errorf("the pane ran %v, want the %v the window was started with", got, want)
	}
}

// TestThePickedShellIsRememberedForTheNextTerminal checks the pick is
// written down and that "Terminal" opens on it afterwards, which is what
// keeps the common case one click.
func TestThePickedShellIsRememberedForTheNextTerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	if id, saved := a.shellPick.remembered.Shell(); !saved || id != "pwsh" {
		t.Errorf("the settings remember %q (saved %v), want pwsh", id, saved)
	}
	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	// The pane count first: pwsh is the last argv already, so a Terminal
	// line that opened nothing at all would leave the argv below right.
	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the first, the one picked and the one Terminal opened", len(a.panes))
	}
	want := argvOf(t, "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("Terminal opened on %v, want the remembered %v", got, want)
	}
}

// TestAShellThatIsNotInstalledIsNotOffered checks that the menu is built
// from what the looking found rather than from what Windows usually has.
func TestAShellThatIsNotInstalledIsNotOffered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	without := []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\system32\cmd.exe`},
		{ID: "powershell", Title: "Windows PowerShell", Path: `C:\Windows\powershell.exe`},
	}
	onMachine(a, without)
	scanShells(t, a)

	ids := menuCommands(clickPlus(t, a, conns.Local))

	if !slices.Contains(ids, shellCommandID("cmd")) {
		t.Errorf("the menu does not offer the shells that are here: %v", ids)
	}
	if slices.Contains(ids, shellCommandID("pwsh")) {
		t.Errorf("the menu offers pwsh, which is not installed: %v", ids)
	}
	if _, ok := a.root.Commands.Lookup(shellCommandID("pwsh")); ok {
		t.Error("a command was registered for a shell that is not installed")
	}
}

// TestARememberedShellThatHasGoneFallsBackAndSaysSo checks the one case
// where the window opens something other than what was asked for. It is
// said once a run, not once a pane.
func TestARememberedShellThatHasGoneFallsBackAndSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.shellPick.remembered.PutShell("wsl:Ubuntu"); err != nil {
		t.Fatalf("remember a shell: %v", err)
	}
	a.shellPick.namedShell = func(string) (shells.Shell, bool) { return shells.Shell{}, false }

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	if got := a.lastArgv(t); len(got) != 0 {
		t.Errorf("the pane started on %v, want the default shell", got)
	}
	n := awaitModal[*ui.Notice](t, a, "a notice about the shell that has gone", nil)
	if !strings.Contains(n.Message(), "wsl:Ubuntu") {
		t.Errorf("the notice does not name the shell:\n%s", n.Message())
	}
	if !strings.Contains(n.Message(), "New Terminal In") {
		t.Errorf("the notice does not say how to pick another shell:\n%s", n.Message())
	}
	// Said once a run: another pane opens on the default in silence.
	modals := len(a.modals)
	if err := a.openPaneHere(); err != nil {
		t.Fatalf("open another tab: %v", err)
	}
	a.pump.run()
	if len(a.modals) != modals {
		t.Errorf("%d dialogs after a second pane, want the %d already up", len(a.modals), modals)
	}
}

// TestTheFileMenuOffersTheSameShellsAsThePlus checks the two ways in are
// one list.
func TestTheFileMenuOffersTheSameShellsAsThePlus(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	scanShells(t, a)

	onFile := shellLines(fileMenuLines(t, a, bar))
	onPlus := shellLines(menuCommands(clickPlus(t, a, conns.Local)))

	want := []string{shellCommandID("cmd"), shellCommandID("pwsh"), shellCommandID("wsl:Ubuntu")}
	if !slices.Equal(onFile, want) {
		t.Errorf("the File menu offers %v, want %v", onFile, want)
	}
	if !slices.Equal(onPlus, want) {
		t.Errorf("the plus offers %v, want %v", onPlus, want)
	}
}

// TestOneShellAddsNoLines checks every machine that is not Windows:
// there is nothing to choose, so "Terminal" stays the only way in.
func TestOneShellAddsNoLines(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	onMachine(a, []shells.Shell{{ID: "login", Title: "bash", Path: "/bin/bash"}})
	scanShells(t, a)

	if got := shellLines(fileMenuLines(t, a, bar)); len(got) != 0 {
		t.Errorf("the File menu offers %v on a machine with one shell", got)
	}
	ids := menuCommands(clickPlus(t, a, conns.Local))
	if got := shellLines(ids); len(got) != 0 {
		t.Errorf("the plus offers %v on a machine with one shell", got)
	}
	if !slices.Contains(ids, "conn.terminal") {
		t.Errorf("the plus no longer offers a terminal: %v", ids)
	}
}

// TestAShellThatWillNotStartIsNotRemembered checks the order: the pane
// opens first, and only a shell that started is worth writing down.
func TestAShellThatWillNotStartIsNotRemembered(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)
	a.newShell = func([]string, string, int, int) (session.Session, error) {
		return nil, errors.New("pwsh.exe is not where it was")
	}

	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	n := awaitModal[*ui.Notice](t, a, "a notice about the shell that would not start", nil)
	if !strings.Contains(n.Message(), "pwsh.exe is not where it was") {
		t.Errorf("the notice does not say why:\n%s", n.Message())
	}
	if id, saved := a.shellPick.remembered.Shell(); saved {
		t.Errorf("a shell that would not start was remembered as %q", id)
	}
}

// TestTheShellScanReportsAFailureAndKeepsWhatItFound checks the one
// failure the looking has: the WSL distributions could not be listed,
// and the shells that were found are still offered.
func TestTheShellScanReportsAFailureAndKeepsWhatItFound(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	bar := withMenubar(t, a)
	var logged []error
	a.onError = func(err error) { logged = append(logged, err) }
	reason := "wsl.exe: the operation timed out"
	a.shellPick.findShells = func() ([]shells.Shell, error) {
		return testShells(), errors.New(reason)
	}

	scanShells(t, a)

	if len(logged) == 0 {
		t.Error("the failure was swallowed")
	}
	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("top dialog = %T, want a notice about the shells", a.root.Modal())
	}
	if !strings.Contains(n.Message(), reason) {
		t.Errorf("the notice does not hold the reason:\n%s", n.Message())
	}
	// On the menu rather than in the registry: a command nothing offers
	// is a shell the user cannot open.
	dismissNotice(t, a)
	if got := shellLines(fileMenuLines(t, a, bar)); !slices.Contains(got, shellCommandID("pwsh")) {
		t.Errorf("the File menu offers %v, with none of the shells that were found", got)
	}
}

// TestChoosingAShellFromTheFileMenuOpensAPaneOnIt drives the other way
// in. Those lines carry no title of their own, so nothing else runs one.
func TestChoosingAShellFromTheFileMenuOpensAPaneOnIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	scanShells(t, a)

	chooseMenuItem(t, openFileMenu(t, a, bar), shellCommandID("pwsh"))

	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
	want := argvOf(t, "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
}

// TestTheLinesAndThePaletteSayWhichShell checks the words the user
// reads: the plus names the shell, the File menu leaves the naming to
// the command, and the palette finds that command by its own name.
func TestTheLinesAndThePaletteSayWhichShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	scanShells(t, a)

	plus := clickPlus(t, a, conns.Local)
	if got := lineTitle(t, plus, shellCommandID("pwsh")); got != "PowerShell" {
		t.Errorf("the line on the plus says %q, want the shell's own name", got)
	}
	dismiss(t, plus)
	file := openFileMenu(t, a, bar)
	if got := lineTitle(t, file, shellCommandID("pwsh")); got != "" {
		t.Errorf("the line on the File menu says %q, want the command's own title", got)
	}
	bar.Close()

	cmd, ok := a.root.Commands.Lookup(shellCommandID("pwsh"))
	if !ok {
		t.Fatal("no command opens a pane on pwsh")
	}
	if want := "New Terminal: PowerShell"; cmd.Title != want {
		t.Errorf("the command is called %q, want %q", cmd.Title, want)
	}
	runFromPalette(t, a, shellCommandID("pwsh"))

	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
	if got, want := a.lastArgv(t), argvOf(t, "pwsh"); !slices.Equal(got, want) {
		t.Errorf("the palette opened a pane on %v, want %v", got, want)
	}
}

// TestThePickSurvivesARestart is the whole way round: the window is
// given its settings the way main gives them, and a second window
// reading the same file opens on the shell the first one picked.
func TestThePickSurvivesARestart(t *testing.T) {
	at := filepath.Join(t.TempDir(), "settings.json")
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if _, err := withSettings(t, a, at); err != nil {
		t.Fatalf("the first window's settings: %v", err)
	}
	scanShells(t, a)
	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	b := newTestApp(t, 80, 24)
	withDialogs(t, b)
	withPanel(t, b)
	if _, err := withSettings(t, b, at); err != nil {
		t.Fatalf("the second window's settings: %v", err)
	}
	chooseMenuItem(t, clickPlus(t, b, conns.Local), "conn.terminal")

	if len(b.panes) != 2 {
		t.Fatalf("%d panes in the second window, want the first and the one Terminal opened",
			len(b.panes))
	}
	want := argvOf(t, "pwsh")
	if got := b.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the second window opened on %v, want the %v the first one picked", got, want)
	}
}

// TestAMachineWithNoShellsStillOpensATerminal checks the machine the
// looking found nothing on: no lines anywhere, and "Terminal" still
// opens a pane on whatever the session layer picks.
func TestAMachineWithNoShellsStillOpensATerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	onMachine(a, nil)
	scanShells(t, a)

	if got := shellLines(fileMenuLines(t, a, bar)); len(got) != 0 {
		t.Errorf("the File menu offers %v on a machine with no shells", got)
	}
	menu := clickPlus(t, a, conns.Local)
	if got := shellLines(menuCommands(menu)); len(got) != 0 {
		t.Errorf("the plus offers %v on a machine with no shells", got)
	}

	chooseMenuItem(t, menu, "conn.terminal")

	if len(a.panes) != 2 {
		t.Fatalf("%d panes, want the one the window opened with and the new one", len(a.panes))
	}
	if got := a.lastArgv(t); len(got) != 0 {
		t.Errorf("the pane started on %v, want the default shell", got)
	}
}

// TestASplitAfterAPickOpensOnThePickedShell checks the other way a pane
// opens here: a split runs the shell that was picked, the same as a tab.
func TestASplitAfterAPickOpensOnThePickedShell(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)
	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))

	if err := a.splitHere(ui.Columns); err != nil {
		t.Fatalf("split the pane: %v", err)
	}

	if len(a.panes) != 3 {
		t.Fatalf("%d panes, want the first, the one picked and the split", len(a.panes))
	}
	want := argvOf(t, "pwsh")
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the split opened on %v, want the picked %v", got, want)
	}
}

// TestASavedServersPlusOffersNoShellLines checks the gate on the lines:
// they open a pane here, and the row they would be on is a machine
// somewhere else, with shells of its own that nothing here has looked for.
func TestASavedServersPlusOffersNoShellLines(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	scanShells(t, a)
	if err := a.book.Put(remote.Host{Name: "debian", Address: "10.0.0.9", Port: 22}, ""); err != nil {
		t.Fatalf("save a server: %v", err)
	}
	a.refreshServers()

	ids := menuCommands(clickPlus(t, a, "debian"))

	if got := shellLines(ids); len(got) != 0 {
		t.Errorf("the plus on a saved server offers %v", got)
	}
}

// TestUnderSshAShellLineOpensAPaneHereAndTheFileMenuHasNone checks the
// two menus in a window whose panes open on the machine -ssh named. The
// row the plus is on says this machine; the File menu has no row, so it
// offers no shells at all.
func TestUnderSshAShellLineOpensAPaneHereAndTheFileMenuHasNone(t *testing.T) {
	s := sshtest.New(t)
	target := serverConfig(t, s).Target()
	a := startedWithSSH(t, target)
	pinServers(t, a, s)

	// A pane here, opened before the target answered, which is what puts
	// a row for this machine on the sidebar.
	sendKey(t, a, press(input.KeyT, input.ModCtrl|input.ModShift))
	waitForPanes(t, a, 2)
	if a.about(target).machine == nil {
		t.Fatal("the target is not connected, so this proves nothing")
	}
	scanShells(t, a)

	if got := shellLines(fileMenuLines(t, a, a.bar)); len(got) != 0 {
		t.Errorf("the File menu offers %v in a window whose panes open on %s", got, target)
	}
	menu := clickPlus(t, a, conns.Local)
	if got := shellLines(menuCommands(menu)); len(got) == 0 {
		t.Fatalf("the plus on this machine's row offers no shells: %v", menuCommands(menu))
	}

	chooseMenuItem(t, menu, shellCommandID("pwsh"))

	if e := a.panes[newestPane(t, a)]; e == nil || e.Host != conns.Local {
		t.Fatalf("the new pane's row is %+v, want one on this machine", e)
	}
	if got, want := a.lastArgv(t), argvOf(t, "pwsh"); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
	if n := s.Conns(); n != 1 {
		t.Errorf("the server saw %d logins, want the one it had already", n)
	}
}

// TestTwoShellsWhoseIdsDifferOnlyInPunctuationBothGetALine checks that
// nothing is dropped over a command id two shells would otherwise share.
func TestTwoShellsWhoseIdsDifferOnlyInPunctuationBothGetALine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	onMachine(a, []shells.Shell{
		{ID: "wsl:Ubuntu-22.04", Title: "Ubuntu-22.04 (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu-22.04"}, Distro: "Ubuntu-22.04"},
		{ID: "wsl:Ubuntu 22 04", Title: "Ubuntu 22 04 (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu 22 04"}, Distro: "Ubuntu 22 04"},
	})
	scanShells(t, a)

	menu := clickPlus(t, a, conns.Local)
	lines := shellLines(menuCommands(menu))
	if len(lines) != 2 {
		t.Fatalf("the plus offers %v, want a line for each of the two shells", lines)
	}
	if lines[0] == lines[1] {
		t.Fatalf("both lines run the command %q", lines[0])
	}

	// The second line opens the second distribution, not the first again.
	chooseMenuItem(t, menu, lines[1])

	want := []string{wslPath, "-d", "Ubuntu 22 04"}
	if got := a.lastArgv(t); !slices.Equal(got, want) {
		t.Errorf("the second line opened %v, want %v", got, want)
	}
}

// TestShellCommandIDsDoNotCollide checks the naming: a readable id per
// shell, and a number where two would come out the same.
func TestShellCommandIDsDoNotCollide(t *testing.T) {
	got := shellCommandIDs([]shells.Shell{
		{ID: "cmd"},
		{ID: "wsl:Ubuntu-22.04"},
		{ID: "wsl:Ubuntu 22 04"},
		{ID: "wsl:ubuntu:22:04"},
	})
	want := []string{
		shellCommandPrefix + "cmd",
		shellCommandPrefix + "wsl-ubuntu-22-04",
		shellCommandPrefix + "wsl-ubuntu-22-04-2",
		shellCommandPrefix + "wsl-ubuntu-22-04-3",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the shells are named %v, want %v", got, want)
	}
}

// TestASecondScanOffersTheShellsItFound checks that looking again
// works: the commands of the scan before it are taken away, and a shell
// installed since is on both menus.
func TestASecondScanOffersTheShellsItFound(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	bar := withMenubar(t, a)
	onMachine(a, []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: cmdPath},
		{ID: "pwsh", Title: "PowerShell", Path: pwshPath},
	})
	scanShells(t, a)

	// pwsh is gone and a distribution has arrived since.
	onMachine(a, []shells.Shell{
		{ID: "cmd", Title: "Command Prompt", Path: cmdPath},
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
	})
	scanShells(t, a)

	want := []string{shellCommandID("cmd"), shellCommandID("wsl:Ubuntu")}
	if got := shellLines(fileMenuLines(t, a, bar)); !slices.Equal(got, want) {
		t.Errorf("the File menu offers %v, want %v", got, want)
	}
	menu := clickPlus(t, a, conns.Local)
	if got := shellLines(menuCommands(menu)); !slices.Equal(got, want) {
		t.Errorf("the plus offers %v, want %v", got, want)
	}
	if _, ok := a.root.Commands.Lookup(shellCommandID("pwsh")); ok {
		t.Error("a command still opens a pane on the shell that has gone")
	}

	chooseMenuItem(t, menu, shellCommandID("wsl:Ubuntu"))

	if got, want := a.lastArgv(t), argvOf(t, "wsl:Ubuntu"); !slices.Equal(got, want) {
		t.Errorf("the pane started on %v, want %v", got, want)
	}
}

// TestARememberedDistributionThatIsGoneFallsBackOnceTheScanHasLanded
// checks what the scan is the authority for. Resolving a wsl: id asks
// only whether wsl.exe is there, so a distribution the user has
// unregistered still answers, and the pane would start, print wsl.exe's
// complaint and die, on every new tab of every run.
func TestARememberedDistributionThatIsGoneFallsBackOnceTheScanHasLanded(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	// A machine with wsl.exe on it and no Ubuntu: the looking finds two
	// shells, and resolving the id on its own still says yes.
	a.shellPick.findShells = func() ([]shells.Shell, error) {
		return []shells.Shell{
			{ID: "cmd", Title: "Command Prompt", Path: cmdPath},
			{ID: "pwsh", Title: "PowerShell", Path: pwshPath},
		}, nil
	}
	a.shellPick.namedShell = func(id string) (shells.Shell, bool) {
		if id == "wsl:Ubuntu" {
			return shells.Shell{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: wslPath,
				Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}, true
		}
		return shells.Lookup(testShells(), id)
	}
	if err := a.shellPick.remembered.PutShell("wsl:Ubuntu"); err != nil {
		t.Fatalf("remember a shell: %v", err)
	}
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	if got := a.lastArgv(t); len(got) != 0 {
		t.Errorf("the pane started on %v, want the default shell", got)
	}
	n := awaitModal[*ui.Notice](t, a, "a notice about the distribution that has gone", nil)
	if !strings.Contains(n.Message(), "wsl:Ubuntu") {
		t.Errorf("the notice does not name the shell:\n%s", n.Message())
	}
}

// TestAShellThatGoesAfterAnotherPickIsSaidAgain checks what "once a
// run" means: the telling starts again at the next pick, or a user who
// picked a shell after the first notice would never hear about the
// second one going.
func TestAShellThatGoesAfterAnotherPickIsSaidAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	// A machine with no cmd on it, and cmd is what was picked last run.
	onMachine(a, []shells.Shell{
		{ID: "pwsh", Title: "PowerShell", Path: pwshPath},
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
	})
	if err := a.shellPick.remembered.PutShell("cmd"); err != nil {
		t.Fatalf("remember a shell: %v", err)
	}
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")
	n := awaitModal[*ui.Notice](t, a, "a notice about cmd", nil)
	if !strings.Contains(n.Message(), "cmd") {
		t.Errorf("the notice does not name the shell:\n%s", n.Message())
	}
	dismissNotice(t, a)

	// pwsh is picked, and then it goes too.
	chooseMenuItem(t, clickPlus(t, a, conns.Local), shellCommandID("pwsh"))
	onMachine(a, []shells.Shell{
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: wslPath,
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
	})
	scanShells(t, a)

	chooseMenuItem(t, clickPlus(t, a, conns.Local), "conn.terminal")

	n = awaitModal[*ui.Notice](t, a, "a notice about pwsh", nil)
	if !strings.Contains(n.Message(), "pwsh") {
		t.Errorf("the notice does not name the shell that has just gone:\n%s", n.Message())
	}
	if got := a.lastArgv(t); len(got) != 0 {
		t.Errorf("the pane started on %v, want the default shell", got)
	}
}

// menuHasShellLine reports whether the shell lines offer a command.
func menuHasShellLine(a *testApp, command string) bool {
	for _, item := range a.shellPick.lines(true) {
		if item.Command == command {
			return true
		}
	}
	return false
}

// The way back to the default is offered once a shell has been picked,
// and not before: a line that undoes nothing is a line the user reads
// and tries. On the File menu as well as the plus, because the File
// menu keeps a copy of its lines.
func TestTheDefaultShellLineArrivesWithThePick(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	bar := withMenubar(t, a)
	scanShells(t, a)

	if menuHasShellLine(a, defaultShellCommand) {
		t.Error("the plus offers the way back before a shell has been picked")
	}
	if got := fileMenuLines(t, a, bar); slices.Contains(got, defaultShellCommand) {
		t.Errorf("the File menu offers %v before a shell has been picked", got)
	}

	a.rememberShell(testShells()[1])

	if !menuHasShellLine(a, defaultShellCommand) {
		t.Error("the plus does not offer the way back after a shell was picked")
	}
	if got := fileMenuLines(t, a, bar); !slices.Contains(got, defaultShellCommand) {
		t.Errorf("the File menu offers %v after a shell was picked", got)
	}
}

// A machine with one shell has nothing to pick between, and can still be
// carrying a pick for a shell that has since gone. The way back is what
// the notice about that shell tells the user to look for.
func TestTheWayBackIsOfferedWithOneShellLeft(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	onMachine(a, testShells())
	scanShells(t, a)
	if err := a.shellPick.choose("pwsh"); err != nil {
		t.Fatalf("pick a shell: %v", err)
	}

	// The shell that was picked is gone, and one is left.
	onMachine(a, testShells()[:1])
	scanShells(t, a)

	if !menuHasShellLine(a, defaultShellCommand) {
		t.Errorf("the plus offers %v, want the way back", a.shellPick.lines(true))
	}
}

// Taking the default opens a pane on whatever the machine's own default
// is, and every pane after it opens on the default too.
func TestTakingTheDefaultForgetsThePick(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	scanShells(t, a)
	a.rememberShell(testShells()[1])
	if got := a.localShell(); !slices.Equal(got, argvOf(t, "pwsh")) {
		t.Fatalf("a new pane runs %v, so the pick did not take", got)
	}
	was := len(a.panes)

	if err := a.openPaneOnDefault(); err != nil {
		t.Fatalf("open on the default: %v", err)
	}

	if _, picked := a.shellPick.chosen(); picked {
		t.Error("a shell is still picked")
	}
	if got := a.localShell(); got != nil {
		t.Errorf("a new pane runs %v, want whatever the machine picks", got)
	}
	// The pane count first: the first pane ran a nil argv too, so a line
	// that opened nothing at all would leave the argv below right.
	if got := len(a.panes); got != was+1 {
		t.Fatalf("the window has %d panes, want the one it opened", got)
	}
	if got := a.lastArgv(t); got != nil {
		t.Errorf("the pane it opened runs %v", got)
	}
	if menuHasShellLine(a, defaultShellCommand) {
		t.Error("the plus still offers the way back with nothing picked")
	}
}

// A settings file that cannot be written costs the user the reason, not
// the pane they asked for.
func TestAPickThatCannotBeForgottenStillOpensThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	scanShells(t, a)
	a.rememberShell(testShells()[1])
	was := len(a.panes)
	// Settings that hold the pick and cannot be written back: the file
	// reads, and the directory it would be written into is a file, so
	// the save has nowhere to put its temporary copy.
	dir := filepath.Join(t.TempDir(), "settings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make the directory: %v", err)
	}
	path := filepath.Join(dir, "settings.json")
	set, err := settings.Load(path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := set.PutShell("pwsh"); err != nil {
		t.Fatalf("put the shell: %v", err)
	}
	a.shellPick.remember(set)
	blockSaving(t, path)

	if err := a.openPaneOnDefault(); err != nil {
		t.Fatalf("open on the default: %v", err)
	}

	if got := len(a.panes); got != was+1 {
		t.Errorf("the window has %d panes, want the one it opened", got)
	}
	awaitModal(t, a, "the reason the pick could not be forgotten",
		byTitle[*ui.Notice]("Could not save the settings"))
}

// A window whose panes open on another machine has no local pick to
// undo, and the line must not open a local pane in it.
func TestTheDefaultShellIsRefusedOnARemoteWindow(t *testing.T) {
	s := sshtest.New(t)
	a := startedWithSSH(t, serverConfig(t, s).Target())
	pinServers(t, a, s)
	waitForPanes(t, a, 1)
	waitFor(t, a, "the window to reach the machine", func() bool {
		return a.homeMachine() != nil
	})
	was := len(a.panes)

	err := a.openPaneOnDefault()

	if err == nil {
		t.Fatal("it opened a local pane in a window whose panes are elsewhere")
	}
	if got := len(a.panes); got != was {
		t.Errorf("the window has %d panes, want the %d it had", got, was)
	}
}

// The pick coming back is written to the settings file, so it is gone on
// the next run too, and nothing else in the file goes with it.
func TestForgettingTheShellReachesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	set, err := settings.Load(path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if err := set.PutShell("pwsh"); err != nil {
		t.Fatalf("put the shell: %v", err)
	}
	if err := set.KeepCommand(settings.SavedCommand{Line: "make deploy"}, 10); err != nil {
		t.Fatalf("keep a command: %v", err)
	}

	if err := set.ForgetShell(); err != nil {
		t.Fatalf("forget: %v", err)
	}

	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("read it again: %v", err)
	}
	if id, picked := again.Shell(); picked {
		t.Errorf("the file still says %q", id)
	}
	// It is the first thing that takes a key out of the file, so what
	// else was in it has to still be there.
	if got := again.Commands(); len(got) != 1 || got[0].Line != "make deploy" {
		t.Errorf("the file holds %v, want the command that was in it", got)
	}
}

// blockSaving leaves a settings file readable and makes writing it fail,
// by turning the directory it sits in into a file of its own.
func blockSaving(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	dir := filepath.Dir(path)
	kept := filepath.Join(filepath.Dir(dir), "kept.json")
	if err := os.WriteFile(kept, raw, 0o600); err != nil {
		t.Fatalf("keep a copy: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("take the directory away: %v", err)
	}
	// A file where the directory was: MkdirAll then refuses, so the
	// save cannot make itself somewhere to write.
	if err := os.WriteFile(dir, raw, 0o600); err != nil {
		t.Fatalf("put a file in its place: %v", err)
	}
}

// Forgetting when nothing is picked writes nothing: every run of the
// line from the palette would otherwise rewrite the settings file.
func TestForgettingWithNothingPickedWritesNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	// Settings that fail any write, so a write that should not happen
	// says so instead of passing quietly.
	a.shellPick.remember(settings.Unusable(errors.New("the settings file is unreadable")))

	if err := a.shellPick.forget(); err != nil {
		t.Errorf("forgetting with nothing picked tried to save: %v", err)
	}
}
