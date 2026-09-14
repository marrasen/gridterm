# gridterm

A GPU-rendered terminal emulator in Go, built for Windows first.

It runs a shell on a local pseudo-terminal — a PTY on Unix, a ConPTY on
Windows — or on another machine over SSH, feeds the output through a VT
emulator, and draws the resulting character grid as batched triangles.

24,043 lines of Go, 29,048 lines of tests, 1,117 tests.

![a shell running in gridterm](docs/shell.png)

## What works

- **A real terminal.** bash, vim and less all run: alternate screen,
  scroll regions, scrollback, 256 and true colour, bold, dim, italic,
  underline, strikethrough and reverse video, window title, cursor
  shapes, device reports, bracketed paste and mouse modes.
- **Local shells and SSH.** One `session.Session` interface with two
  implementations. Nothing above it — the emulator, the grid, the
  renderer — can tell the difference.
- **Connections, not just shells.** One SSH connection carries several
  things at once, so a second terminal on a machine is a second channel
  rather than a second login. A remote command gets a connection of its
  own, named by what it runs. Connect from inside the window with
  `Ctrl+Shift+N`.
- **One machine reached through another.** A saved server can say it is
  behind another one. The second connection is carried inside a channel
  of the first, so no local port is opened for it and nothing else on
  the machine can use it. Closing the one in the middle closes what
  rides on it.
- **A file manager with as many panes as you want.** One manager for the
  window, and a pane added to it from the plus on any machine in the
  sidebar: this machine, a server, or five of each with gridterm in the
  middle. The keys are the ones a two-pane browser has had for thirty
  years — Tab moves to the next pane, Enter descends, Backspace goes up,
  Space marks, F5 copies to the next pane, F6 moves, F7 makes a
  directory, F8 deletes — and a bar along the bottom says which key does
  what, the way Midnight Commander does. Clicking a key on the bar does
  what pressing it does. A directory is never read on the goroutine that
  draws, so a slow machine cannot stop the window, and a read that fails
  leaves the listing that worked on screen with the reason beside it.
- **File work in the background.** Copying, moving and deleting, on one
  machine or between two, with how far along it is and a way to stop it.
  A name that is already there is asked about — replace, skip, rename, or
  stop — and never decided alone. A file is written beside its name and
  moved onto it at the end, so what is at that name is either the file
  that was there or the whole of the new one, never half of either. Every
  failure stops the job and says why: half a directory that says it
  worked is worse than one that stopped.
- **Tunnels.** A port here that stands for a service over there, a port
  over there that stands for one here, or a SOCKS5 proxy that reaches
  whatever it is asked for as the far machine sees it. A tunnel with no
  address of its own listens on that machine only, and one that would
  let the rest of the network through asks before it opens — as does
  every remote forward, because where the far machine really binds it is
  the far machine's decision. The panel shows what each is carrying: how
  many streams, how fast, and how many failed.
- **A sidebar instead of a row of tabs.** It is open when the window
  opens, and it is how everything is reached: every terminal, file pane,
  tunnel and transfer, grouped by the machine it is on with this one at
  the top. A dot in front of each row says what it is doing — green for
  open, brightening and dimming while bytes are going past, grey once it
  has finished — so the words beside it are left for a speed or a count.
  Every machine carries a plus that drops a menu of what can be opened
  there, and "Connect to server…" is pinned under the list. Nothing polls
  and nothing ticks: the row is worked out afresh each frame from when
  the last byte went by, so an idle sidebar redraws nothing at all.
  `Ctrl+Shift+B` hides it and shows it again.
- **Servers are saved.** A machine you add gets a line on the Servers
  menu and an entry in the palette, kept in a JSON file under the OS
  configuration directory. It holds no secret and never will. A list
  that cannot be read is reported and is never written over, because a
  file nobody could parse is still somebody's list of servers.
- **Secrets are asked for in the window.** A key passphrase, an account
  password and a one-time code all get a dialog. An unlocked key is kept
  in memory for as long as the window is open and never written
  anywhere, so the second connection to a machine asks nothing.
- **Unknown host keys are shown, not assumed.** A host that is not in
  `known_hosts` gets a dialog with its fingerprint, and only an explicit
  yes records it. A key that does not match one already recorded is
  refused with no button to press.
- **Batched rendering.** A full screen of text is one `DrawTriangles`
  call for the backgrounds plus one per atlas page for the glyphs,
  typically two in total however much text is on screen.
- **Damage tracking.** Writing a cell that already holds the same
  content does not dirty its row, so an idle screen draws nothing at all.
- **Wide characters and combining marks.** CJK and emoji take two
  columns; a base character and its marks share one cell.
- **Box drawing that joins up.** The box and block characters are drawn
  in code at the exact cell size, so framed TUIs have unbroken lines.
- **A real key pipeline.** Press, release and OS repeat with modifiers,
  correlated with the text they produced, encoded to the bytes a program
  expects — including application cursor mode, which vim and readline
  need.
