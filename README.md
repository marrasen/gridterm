# kakel

A terminal emulator in Go, for Windows and Linux, drawn on the GPU with
[gunim](https://github.com/marrasen/gunim) and animated all through.

It runs a shell on a local pseudo-terminal -- a PTY on Unix, a ConPTY on
Windows -- or on another machine over SSH, feeds the output through a VT
emulator of its own, and draws the character grid with gunim. A screen
that did not change draws nothing at all, and one that changed in part
redraws that part.

It is also a window manager for the things a terminal drags along with
it: panes on several machines at once, a two-pane file manager, a file
viewer, tunnels, and a way to hand a few panes to an agent without
handing over the machine.

Kakel is Swedish for tile. It was called gridterm until September 2026;
see [Coming from gridterm](#coming-from-gridterm).

![kakel with vim open on one of its own source files](docs/hero.png)

## Install

**One line.** These fetch the newest release, check it against
`SHA256SUMS`, and install it for you alone, with no administrator.

On Windows, in PowerShell:

```
irm https://raw.githubusercontent.com/marrasen/kakel/main/install.ps1 | iex
```

On Linux:

```
curl -fsSL https://raw.githubusercontent.com/marrasen/kakel/main/install.sh | sh
```

On Windows kakel goes to `%LOCALAPPDATA%\Programs\kakel`, with a Start
menu shortcut and an entry under Installed apps, which removes it again.
On Linux it goes to `~/.local/bin/kakel`, with a desktop file.

**Or download a build.** The [releases page](https://github.com/marrasen/kakel/releases)
has a zip for Windows and a tarball for Linux, both amd64, with
`SHA256SUMS` beside them. Unpack it and run it as it is, or install it
from Options › Install kakel…, which also offers a desktop shortcut, a
start with the computer, and automatic updates. `kakel -install` and
`kakel -uninstall` do the same from a shell.

**Updates.** An installed release looks for a newer one a minute after it
starts and once a day after. Options › Updates… says what it does then:
tell you (the default), install it by itself, or nothing. An update is
put in place for the next start, and kakel offers to restart into it.

**Or with Go.** Every build is pure Go, with no C toolchain on either
platform:

```
go install github.com/marrasen/kakel@latest
```

**Or from source.** [BUILDING.md](BUILDING.md).

## Start

```
kakel                          # your login shell
kakel -ssh user@host           # a shell on another machine
kakel -e 'vim /etc/hosts'      # one command
kakel -font-size 18            # 8 to 96 pixels
kakel -font-family 'Cascadia Mono'
kakel -list-fonts              # the monospace families installed
kakel -tray                    # into the tray, with no window
kakel -launcher                # the launcher, from a key the desktop binds
kakel -quit                    # ends the kakel running
```

With `-ssh` the window opens first and connects in a pane, so it asks
about an unknown host key in a dialog and keeps the account of how the
machine was reached. A new pane or split opens on that machine too, and
its heading in the Servers pane offers the rest: files, a command, a
tunnel and the account.

Text is drawn in Go Mono, compiled into the binary, until you pick an
installed family with `-font-family` or from the Font menu, which
remembers your pick for next time. `-font`
takes font files instead, comma separated, in the order regular, bold,
italic, bold italic. Only the regular font is required: a style you
leave out borrows one you gave.

## What it does

- **A real terminal.** bash, vim and less all run: alternate screen,
  scrollback, true colour, the text attributes, cursor shapes, mouse
  modes, bracketed paste and synchronized updates. Wide characters and
  combining marks are handled, and the box-drawing and block characters
  are drawn to the exact cell size, so a framed TUI has unbroken lines.
- **Shells here and on other machines**, over one SSH connection that
  carries several panes, a file session and tunnels at once.
- **A two-pane file manager** with panes on as many machines as you
  like, and copying, moving and deleting that run in the background and
  say how far they have got.
- **A file viewer without a shell**: paging, search, hex, tailing,
  syntax colour, and JSON logs laid out as logs.
- **Tunnels**, local, remote and SOCKS5, each with a pane saying what it
  is carrying.
- **One kakel working inside another**, and through it on the servers
  it is connected to, including joining a program already running over
  there so both people see it.
- **Panes shared with an agent** over MCP, where one code reaches
  exactly the panes you shared and nothing else.
- **Motion that says what happened.** The pane switcher zooms from
  every pane into the one you pick, and the window arrives and leaves
  with a fade. When something happens
  that you may be looking away from, the window sends an echo out past
  its edges, onto the desktop: red for a failure, green for work done,
  amber for a bell, and a faint grey ring while a connection is made. A
  theme sets the echo's colours and strength.
- **Themes that change more than colour.** A theme sets how round and
  how roomy the window is, and how it moves: Phosphor is a tight green
  screen, Marshmallow a round and bouncy pastel,
  and Ink an e-paper page with next to no motion. The theme picker
  shows each one as the highlight moves onto it.

[FEATURES.md](FEATURES.md) has the whole list, in detail.

## Keys

kakel comes with:

| Key | |
|---|---|
| `Ctrl+Shift+T` | a new terminal |
| `Ctrl+Shift+D` / `Ctrl+Shift+E` | split right / split down |
| `Ctrl+Shift+W` | close the pane |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` | the next / previous pane |
| `Ctrl+PageDown` / `Ctrl+PageUp` | the next / previous tab |
| `Ctrl+Shift+A` | show every pane at once, and pick one |
| `Ctrl+Shift+K` | the command palette |
| `Shift+Win+K` (Windows), `Ctrl+Alt+K` (Linux) | the launcher, from any program |
| `Shift+PageUp` / `Shift+PageDown` | scroll the scrollback |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | copy and paste |
| `Ctrl+Alt+V` | paste an image as a file, and type its path |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | font size |
| `Ctrl+Shift+B` | open or close the Servers pane |
| `Ctrl+Shift+L` | go to the Servers pane |
| `Ctrl+Shift+N` | connect to a server |
| `Ctrl+Shift+G` | go to a directory, in a file pane |
| `Ctrl+Shift+H` | every command and shortcut |
| `F10` | the menus |
| `Alt`, then an underlined letter | a menu, then a line in it |
| `F11` | fill the screen with the pane or split in front |

[USAGE.md](USAGE.md) covers the rest, and how to change a shortcut.

## An administrator shell on Windows

kakel has no setting to run a pane as administrator. On Windows 11 24H2
and later, Windows `sudo` can do it inside a normal pane:

```
sudo config --enable normal    # once: run elevated commands in the same pane
sudo pwsh                      # an administrator PowerShell, in this pane
```

The first line asks for UAC (User Account Control) itself, and can also
be set in Settings → System → For developers → Enable sudo → Inline.
Out of the box `sudo` is set to open a new window, which is a console
window outside kakel. Inline mode shares the pane with processes that
are not elevated, which is why Windows does not turn it on by default.

## Coming from gridterm

kakel is gridterm, renamed, with its window rebuilt on gunim.

- **Your files come along.** The first time kakel starts, it renames
  gridterm's directory, with the saved servers, secrets, themes, keys
  and shortcuts in it, to kakel's. A copy that carried a `gridterm-files`
  directory beside it carries it on as `kakel-files`.
- **A gridterm still installed keeps its files.** On Linux and macOS,
  kakel leaves a link at the old name, leading to its own directory, so
  both programs read the same files. On Windows there is no link:
  making one needs rights most users lack. A gridterm started there
  after kakel starts with no files.
- **Shortcut files keep working.** The commands kept gridterm's names.
- **A kakel and a gridterm still talk to each other.** The names they
  use between machines are unchanged, so windows of either can connect
  to each other.
- **An agent set up for gridterm is set up again for kakel.** The MCP
  server is now called kakel, and runs the kakel executable.

## Read more

| | |
|---|---|
| [FEATURES.md](FEATURES.md) | everything it does, in full |
| [USAGE.md](USAGE.md) | keys, shortcuts, sharing panes with an agent |
| [BUILDING.md](BUILDING.md) | building it, and building a release |
| [DESIGN.md](DESIGN.md) | why the code is the shape it is |
| [GAPS.md](GAPS.md) | what is not there yet |
| [LINUX.md](LINUX.md) | the state of the Linux build |
| [WORDING.md](WORDING.md) | how dialogs, buttons and commands are worded |
| [CHANGELOG.md](CHANGELOG.md) | what changed in each release |
| [RELEASING.md](RELEASING.md) | how a release is cut |

## Licence

MIT; see [LICENSE](LICENSE). The window is drawn by
[gunim](https://github.com/marrasen/gunim), by the same author. The
other dependencies are permissive: `golang.org/x/*`, `pkg/sftp`,
`kr/fs` and `atotto/clipboard` are BSD, and `go-vte` (as the fork
[marrasen/go-vte](https://github.com/marrasen/go-vte)), `go-pty`,
`uniseg` and `golang.design/x/clipboard` are MIT.

## How this was built

Each step was reviewed adversarially before the next one started, which
is where most of the interesting bugs came from: a crash on a
one-column screen, two denial-of-service paths, a deadlock between the
output pump and a device report, and a reaper that threw away a short
command's entire output. The commit messages record what each review
found.
