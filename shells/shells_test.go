package shells

import (
	"errors"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"unicode/utf16"
)

// comspecCmd is the cmd.exe COMSPEC names, on a machine whose Windows is not on C:, so a test can tell
// it apart from the one the PATH finds.
const comspecCmd = `D:\Windows\System32\cmd.exe`

// systemDir is the directory the fake PATH finds programs in.
const systemDir = `C:\Windows\System32\`

// windowsMachine is a Windows machine with everything installed, which a test then takes things away
// from by naming the programs that are not there. Naming COMSPEC unsets it, and naming comspecCmd
// leaves it set to a file that has gone.
func windowsMachine(t *testing.T, distros []string, distroErr error, missing ...string) probe {
	t.Helper()
	gone := func(file string) bool {
		for _, m := range missing {
			if m == file {
				return true
			}
		}
		return false
	}
	return probe{
		goos: "windows",
		getenv: func(name string) string {
			if name == "COMSPEC" && !gone("COMSPEC") {
				return comspecCmd
			}
			return ""
		},
		lookPath: func(file string) (string, error) {
			switch file {
			case comspecCmd:
				if !gone(comspecCmd) {
					return comspecCmd, nil
				}
			case "cmd.exe", "powershell.exe", "pwsh.exe", "wsl.exe":
				if !gone(file) {
					return systemDir + file, nil
				}
			}
			return "", exec.ErrNotFound
		},
		distros: func() ([]string, error) { return distros, distroErr },
	}
}

// unixMachine is a machine running goos, with $SHELL set to shell and the programs in have installed.
// Asking it for distributions fails the test, because nothing but Windows should.
func unixMachine(t *testing.T, goos, shell string, have []string) probe {
	t.Helper()
	return probe{
		goos: goos,
		getenv: func(name string) string {
			if name == "SHELL" {
				return shell
			}
			return ""
		},
		lookPath: func(file string) (string, error) {
			for _, h := range have {
				if h == file {
					return file, nil
				}
			}
			return "", exec.ErrNotFound
		},
		distros: func() ([]string, error) {
			t.Error("a machine that is not Windows asked for WSL distributions")
			return nil, nil
		},
	}
}

// ids is the list of shells as a test reads it.
func ids(list []Shell) []string {
	got := make([]string, 0, len(list))
	for _, s := range list {
		got = append(got, s.ID)
	}
	return got
}

// A Windows machine offers cmd, both PowerShells, and one line per distribution.
func TestAWindowsMachineOffersEveryShellItHas(t *testing.T) {
	got, err := find(windowsMachine(t, []string{"Ubuntu", "Debian"}, nil))
	if err != nil {
		t.Fatalf("finding the shells: %v", err)
	}

	want := []Shell{
		{ID: "cmd", Title: "Command Prompt", Path: comspecCmd},
		{ID: "powershell", Title: "Windows PowerShell", Path: systemDir + "powershell.exe"},
		{ID: "pwsh", Title: "PowerShell", Path: systemDir + "pwsh.exe"},
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: systemDir + "wsl.exe",
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
		{ID: "wsl:Debian", Title: "Debian (WSL)", Path: systemDir + "wsl.exe",
			Args: []string{"-d", "Debian"}, Distro: "Debian"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the machine offers %+v, want %+v", got, want)
	}
}

// A shell that is not on the PATH is not offered.
func TestAShellThatIsNotInstalledIsNotOffered(t *testing.T) {
	for _, c := range []struct {
		missing string
		want    []string
	}{
		{"powershell.exe", []string{"cmd", "pwsh"}},
		{"pwsh.exe", []string{"cmd", "powershell"}},
	} {
		list, err := find(windowsMachine(t, nil, nil, c.missing))
		if err != nil {
			t.Fatalf("finding the shells without %s: %v", c.missing, err)
		}
		if got := ids(list); !reflect.DeepEqual(got, c.want) {
			t.Errorf("without %s the machine offers %v, want %v", c.missing, got, c.want)
		}
	}
}

// cmd.exe comes from COMSPEC when the file it names is there, and from the PATH when it is not.
func TestCmdComesFromComspecOrThePath(t *testing.T) {
	for _, c := range []struct {
		what    string
		missing []string
		want    string
	}{
		{"COMSPEC names a cmd.exe that is there", nil, comspecCmd},
		{"COMSPEC is not set", []string{"COMSPEC"}, systemDir + "cmd.exe"},
		{"COMSPEC names a file that has gone", []string{comspecCmd}, systemDir + "cmd.exe"},
		{"COMSPEC names a cmd.exe the PATH does not", []string{"cmd.exe"}, comspecCmd},
	} {
		list, err := find(windowsMachine(t, nil, nil, c.missing...))
		if err != nil {
			t.Fatalf("when %s, finding the shells: %v", c.what, err)
		}
		if len(list) == 0 || list[0].ID != "cmd" {
			t.Fatalf("when %s the machine offers %v, want cmd first", c.what, ids(list))
		}
		if list[0].Path != c.want {
			t.Errorf("when %s cmd is %q, want %q", c.what, list[0].Path, c.want)
		}
	}
}

// With no cmd.exe that anything can find, cmd is not offered.
func TestWithNoCmdAnywhereItIsNotOffered(t *testing.T) {
	for _, c := range []struct {
		what    string
		missing []string
	}{
		{"COMSPEC is not set and cmd.exe is not on the PATH", []string{"COMSPEC", "cmd.exe"}},
		{"COMSPEC names a file that has gone and cmd.exe is not on the PATH", []string{comspecCmd, "cmd.exe"}},
	} {
		list, err := find(windowsMachine(t, nil, nil, c.missing...))
		if err != nil {
			t.Fatalf("when %s, finding the shells: %v", c.what, err)
		}
		if got, want := ids(list), []string{"powershell", "pwsh"}; !reflect.DeepEqual(got, want) {
			t.Errorf("when %s the machine offers %v, want %v", c.what, got, want)
		}
	}
}

// Nothing wsl.exe does takes the other shells off the list, and only a real failure comes back as an
// error: a machine with no WSL at all is an answer.
func TestWslNotAnsweringLeavesTheRestOfTheList(t *testing.T) {
	broken := errors.New("exit status 1")
	want := []string{"cmd", "powershell", "pwsh"}

	for _, c := range []struct {
		what    string
		p       probe
		wantErr error
	}{
		{"wsl.exe is missing", windowsMachine(t, nil,
			&exec.Error{Name: "wsl.exe", Err: exec.ErrNotFound}, "wsl.exe"), nil},
		{"wsl.exe fails", windowsMachine(t, nil, broken), broken},
		{"wsl.exe says nothing", windowsMachine(t, nil, nil), nil},
		{"wsl.exe listed a distribution but is gone from the PATH",
			windowsMachine(t, []string{"Ubuntu"}, nil, "wsl.exe"), nil},
	} {
		got, err := find(c.p)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("when %s, finding the shells gave the error %v, want %v", c.what, err, c.wantErr)
		}
		if !reflect.DeepEqual(ids(got), want) {
			t.Errorf("when %s the machine offers %v, want %v", c.what, ids(got), want)
		}
	}
}

// utf16le writes what wsl.exe writes: UTF-16LE, behind a byte order mark or not.
func utf16le(s string, bom bool) []byte {
	var b []byte
	if bom {
		b = append(b, 0xff, 0xfe)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// Distribution names come back from UTF-16LE, with or without a byte order mark.
func TestDistributionNamesComeBackFromUtf16(t *testing.T) {
	for _, c := range []struct {
		what string
		in   []byte
		want []string
	}{
		{"UTF-16LE", utf16le("Ubuntu\r\nDebian\r\n", false), []string{"Ubuntu", "Debian"}},
		{"UTF-16LE with a mark", utf16le("Ubuntu\r\nDebian\r\n", true), []string{"Ubuntu", "Debian"}},
		{"a name with a space", utf16le("Ubuntu 22.04 LTS\r\n", true), []string{"Ubuntu 22.04 LTS"}},
		{"blank lines", utf16le("\r\nUbuntu\r\n\r\n", false), []string{"Ubuntu"}},
		{"a non-ASCII name", utf16le("日本\r\n", false), []string{"日本"}},
		{"a non-ASCII name with a mark", utf16le("日本\r\n", true), []string{"日本"}},
		{"a non-ASCII name and no newline", utf16le("日本", false), []string{"日本"}},
		{"a name outside the basic plane", utf16le("Ubuntu 🐧\r\n", true), []string{"Ubuntu 🐧"}},
		// An even number of bytes, so the length check is not what lets this one through.
		{"plain UTF-8", []byte("Ubuntu\nDebian\n"), []string{"Ubuntu", "Debian"}},
		{"a mark and nothing else", utf16le("", true), nil},
		{"a mark and half a character", append(utf16le("Ubuntu", true), 'X'), []string{"Ubuntu"}},
		{"nothing at all", nil, nil},
	} {
		if got := parseDistros(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s reads as %q, want %q", c.what, got, c.want)
		}
	}
}

// A Windows directory as WSL sees it.
func TestUnixPath(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{`C:\Workspace`, "/mnt/c/Workspace"},
		{`c:\`, "/mnt/c/"},
		{"C:/Workspace/gridterm", "/mnt/c/Workspace/gridterm"},
		{`D:\a b\c`, "/mnt/d/a b/c"},
		{`C:\Workspace\`, "/mnt/c/Workspace/"},
		{`C:/Workspace\gridterm/shells`, "/mnt/c/Workspace/gridterm/shells"},
		// A drive-relative path names the drive's working directory, which WSL has no idea about.
		{`C:Workspace`, ""},
		// A long-path prefix is left for WSL to fail on, the same as a UNC path.
		{`\\?\C:\Workspace`, ""},
		{`\\server\share`, ""},
		{"//server/share", ""},
		{`Workspace\gridterm`, ""},
		{"", ""},
		{"C:", ""},
		{"/usr/local", ""},
	} {
		if got := UnixPath(c.in); got != c.want {
			t.Errorf("%q is %q to WSL, want %q", c.in, got, c.want)
		}
	}
}

// What a pane runs, with and without a directory to start in.
func TestCommand(t *testing.T) {
	// Room to spare in Args, so a Command that appended to it would be caught below.
	wsl := Shell{ID: "wsl:Ubuntu", Path: systemDir + "wsl.exe", Distro: "Ubuntu",
		Args: append(make([]string, 0, 8), "-d", "Ubuntu")}
	cmd := Shell{ID: "cmd", Path: comspecCmd}

	for _, c := range []struct {
		what string
		s    Shell
		dir  string
		want []string
	}{
		{"WSL in a directory", wsl, `C:\Workspace`,
			[]string{systemDir + "wsl.exe", "-d", "Ubuntu", "--cd", "/mnt/c/Workspace"}},
		{"WSL in a directory it cannot reach", wsl, `\\server\share`,
			[]string{systemDir + "wsl.exe", "-d", "Ubuntu"}},
		{"WSL with no directory", wsl, "",
			[]string{systemDir + "wsl.exe", "-d", "Ubuntu"}},
		{"cmd in a directory", cmd, `C:\Workspace`, []string{comspecCmd}},
	} {
		if got := c.s.Command(c.dir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s runs %q, want %q", c.what, got, c.want)
		}
	}

	// Command copies the arguments rather than handing back the shell's own slice.
	if want := []string{"-d", "Ubuntu"}; !reflect.DeepEqual(wsl.Args, want) {
		t.Errorf("Command left the shell with the arguments %q, want %q", wsl.Args, want)
	}
	if argv := wsl.Command(`C:\Workspace`); &argv[1] == &wsl.Args[0] {
		t.Error("Command gave back arguments that share the shell's own slice")
	}
}

// A distribution name with a space in it keeps its shape through the list, the id and the argv.
func TestADistributionNameWithASpace(t *testing.T) {
	const distro = "Ubuntu 22.04 LTS"
	want := Shell{
		ID:     "wsl:Ubuntu 22.04 LTS",
		Title:  "Ubuntu 22.04 LTS (WSL)",
		Path:   systemDir + "wsl.exe",
		Args:   []string{"-d", distro},
		Distro: distro,
	}

	list, err := find(windowsMachine(t, []string{distro}, nil))
	if err != nil {
		t.Fatalf("finding the shells: %v", err)
	}
	if got, ok := Lookup(list, want.ID); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("the list holds %+v, %v, want %+v", got, ok, want)
	}
	if got, ok := named(windowsMachine(t, nil, nil), want.ID); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("%q resolves to %+v, %v, want %+v", want.ID, got, ok, want)
	}

	got := want.Command(`C:\Workspace`)
	wantArgv := []string{systemDir + "wsl.exe", "-d", distro, "--cd", "/mnt/c/Workspace"}
	if !reflect.DeepEqual(got, wantArgv) {
		t.Errorf("a pane runs %q, want %q", got, wantArgv)
	}
}

// Anything but Windows offers the login shell and nothing else, and nothing at all when there is no
// shell to offer.
func TestOnAnythingButWindowsTheLoginShellIsTheOnlyOne(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, c := range []struct {
			what  string
			shell string
			have  []string
			want  []Shell
		}{
			{"SHELL is set", "/usr/bin/zsh", nil,
				[]Shell{{ID: "login", Title: "zsh", Path: "/usr/bin/zsh"}}},
			{"SHELL is not set", "", []string{"/bin/bash", "/bin/sh"},
				[]Shell{{ID: "login", Title: "bash", Path: "/bin/bash"}}},
			{"there is no bash either", "", []string{"/bin/sh"},
				[]Shell{{ID: "login", Title: "sh", Path: "/bin/sh"}}},
			{"there is no shell at all", "", nil, nil},
		} {
			got, err := find(unixMachine(t, goos, c.shell, c.have))
			if err != nil {
				t.Fatalf("on %s when %s, finding the shells: %v", goos, c.what, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("on %s when %s the machine offers %+v, want %+v", goos, c.what, got, c.want)
			}
		}
	}
}

// A remembered shell is found by its id, or reported gone.
func TestLookupFindsAShellById(t *testing.T) {
	cmd := Shell{ID: "cmd", Title: "Command Prompt", Path: comspecCmd}
	ubuntu := Shell{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: systemDir + "wsl.exe",
		Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}
	list := []Shell{cmd, ubuntu}

	for _, want := range list {
		if got, ok := Lookup(list, want.ID); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("looking up %q gave %+v, %v, want %+v", want.ID, got, ok, want)
		}
	}
	for _, c := range []struct {
		what string
		list []Shell
		id   string
	}{
		{"a shell that has gone", list, "wsl:Debian"},
		{"an empty id", list, ""},
		{"an id in an empty list", nil, "cmd"},
	} {
		got, ok := Lookup(c.list, c.id)
		if ok || !reflect.DeepEqual(got, Shell{}) {
			t.Errorf("looking up %s gave %+v, %v, want nothing", c.what, got, ok)
		}
	}
}

// One id on its own resolves without asking wsl.exe what is installed, which is what the window needs
// at startup.
func TestNamedResolvesOneIdWithoutAskingWsl(t *testing.T) {
	// Fail the test if resolving an id lists the distributions.
	p := windowsMachine(t, nil, nil)
	p.distros = func() ([]string, error) {
		t.Error("resolving one id ran wsl.exe")
		return nil, nil
	}

	for _, c := range []struct {
		id   string
		want Shell
	}{
		{"cmd", Shell{ID: "cmd", Title: "Command Prompt", Path: comspecCmd}},
		{"powershell", Shell{ID: "powershell", Title: "Windows PowerShell", Path: systemDir + "powershell.exe"}},
		{"pwsh", Shell{ID: "pwsh", Title: "PowerShell", Path: systemDir + "pwsh.exe"}},
		{"wsl:Ubuntu", Shell{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: systemDir + "wsl.exe",
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}},
	} {
		got, ok := named(p, c.id)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q resolves to %+v, %v, want %+v", c.id, got, ok, c.want)
		}
	}
}

// An id nobody can run resolves to nothing.
func TestNamedSaysWhenAnIdIsNotAShellHere(t *testing.T) {
	for _, c := range []struct {
		what    string
		id      string
		missing []string
	}{
		{"the program is not installed", "pwsh", []string{"pwsh.exe"}},
		{"wsl.exe is not installed", "wsl:Ubuntu", []string{"wsl.exe"}},
		{"there is no cmd.exe anywhere", "cmd", []string{"COMSPEC", "cmd.exe"}},
		{"COMSPEC names a file that has gone and the PATH has no cmd.exe", "cmd", []string{comspecCmd, "cmd.exe"}},
		{"the id is not one of ours", "fish", nil},
		{"the distribution has no name", "wsl:", nil},
		{"the id is empty", "", nil},
	} {
		got, ok := named(windowsMachine(t, nil, nil, c.missing...), c.id)
		if ok || !reflect.DeepEqual(got, Shell{}) {
			t.Errorf("when %s, %q resolved to %+v, %v, want nothing", c.what, c.id, got, ok)
		}
	}
}

// On anything but Windows the only id that resolves is the login shell, and even that needs a shell to
// be there.
func TestNamedOnAnythingButWindows(t *testing.T) {
	p := unixMachine(t, "linux", "/usr/bin/zsh", nil)

	got, ok := named(p, "login")
	if want := (Shell{ID: "login", Title: "zsh", Path: "/usr/bin/zsh"}); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("login resolves to %+v, %v, want %+v", got, ok, want)
	}
	if got, ok := named(p, "cmd"); ok {
		t.Errorf("cmd resolved to %+v on a machine that is not Windows", got)
	}
	if got, ok := named(unixMachine(t, "darwin", "", nil), "login"); ok {
		t.Errorf("login resolved to %+v on a machine with no shell at all", got)
	}
}

// Find and Named answer on the machine the tests are running on.
func TestFindAndNamedOnThisMachine(t *testing.T) {
	list, err := Find()
	if err != nil {
		// A machine whose WSL is broken still offers its other shells, so this is not a failure here.
		t.Logf("listing the WSL distributions failed: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("this machine offers no shells at all")
	}

	if runtime.GOOS != "windows" {
		if got, want := ids(list), []string{loginID}; !reflect.DeepEqual(got, want) {
			t.Fatalf("this machine offers %v, want %v", got, want)
		}
		if got, ok := Named(loginID); !ok || got.ID != loginID {
			t.Errorf("Named(%q) gave %+v, %v, want the login shell", loginID, got, ok)
		}
		return
	}

	cmd, ok := Lookup(list, "cmd")
	if !ok || cmd.Path == "" {
		t.Fatalf("this machine offers %v, want a cmd with a path, got %+v", ids(list), cmd)
	}
	if got, ok := Named("cmd"); !ok || got.ID != cmd.ID {
		t.Errorf("Named(cmd) gave %+v, %v, want the id %q", got, ok, cmd.ID)
	}
}