- **Mouse, selection and clipboard.** Programs that ask for the mouse
  get it; hold Shift to select text anyway. Drag to select, Alt+drag for
  a rectangle.

![selecting text with the mouse](docs/selection.png)

![vim running on the alternate screen](docs/vim.png)

## Try it

```
git clone https://github.com/marrasen/gridterm
cd gridterm
go run .                       # your login shell
go run . -ssh user@host        # a shell on another machine
go run . -e 'vim /etc/hosts'   # one command
go run . -font-size 18
go run . -font /path/to/Regular.ttf,/path/to/Bold.ttf
```

Text is drawn in the four Go Mono faces compiled into the binary:
regular, bold, italic and bold italic. `-font` takes font files instead,
comma separated, in the order regular, bold, italic, bold italic. Only
the regular font is required — a style you leave out borrows one you
gave. There is no way to pick a font by family name yet; give paths.

| Key | |
|---|---|
| `Shift+PageUp` / `Shift+PageDown` | scroll the scrollback |
| mouse wheel | scroll, or arrow keys on the alternate screen |
| drag | select; `Alt+drag` selects a rectangle |
| `Shift+drag` | select even while a program owns the mouse |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | copy and paste |
| middle click | paste |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | font size |
| `Ctrl+Shift+B` | show or hide the sidebar |
| `Ctrl+Shift+L` | go to the sidebar |
| `Ctrl+Shift+N` | connect to a server |

In the file manager: `Tab` moves to the next pane, `Enter` descends,
`Backspace` goes up, `Space` marks, `F2` renames, `F5` copies to the next
pane, `F6` moves, `F7` makes a directory, `F8` deletes. The bar along the
bottom says the same thing, and clicking a key on it runs that key.

On Windows there is nothing else to install — no C toolchain, no cgo:

```
go build -o gridterm.exe .
```

Cross-compiling to Windows from anywhere else works the same way:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o gridterm.exe .
```

Building for Linux needs the X11 development headers ebitengine's
bundled GLFW compiles against:

```
sudo apt-get install -y libxcursor-dev libxinerama-dev libxi-dev \
    libxxf86vm-dev libxrandr-dev libgl1-mesa-dev
