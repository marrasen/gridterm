# The xpra spike

Step 2 of `../../REMOTE-APPS.md`: stand `go-xpra` up outside gridterm and
see what the frames and the window events look like.

It draws nothing. Each forwarded window is a buffer in memory, written
out as a PNG whenever its pixels change, and every call the library makes
into the desktop is logged. So it runs over SSH on a machine with no
display, which is how it was written.

## Running it

It is both halves, so nothing has to be installed and no xpra has to
exist anywhere:

```shell
go run . -serve 127.0.0.1:14500 &
go run . tcp://127.0.0.1:14500/
```

Against a real server, which has to be **Xpra 6.5 or newer** -- see
below:

```shell
xpra start :100 --bind-tcp=127.0.0.1:14500 --start=mousepad
go run . -click 18,14 tcp://127.0.0.1:14500/
```

`mousepad` because it is a GTK application with a real menu bar, and a
menu is the case the whole floating-window design turns on. `-click
18,14` lands on its File menu: the middle of a window is rarely a menu,
so the scripted click has to be aimed. Anything with menus does --
`xfontsel`, `xedit` and `xditview` are the ones in `x11-apps`.

That run produces three windows: the editor, the File menu as an
override-redirect popup, and a "Save Changes" dialog. Which is the whole
argument for a remote application being a pane plus some floating
windows rather than a pane.

## It needs Xpra 6.5 or newer

go-xpra declares `MinProtocolVersion` (6, 5) and always sends the modern
packet names. Its `BackwardsCompatible` switch only makes it *accept*
old names; it never sends them.

So an older server rejects everything this client says. Run against
Ubuntu 24.04's own Xpra 3.1.5 and the connection comes up, windows and
titles and the cursor all arrive -- and then every outbound packet is
refused:

```
Error: unknown or invalid packet type 'window-map'
Error: unknown or invalid packet type 'window-configure'
Error: unknown or invalid packet type 'display-configure'
Error: unknown or invalid packet type 'window-close'
```

Xpra 3 wants `map-window`, `configure-window` and `close-window`
(`xpra/server/mixins/window_server.py:405`). Because the map never
lands, the server never learns the windows are on screen and never
paints: the session runs to the end with zero frames.

Ubuntu, Debian and Fedora all ship versions far below the floor. The
official repository is the only way to a new enough one:

```shell
sudo wget -O /usr/share/keyrings/xpra.asc https://xpra.org/xpra.asc
sudo wget -O /etc/apt/sources.list.d/xpra.sources \
  https://raw.githubusercontent.com/Xpra-org/xpra/master/packaging/repos/noble/xpra.sources
sudo apt update && sudo apt install xpra
```

`go test ./...` runs the same session over a pipe and checks the pixels
that come back. It needs no network and no xpra.

## What the fake server does

`fake.go`, and the point of it is the parts go-xpra's own mock server
leaves out. It plays one application:

- A main window, painted whole as raw B,G,R,X with a padded rowstride,
  then patched in PNG, WebP and JPEG. Red, green and blue, so a channel
  swap cannot go unnoticed.
- A session cursor and a window icon, the two things that arrive as PNG
  in packets of their own.
- **A menu**, as an override-redirect window hanging past the main
  window's right edge. That is the case a pane cannot hold, and the
  reason `REMOTE-APPS.md` says a remote application is a pane plus some
  floating windows.
- A title change, a bell, a raise and a server-driven move.

Every stage is triggered by something the client sent, not by a sleep. A
click opens the menu; a geometry change makes the application reflow.
So the hand-run session and the test tell the same story.

WebP is the one frame that is a file, in `testdata/`: Go has no WebP
encoder, in the standard library or in `x/image`.

## The coordinate scheme

`place.go` is the other half of the spike, and it has nothing to do with
the protocol. It is the mapping `REMOTE-APPS.md` proposes between
gridterm's window of pixels and the single desktop an xpra session
believes it is drawing on, written out so it can be tested before
anything in gridterm depends on it.

