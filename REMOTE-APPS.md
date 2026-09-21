# Showing a remote app inside gridterm

An idea, not a plan. Nothing here is started.

gridterm already holds SSH connections to a set of machines, already
draws layered pictures over panes, and already routes a keyboard and a
mouse into a pane. A window from a program running on one of those
machines, sitting in the grid next to the terminals, is a smaller step
from here than it looks.

## The short of it

Be an xpra client. Do not write an X server.

xpra is sometimes called "screen for X": it runs a graphical program on
a machine somewhere else and hands the window to a client, one window at
a time rather than a whole desktop, and the session survives the client
going away. Close the laptop, reconnect tomorrow from somewhere else,
and the program is still running with its state. `ssh -X` cannot do
that: the connection ends and the program dies with it.

The split is what makes this cheap:

- **The far machine** runs the X server and the program. That is xpra's
  own server, already written and maintained by somebody else.
- **gridterm** receives "here is window 3, here are its new pixels, it
  moved" and sends keys and clicks back.

So the entire X server -- resources, extensions, XKB, RENDER -- is code
never written here.

`github.com/Xpra-org/go-xpra` is a client of that protocol in pure Go,
no cgo, and it already speaks TCP, TLS, WebSocket, Unix sockets and SSH.
It covers window create, destroy, move and resize, keyboard, mouse and
clipboard, and RGB, JPEG, PNG and WebP frames. It leaves out h264,
audio, tray icons and rich clipboard formats.

**The catch: xpra has to be installed on the far machine.** That is the
main argument against, and it is a real one. gridterm otherwise works
against anything that answers SSH.

A cheaper cousin, if the install turns out to be the blocker: RFB, which
is VNC's protocol. The core of it is a few hundred lines and it needs
only a VNC server over there. It hands over one framebuffer for a whole
screen rather than a window at a time, which is worse for putting a
single program in a pane.

## What is already here

- `render` has layers, pictures and compositing onto an ebiten image.
  The drawing half exists.
- OSC 1337 and OSC 1338 mean pictures inside panes are already plumbed,
  including pictures that arrived from another window over the wire.
- The SSH transport, the machine list and the connection model are
  built and tested.

A remote window would be a picture layer that takes input. Most of that
is a shape the code already has.

## The other one, for fun

Writing an X **server** in Go, rendering through ebiten, so gridterm is
the display server and remote programs are its windows.

It is not mad. `SwiftX11` is exactly this in Swift with Metal rendering:
one person, 788 commits, twelve extensions, and it runs xterm, xeyes,
xcalc and xclock as well as Xilinx Vivado with menus, dialogs, clipboard
and drag.

Two things are worth knowing before anybody starts.

**The pure-Go X11 code in ebiten does not help much.** It is a client:
about 7,900 lines of talking *to* a server. None of the server's own
work is in there -- the window tree, resource ids, focus, grabs,
selections, how an event finds its way to a window. What does carry over
is the wire format, which is generated from the protocol's XML
descriptions, and a generator runs in either direction.

**gridterm is oddly well placed for it.** An X server has to send real
KeyPress and KeyRelease events with modifier state and repeat. A polled
`IsKeyPressed` cannot express that, so an X server cannot be built on
upstream ebiten's input model at all. gridterm carries a fork of ebiten
for exactly that pipeline -- see the note in `go.mod`. The dependency
that makes upgrades awkward is the prerequisite for this.

Guess at the size: 20,000 to 40,000 lines of Go for something that runs
a real toolkit application. The core protocol and resource handling is
the bulk, RENDER is where modern text and compositing actually happen
(and the glyph atlas for it is already here), and XKEYBOARD is nastier
than it looks because libX11 leans on it.

Where it would hurt:

- **No GLX.** SwiftX11 does not do it either. That rules out browsers,
  Electron and anything that wants the GPU, which is a lot of what
  people would want to run.
- **It is a second product.** A display server owns a compatibility
  matrix for ever, maintained beside a terminal.
- **X11 has no isolation between clients.** Any client can read any
  other client's input. A gridterm listening for remote X clients is a
  trust boundary, and worth thinking about before it is a feature.

## If this is ever picked up

1. Read `render/layer.go` and `vt/wirepic.go`. A remote window is that
   shape with input attached.
2. Stand up `go-xpra` against a machine running `xpra start`, outside
   gridterm, and see what the frames and the window events look like.
3. Then decide whether a remote window is a pane in the grid or a
   floating thing over it. That is a question about the window manager
   gridterm already is, and it is the interesting design work here.