```

Most of the code needs neither. `make test` runs everything that does
not touch a GPU, which is the grid, the emulator, the key and mouse
encoders and both session types.

## Layout

| Package | Lines | Needs a GPU? | What it is |
|---|---|---|---|
| `vt` | 1,720 | no | the VT emulator: parser, screen model, two buffers, scrollback |
| `grid` | 863 | no | the display grid, damage tracking, selection, wide-character invariants |
| `input` | 592 | no | key, text, mouse and paste events to VT bytes |
| `session` | 362 | no | a shell as a byte stream, and the local pty |
| `remote` | 3,570 | no | SSH: connections, shells, host keys, unlocked keys, tunnels |
| `conns` | 221 | no | what the window has open, grouped by machine |
| `vfs` | 668 | no | a filesystem a file pane works on: this machine, or one over SFTP |
| `jobs` | 1,142 | no | copying, moving and deleting in the background, with progress and cancel |
| `meter` | 257 | no | bytes moved, and how long ago: the four states |
| `ui` | 5,203 | no | the widget toolkit: panes, tabs, menus, dialogs, fields, lists |
| `ui/term` | 529 | no | a shell on a widget |
| `ui/files` | 1,198 | no | the file manager: any number of panes side by side |
| `glyph` | 1,289 | yes | glyph atlas, system font fallback, box drawing |
| `render` | 1,149 | yes | grid to batched triangles |
| `main` | 5,114 | yes | the window and the wiring |

The layering is deliberate: `vt` never imports the renderer, `input`
never imports ebiten (that lives in `input/ebitenin`), `ui` knows nothing
about terminals or SSH, and `session` knows nothing about any of them. Everything fiddly is testable without a
display, which is how the emulator got written.

## Looking at the pixels

Most of this is testable without a display, but the parts that are not —
a blurred panel, a rounded corner, a dialog drawn over the wrong thing —
are exactly the parts where a test tells you nothing useful. `-shot`
drives a real window through a short script and writes PNG files:

```
gridterm -shot "wait:60 key:ctrl+k wait:2 shot:palette.png"
```

The steps are `wait:<frames>`, `key:<chord>`, `type:<text>` and
`shot:<file>`, each taking one frame so that what a step did has been
drawn before the next one looks at it. Several `shot:` steps in one
script capture several states from one window launch. The window closes
when the script ends.

This found a bug that every test had passed over: the frosted panel was
drawing pure black, because a `SubImage` of the render target silently
draws nothing when used as a source on the Direct3D backend. Nothing
errored. It simply looked wrong, and nothing was looking.

## Design notes worth knowing

**Damage tracking is load-bearing.** `ebiten.SetScreenClearedEveryFrame(false)`
means a row the renderer skips shows the *previous* frame, not a blank.
A row wrongly considered clean is a visible bug, so `grid.Set` compares
before it writes and never dirties a row for content that did not
change.

**Nothing on a UI thread writes to a pty.** Writing to a pty blocks once
the program stops reading its input. Both the output pump — which holds
the terminal lock — and the ebiten thread produce input, so both queue
through a writer goroutine. Without that, a program that stops reading
wedges the whole window.

**Box characters are drawn, not looked up.** A font's box glyphs are cut
for that font's own advance width. Inside a terminal cell the strokes
stop short of the edges and adjacent cells do not meet, so every framed
TUI renders as a field of disconnected ticks.

**Host keys are never assumed.** A terminal that silently trusts an
unknown SSH host key can be man-in-the-middled and nobody finds out. An
unknown host gets a dialog showing its fingerprint, and only an explicit
yes records it. A key that does not match one already in `known_hosts`
is refused outright: there is no answer a user could give that would
make connecting safe. A `known_hosts` that cannot be read is an error
rather than an empty one, because a truncated list does not report a
host as unknown — it reports its key as changed.

## The ebiten fork

`go.mod` replaces ebitengine with
[marrasen/ebiten](https://github.com/marrasen/ebiten), a fork of
[unstablebuild/ebiten](https://github.com/unstablebuild/ebiten). `go
build` fetches it like any other dependency; there is nothing to check
out by hand.

Upstream ebitengine gives you polled `IsKeyPressed` plus
`AppendInputChars`, which cannot tell Ctrl+C from the letter c, nor an
OS key repeat from a fresh press. That rules out writing a terminal
against it. unstablebuild's fork adds `AppendInputEvents`, where each
observation is either a key transition or a committed code point, tied
together by an `InputSource` id — and it implements this properly on
Windows, in the Win32 message loop.

That fork does not itself compile for `GOOS=windows`: `initializeGLFW`
calls `glfw.InitHint`, which only the cgo glfw binding defines, so the
pure-Go Windows port fails to build. This fork is the same commit with
that one macOS-only call put behind a build tag — the branch is
`windows-build`, offered upstream. Once it lands there, the replace can
point back at unstablebuild's own tag.

ebitengine and both forks are Apache-2.0. Nothing here is derived from
the Rune IDE, which is GPL-3.0-or-later and keeps its renderer and
emulator under `internal/` where they cannot be imported.

## Known gaps

- **Fonts are chosen by file path, not by name.** `-font` takes paths.
  Matching a family name means reading the name table out of every font
  file on the system, grouping the four styles despite inconsistent
  subfamily strings, and rejecting proportional fonts; none of that is
  written yet.
- **A fallback glyph is always upright.** The system fonts consulted for
  runes the main font lacks are shared by every style, so CJK, braille
  and heavy box drawing stay regular even in bold or italic text.
- **Variable fonts render at their default instance.**
  `x/image/font/sfnt` does not apply variation axes, so asking such a
  font for its bold weight gets the default one.
- **Blink** is parsed and ignored.
- **Colour emoji** do not render. `x/image/font/sfnt` cannot read the
  bitmap tables that colour emoji fonts use.
- **Emoji ZWJ sequences and flags** show only their first glyph; the
  rest of the cluster is dropped rather than stacked in one cell.
- **OSC 52 clipboard writes** are parsed but not yet applied. Reads are
  deliberately never answered — replying would let any program that can
  write to the terminal exfiltrate the clipboard.
- **A click faster than one frame is missed.** ebiten reports the mouse
  as polled state, so a press and release inside the same 16 ms are
  never seen as either. No human manages it; a test harness does.
- **`-ssh` still needs its secrets up front.** That flag connects before
  the window opens, so there is nowhere to draw a dialog yet and the
  console is the only place left to ask. Connecting from inside the
  window asks in the window.
- **File panes share the width evenly, and the split cannot be dragged.**
  Five panes in an eighty-column window are sixteen columns each. Closing
  one gives its width back to the rest, but there is no way to make one
  pane wider than another.
- **Sixel and the Kitty graphics protocol** are not implemented.
- **An APC, PM or SOS string with no terminator grows without bound.**
  The parser buffers it before the emulator sees anything, so it cannot
  be capped from here; it needs a fix in `danielgatis/go-vte`, which
  already caps OSC the same way.
- `-e` splits its argument on spaces, with no quoting.

## Licence

MIT; see [LICENSE](LICENSE). The dependencies are all permissive:
ebitengine and `golang.org/x/*` are Apache-2.0 or BSD, `pkg/sftp` and
`kr/fs` are BSD, and `go-vte`, `go-pty`, `uniseg` and `atotto/clipboard`
are MIT.

## How this was built

Each step was reviewed adversarially before the next one started, which
is where most of the interesting bugs came from — a crash on a
one-column screen, two denial-of-service paths, a deadlock between the
output pump and a device report, and a reaper that threw away a short
command's entire output. The commit messages record what each review
found.