Most of it is identity, and that is the argument for the scheme rather
than an accident of it: an application's main window is placed at its
pane's own box, so a menu's absolute position is already where it goes
on screen, and a pointer position needs no conversion in either
direction.

What is left over is what the tests are about:

- **A menu that would fall off an edge** slides back in, and never
  shrinks. A menu narrowed to fit is a menu with its labels cut off.
- **A menu hanging over a pane that is not its own takes the click.**
  Searching the panes first would send it to the pane underneath, and
  menus are always opened near an edge. `place_test.go` checks this
  against boxes made up on the spot; `TestSession` checks it again
  against boxes that came off the wire.
- **The gridterm window being resized** moves each pane's window, so
  the application reflows, and leaves floating windows where the user
  put them unless the window shrank past them.

## The keyboard

`keys.go` maps gridterm's 63 named keys to X11 keysyms, and `keys_test.go`
checks every name and value against `testdata/keysyms.txt`, which is
extracted from `/usr/include/X11/keysymdef.h`. A keysym name is not
guessable -- Page Up is `Prior`, Page Down is `Next`, Return is not
`Enter` -- and a wrong one fails silently on the far side.

It also handles gridterm reporting each keystroke twice, once as a key
and once as the text it produced. Sending both types every letter twice,
so the named key wins and the text of the same `Source` is dropped.

`-type` sends a string through that mapping, so a run against a real
server tests what gridterm would rely on rather than a stand-in.

It works. Against Xpra 6.5.3, typing

    The QUICK brown Fox; #1 @ 50% (x*y) [a] {b} "c" <d> ~e|f/g?

arrives character for character.

Getting there needed three things at once -- the resolved keysym rather
than the base key, modifier keys synthesised as real presses, and a
keycode -- and each alone looks like a broken protocol. See the note at
the top of `keys.go`.

The keycodes come from `layout.go`, a keyboard this client makes up and
declares to the server. Every keysym gets a key of its own, unshifted,
because gridterm never learns which physical key produced a character --
only which character, and which modifiers were down. Both facts go out
and the far application sees exactly that. It needs the `keymap-upload`
branch of the go-xpra fork; released go-xpra sends no keymap at all.

## Flags

- `-serve` run the fake server on this address instead of connecting.
- `-out` where the PNGs go. `frames/` by default.
- `-click` aim the scripted click, as `x,y` in desktop pixels. The
  middle of the first window by default.
- `-type` type this once the first window has focus.
- `-drive` send a scripted click, keystroke and resize once the first
  frame arrives, so the outbound half of the protocol is exercised too.
  On by default.
- `-quit` disconnect after this long. 15 seconds by default, `0` waits.
- `-v` log every packet go-xpra did not handle.

## The desktop size

This module points at a fork of go-xpra: `marrasen/go-xpra`, branch
`desktop-resize`. The `replace` in `go.mod` is what does it.

Released go-xpra tells the server how big the client's desktop is once,
in the hello, and never again. A server sizes its virtual display from
that and keeps it for the life of the connection.

For a desktop backend that is fine -- monitors rarely move. It is not
fine for gridterm, whose entire desktop is one window somebody drags
about. The server would go on placing windows, and letting remote
toolkits place their menus, against a screen that is no longer there.

The fork adds `ui.DesktopResized` and sends a `display-configure`
carrying `desktop-size` when one arrives, which is the packet that makes
a server resize its virtual display. `TestSession` checks both halves:
the size in the hello, a fresh one after the window is resized, and that
reporting the same size twice sends nothing -- a window being dragged
reports one on every frame.

## Why it is a module of its own

gridterm's `go.mod` does not name `go-xpra`, and nothing here is in a
release build or in `go build ./...` at the top of the repo. Deleting
this directory undoes the spike completely.

## What it is not

Only `tcp://`. The library takes any stream, so the other transports are
a dial and no more -- and the one gridterm would really use is its own
SSH session running `xpra _proxy`, handed to `protocol.New`, which is not
a URL at all.

Nor has any of this run against a real xpra. The machine it was written
on has none and no way to install one.
