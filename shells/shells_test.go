package shells

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

// windowsMachine is a Windows machine with everything installed, which a test then takes things away
// from by naming the programs that are not there.
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
				return `C:\Windows\System32\cmd.exe`
			}
			return ""
		},
		lookPath: func(file string) (string, error) {
			switch file {
			case "cmd.exe", "powershell.exe", "pwsh.exe", "wsl.exe":
				if !gone(file) {
					return `C:\Windows\System32\` + file, nil
				}
			}
			return "", exec.ErrNotFound
		},
		distros: func() ([]string, error) { return distros, distroErr },
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
	got := find(windowsMachine(t, []string{"Ubuntu", "Debian"}, nil))

	want := []Shell{
		{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\System32\cmd.exe`},
		{ID: "powershell", Title: "Windows PowerShell", Path: `C:\Windows\System32\powershell.exe`},
		{ID: "pwsh", Title: "PowerShell", Path: `C:\Windows\System32\pwsh.exe`},
		{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: `C:\Windows\System32\wsl.exe`,
			Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"},
		{ID: "wsl:Debian", Title: "Debian (WSL)", Path: `C:\Windows\System32\wsl.exe`,
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
		got := ids(find(windowsMachine(t, nil, nil, c.missing)))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("without %s the machine offers %v, want %v", c.missing, got, c.want)
		}
	}
}

// With no COMSPEC, cmd.exe still comes from the PATH.
func TestWithNoComspecCmdComesFromThePath(t *testing.T) {
	list := find(windowsMachine(t, nil, nil, "COMSPEC"))
	if len(list) == 0 || list[0].ID != "cmd" {
		t.Fatalf("the machine offers %v, want cmd first", ids(list))
	}
	if want := `C:\Windows\System32\cmd.exe`; list[0].Path != want {
		t.Errorf("cmd is %q, want %q", list[0].Path, want)
	}
}

// With neither COMSPEC nor cmd.exe on the PATH, cmd is not offered.
func TestWithNoCmdAnywhereItIsNotOffered(t *testing.T) {
	got := ids(find(windowsMachine(t, nil, nil, "COMSPEC", "cmd.exe")))
	if want := []string{"powershell", "pwsh"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the machine offers %v, want %v", got, want)
	}
}

// wsl.exe missing, and wsl.exe failing, both mean no distributions and leave the rest of the list alone.
func TestWslNotAnsweringLeavesTheRestOfTheList(t *testing.T) {
	want := []string{"cmd", "powershell", "pwsh"}
	for _, c := range []struct {
		what string
		p    probe
	}{
		{"wsl.exe is missing", windowsMachine(t, nil, exec.ErrNotFound, "wsl.exe")},
		{"wsl.exe fails", windowsMachine(t, nil, errors.New("exit status 1"))},
		{"wsl.exe says nothing", windowsMachine(t, nil, nil)},
	} {
		if got := ids(find(c.p)); !reflect.DeepEqual(got, want) {
			t.Errorf("when %s the machine offers %v, want %v", c.what, got, want)
		}
	}
}

// utf16le writes what wsl.exe writes: a low byte and a high byte for every character.
func utf16le(s string, bom bool) []byte {
	var b []byte
	if bom {
		b = append(b, 0xff, 0xfe)
	}
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
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
		{"plain UTF-8", []byte("Ubuntu\nDebian\n"), []string{"Ubuntu", "Debian"}},
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
	wsl := Shell{ID: "wsl:Ubuntu", Path: `C:\Windows\System32\wsl.exe`, Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}
	cmd := Shell{ID: "cmd", Path: `C:\Windows\System32\cmd.exe`}

	for _, c := range []struct {
		what string
		s    Shell
		dir  string
		want []string
	}{
		{"WSL in a directory", wsl, `C:\Workspace`,
			[]string{`C:\Windows\System32\wsl.exe`, "-d", "Ubuntu", "--cd", "/mnt/c/Workspace"}},
		{"WSL in a directory it cannot reach", wsl, `\\server\share`,
			[]string{`C:\Windows\System32\wsl.exe`, "-d", "Ubuntu"}},
		{"WSL with no directory", wsl, "",
			[]string{`C:\Windows\System32\wsl.exe`, "-d", "Ubuntu"}},
		{"cmd in a directory", cmd, `C:\Workspace`,
			[]string{`C:\Windows\System32\cmd.exe`}},
	} {
		if got := c.s.Command(c.dir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s runs %q, want %q", c.what, got, c.want)
		}
	}
	if want := []string{"-d", "Ubuntu"}; !reflect.DeepEqual(wsl.Args, want) {
		t.Errorf("Command left the shell with the arguments %q, want %q", wsl.Args, want)
	}
}

// Anything but Windows offers the login shell and nothing else.
func TestOnAnythingButWindowsTheLoginShellIsTheOnlyOne(t *testing.T) {
	for _, c := range []struct {
		what  string
		shell string
		have  []string
		want  Shell
	}{
		{"SHELL is set", "/usr/bin/zsh", nil, Shell{ID: "login", Title: "zsh", Path: "/usr/bin/zsh"}},
		{"SHELL is not set", "", []string{"/bin/bash"}, Shell{ID: "login", Title: "bash", Path: "/bin/bash"}},
		{"there is no bash either", "", nil, Shell{ID: "login", Title: "sh", Path: "/bin/sh"}},
	} {
		got := find(unixMachine(t, c.shell, c.have))
		if want := []Shell{c.want}; !reflect.DeepEqual(got, want) {
			t.Errorf("when %s the machine offers %+v, want %+v", c.what, got, want)
		}
	}
}

// unixMachine is a machine that is not Windows, with $SHELL set to shell and the programs in have
// installed. Asking it for distributions fails the test, because nothing but Windows should.
func unixMachine(t *testing.T, shell string, have []string) probe {
	t.Helper()
	return probe{
		goos: "linux",
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

// A remembered shell is found by its id, or reported gone.
func TestLookupFindsAShellById(t *testing.T) {
	list := []Shell{{ID: "cmd"}, {ID: "wsl:Ubuntu"}}

	got, ok := Lookup(list, "wsl:Ubuntu")
	if !ok || got.ID != "wsl:Ubuntu" {
		t.Errorf("looking up wsl:Ubuntu gave %+v, %v", got, ok)
	}
	if got, ok := Lookup(list, "wsl:Debian"); ok {
		t.Errorf("looking up a shell that has gone gave %+v", got)
	}
}

// One id on its own resolves without asking wsl.exe what is installed, which is what the window needs
// at startup.
func TestNamedResolvesOneIdWithoutAskingWsl(t *testing.T) {
	// windowsMachine fails the test if anything asks for distributions.
	p := windowsMachine(t, nil, nil)
	p.distros = func() ([]string, error) {
		t.Error("resolving one id ran wsl.exe")
		return nil, nil
	}

	for _, c := range []struct {
		id   string
		want Shell
	}{
		{"cmd", Shell{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\System32\cmd.exe`}},
		{"powershell", Shell{ID: "powershell", Title: "Windows PowerShell", Path: `C:\Windows\System32\powershell.exe`}},
		{"pwsh", Shell{ID: "pwsh", Title: "PowerShell", Path: `C:\Windows\System32\pwsh.exe`}},
		{"wsl:Ubuntu", Shell{ID: "wsl:Ubuntu", Title: "Ubuntu (WSL)", Path: `C:\Windows\System32\wsl.exe`,
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
		{"the id is not one of ours", "fish", nil},
		{"the distribution has no name", "wsl:", nil},
		{"the id is empty", "", nil},
	} {
		if got, ok := named(windowsMachine(t, nil, nil, c.missing...), c.id); ok {
			t.Errorf("when %s, %q resolved to %+v", c.what, c.id, got)
		}
	}
}

// On anything but Windows the only id that resolves is the login shell.
func TestNamedOnAnythingButWindows(t *testing.T) {
	p := unixMachine(t, "/usr/bin/zsh", nil)

	got, ok := named(p, "login")
	if want := (Shell{ID: "login", Title: "zsh", Path: "/usr/bin/zsh"}); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("login resolves to %+v, %v, want %+v", got, ok, want)
	}
	if got, ok := named(p, "cmd"); ok {
		t.Errorf("cmd resolved to %+v on a machine that is not Windows", got)
	}
}
