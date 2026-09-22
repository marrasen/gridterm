# Showing a remote app inside gridterm

An idea, not a plan. One step of it is taken: there is a spike in
`spike/xpra`, and gridterm itself is untouched.

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

The part that matters, and which reading the code turned up: it already
has the seam. `ui.Display` and `ui.Window` are how the client talks to
the desktop, and it talks to nothing else. Five methods on one, ten on
the other, and six kinds of event going back. X11, Wayland and Win32 are
three implementations of that pair, chosen at run time, and gridterm
would be a fourth. Everything above the seam -- the handshake, the
packet decoding, the damage sequencing, the draw acknowledgements the
server waits on -- is not gridterm's to write.

`protocol.New` takes any `io.ReadWriteCloser`, so gridterm would not use
go-xpra's SSH either: that one shells out to the system `ssh`, and
gridterm already holds a connection it can run `xpra _proxy` over.

**The catch: xpra 6.5 or newer has to be installed on the far machine.**
That is the main argument against, and running the spike against a real
server made it worse than it looked. go-xpra declares a minimum protocol
version of 6.5 and always sends the modern packet names, so an older
server refuses everything it says.

Ubuntu 24.04 ships Xpra 3.1.5, and Debian and Fedora are no better. The
far machine needs xpra.org's own repository, which means root on a
machine gridterm otherwise reaches over SSH and nothing else.

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
is a shape the code already has -- `termpic.go` is a bare layer with a
`Picture` on it and no grid, positioned in pixels and added under the
modals, which is the shape exactly.

Three things in it would have to change, and they are small and known:

- `render.Picture` shrinks to fit and centres. A remote window is drawn
  at its own size at a place it was told, and never blown up or moved
  to the middle of anything.
- `termPic.take` builds a whole new texture with
  `ebiten.NewImageFromImage` whenever the picture changes. That is
  right for a thumbnail and wrong for a window at thirty frames a
  second: a remote window wants one texture kept, with each damage
  rectangle written into it.
- The pixels arrive as B,G,R,X with the X undefined, which is what an X
  framebuffer and a Windows DIB both are. ebiten wants R,G,B,A. So one
  pass per damage rectangle to swap the ends and force the alpha
  opaque. The format is hardcoded in go-xpra's hello and is not a
  client option, so asking for R,G,B,X instead is a change upstream.

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

## The spike

`spike/xpra` is steps 1 and 2 of the list this section used to hold. It
is a go-xpra desktop backend that draws nothing: every window is a
buffer in memory, written out as a PNG whenever its pixels change, and
every call the library makes is logged. It runs headless, which is how
it was written -- on a machine with no display and no xpra installed.

It is a module of its own, so `go-xpra` is not in gridterm's `go.mod`
and none of it is in a release build. Deleting the directory undoes the
whole thing.

It is both halves, so the round trip runs with nothing installed:

```shell
go run . -serve 127.0.0.1:14500 &
go run . tcp://127.0.0.1:14500/
```

`go test ./...` in that directory runs the same session over a pipe and
checks the pixels that come back.

### What it showed

A whole session, both directions. One application: a main window painted
whole as raw B,G,R,X and then patched in PNG, WebP and JPEG, a session
cursor, a window icon, a menu, a title change, a bell, a raise and a
server-driven move. Focus, a click and a geometry change went the other
way, and each of them made the server do the next thing. Then the client
asked for the window to close and the server tore the session down.

All three compressed encodings decoded to the right colours, so nothing
in that path swaps red and blue.

Five things worth having written down:

- **Damage arrives padded and offset.** `rowstride` is the server's
  choice, rounded up to four bytes and free to be wider still.
  `ui.Convert` handles the stride, the bounds and the format, which is
  why the backend has no pixel arithmetic in it. Do not assume packed.
- **Pointer coordinates are absolute**, in the server's own desktop
  space, and so are window positions. That is the thing gridterm has to
  answer for; see below.
- **A resize goes out as an event, not as a call.** A backend emits
  `Configure` and the client calls `Resized` back. The spike did it the
  other way round at first, and the client compared the new geometry
  against `Geometry()`, found them equal, and sent nothing.
- **Alpha is not sent.** The client tells the server `transparency:
  false`, so the fourth byte is undefined. A backend handing the buffer
  straight to a texture gets garbage.
- **A popup is not mapped by the client.** `handleNewWindow` sends
  `window-map` for an ordinary window, which is what makes the server
  paint it, and deliberately does not for an override-redirect one --
  the server damages those itself and warns if a map arrives.

## What a real server showed

The spike has been run against a real xpra: 6.5.3 from xpra.org's own
repository, forwarding `mousepad` and `xfontsel`. It works. Real frames,
real input, real window lifecycle, and no packet the server refused.

**The desktop size negotiation does what it was written to do.** The
session's virtual display started at 3840x2160. The spike declared
1024x768 in its hello and then reported 768x576 after a resize, and
`xdpyinfo` on the far display followed each one exactly. That is the
fork's `display-configure` reaching `_apply_desktop_size`, confirmed
rather than assumed.

**A real toolkit's menu is an override-redirect window.** Clicking
mousepad's File menu produced one: 281x383 at 0,27, its own window with
its own pixels, destroyed again when the menu closed. The spike's
stacking order routed the next click into it rather than to the window
underneath. So the design this document argues for is now tested against
GTK and not only against a fake server written to agree with it.

**One application really is several windows.** That session had three:
the editor, the menu, and a "Save Changes" dialog at 561x118 that
arrived as a separate top-level window of its own. A pane holds one
thing; this needs three.

**An application reflows to a Configure.** Told the pane was narrower,
both mousepad and xfontsel repainted themselves at the new size. That is
the loop a pane in the grid depends on.

Cursors arrive with hotspots (24x24, hotspot 5,1) and windows carry
icons.

### What is still untested

**Every frame arrived as raw B,G,R,X.** Over a loopback socket xpra
never bothers to compress, so JPEG, PNG and WebP decoding has still only
been exercised against the spike's own fake server. Forcing the issue
does not work either: a server restricted to `--encodings=jpeg` refuses
the connection outright, because its own video subsystem then excludes
jpeg and finds no encoding in common.

**A backend cannot see the wire encoding anyway.** `ui.Window.Paint` is
handed decoded pixels and a pixel format, never the coding they arrived
in, so even when a server does send JPEG the spike cannot say so. Only
the server's log knows.

### And what an old server showed

Before 6.5.3 there was Ubuntu 24.04's own Xpra 3.1.5, which is worth
recording because it is what anybody gets by typing `apt install xpra`.
The connection came up and windows, titles and the cursor all decoded --
and then every outbound packet was refused as an "unknown or invalid
packet type". Xpra 3 spells them `map-window`, `configure-window` and
`close-window`. Because the map never landed the server never painted:
the whole session ran with zero frames.

It found two bugs in the spike, both of a kind only a real server
produces: the scripted input ran once per top-level window rather than
once per session, and a close request to a server that ignores it left
the spike waiting for ever.

## The keyboard

Step 6 was written down as "a table of X11 keysym names, mechanical, the
largest single piece", and flagged as the sleeper risk. Both halves of
that were wrong, in opposite directions.

The table is in `spike/xpra/keys.go`. gridterm names 63 keys, every one
is mapped, and every name and value is checked against
`/usr/include/X11/keysymdef.h` rather than typed from memory. That was
an afternoon.

**And it works.** Typing `Hello, World! 42` into `mousepad` over Xpra
6.5.3 arrives exactly.

Getting there took three things at once, and each one tried alone looks
like a broken protocol:

1. **The resolved keysym, not the base key.** `"A"`, never `"a"` with
   shift held. gridterm reports a keystroke twice -- as a key and as the
   text it produced -- and only the text knows what the layout made. So
   a key that types waits for its text, and the table is for keys that
   type nothing.
2. **Real modifier key presses.** gridterm names no modifier key: there
   is no `input.KeyShift`, only a bitmask riding along with other
   events. Those presses have to be invented, and each one has to
   describe the state *before* itself, the way X does.
3. **A keycode.** Without one the server resolves the name to a keycode
   itself and then types it at the wrong level, whatever the modifier
   list says.

With any one of the three missing, `Hello, World! 42` arrives as
`hello, world1 42` or worse. That is what makes it a sleeper: every
partial answer looks like the protocol failing rather than like a
missing ingredient.

### What is left, and it is real

The keycodes in the spike are a US-layout table read off the test server
with `xmodmap`. **gridterm has no X keymap to take keycodes from** -- it
is ebiten, not an X client -- so that table is a stand-in that would not
survive a server with a different layout.

The real answer is to upload a keymap and use our own keycodes, which is
what every other xpra client does. go-xpra cannot: its hello carries
`keyboard: true` and nothing else. So step 6 is the table, which is
done, plus keymap upload in go-xpra, which is not started.

That is a missing capability rather than a bug. go-xpra's own X11 client
types perfectly -- confirmed by building it, running it on a scratch
Xvfb against the same session, and typing into it with XTEST. It works
because X hands it real keycodes for free. A backend without an X
keyboard underneath it has nowhere to get them.

## Is a remote window a pane, or a floating thing?

Both, split by what the window is for. The protocol settles it.

`NewWindow` carries an `overrideRedirect` flag, because xpra forwards
menus, tooltips and combo drop-downs as windows in their own right. They
sit at absolute positions and they routinely hang past the edge of the
window that opened them. A menu clipped to a pane is a broken program.
So one remote application is several windows and a pane holds one thing.

The spike tests this rather than assuming it. Its fake server opens a
menu when the client clicks, as an override-redirect window whose right
edge is sixteen pixels past the main window's, and the test fails if
that menu ever fits inside.

That gives:

- The application's main window is a pane. It takes the pane's size,
  and the pane's size is reported back with `Configure` so the program
  reflows to it. Splits, the switcher, pane titles and focus all work
  because it is a widget like the others.
- Everything else -- the override-redirect popups, and the program's own
  dialogs -- floats above the grid, clipped to the gridterm window
  rather than to the pane.

Then the coordinates fall out, and this is the part worth getting right
first. xpra has one absolute space for window positions and pointer
positions alike. Make that space the gridterm window's own pixels: put
the application's main window at the pane's position in the window, and
a popup's absolute position is already where it goes, with nothing to
translate. A pane that moves is a `Configure`.

`spike/xpra/place.go` is that scheme written out and tested. Most of it
is identity, which is the argument for it rather than an accident. Three
things are not:

- **A menu that would fall off an edge** slides back inside and never
  shrinks -- a menu narrowed to fit has its labels cut off and the
  program was never asked. Declaring the desktop size is what lets a
  toolkit avoid the case itself; this is the fallback.
- **A menu hanging over a pane that is not its own takes the click.**
  Only the stacking order says so, and searching the panes first would
  send the click to the pane underneath. This is checked twice: against
  boxes made up on the spot, and against boxes that came off the wire
  through go-xpra's own bookkeeping.
- **The gridterm window being resized** moves each pane's window so the
  application reflows, and leaves a dialog where the user put it unless
  the window shrank past it.

The gap that blocked it was half of a conversation. go-xpra's released
v0.2.2 said nothing about the desktop size at all; its master since has
grown `ui.DesktopSizeProvider` and puts a `desktop_size` in the hello,
which every backend now fills in. What none of it does is say so a
second time. The size goes out once at connection time and is never
mentioned again.

That is survivable for a desktop, whose monitors rarely move. It is not
survivable here, because gridterm's whole desktop is one window somebody
drags about: the server would go on placing windows, and letting remote
toolkits place their menus, against a screen that is no longer there.

`marrasen/go-xpra`, branch `desktop-resize`, is that half written --
`ui.DesktopResized`, and the `display-configure` packet it sends, which
is what makes a server call `_apply_desktop_size`. The spike points at
it and checks the size arrives both at connection time and after a
resize.

## The gate

Before any of the below: **is needing xpra on the far machine
acceptable?**

gridterm works against anything that answers SSH. This would be the
first thing that does not, and no amount of the work below changes that.
If the answer is no, none of it happens and the RFB cousin near the top
of this document is what gets looked at instead.

Nothing in gridterm should start until that is settled. Everything done
so far is a spike in a module of its own, and deleting `spike/` undoes
all of it.

## What a plan would be

Nine steps, if the gate opens. They are written down because the
question "how big is this?" deserves an answer, not because anybody has
agreed to do them.

Two go first, because they are cheap and either one can invalidate what
follows:

1. **Keep the server's idea of the desktop up to date.** Written, on a
   fork: `marrasen/go-xpra`, branch `desktop-resize`, and confirmed
   against a real 6.5.3 server -- the far display resizes to match. It
   is not upstreamed, and no desktop backend emits the new event yet:
   each needs its own source for it, RandR or `WM_DISPLAYCHANGE` or
   `wl_output`.
2. **Run the spike against a real xpra 6.5 or newer.** Done, against
   6.5.3 -- see "What a real server showed". What is left of it is the
   compressed encodings, which a loopback server never sends.

Then the spine, which shows nothing on screen:

3. **The texture path in `render`.** One kept texture per window with
   damage rectangles written into it, the B,G,R,X to R,G,B,A pass with
   the alpha forced opaque, and a picture drawn at its own size where it
   was told rather than shrunk and centred. Testable without a display,
   the way `render` already is.
4. **The transport.** Run `xpra _proxy` over the SSH session gridterm
   already holds and hand the stream to `protocol.New`. Small: the
   connection model is built and tested.
5. **The backend.** A real `ui.Display` and `ui.Window` over the layer
   work in 3, replacing the spike's headless pair.

Then the parts that can be seen:

6. **The keyboard.** The table is done and typing works against a real
   server -- see "The keyboard" above. What is left is uploading a
   keymap, in go-xpra, so that the keycodes are ours rather than a
   guess at the far machine's layout. The ebiten fork's press-and-release
   pipeline is the only reason any of it is possible; see the note in
   `go.mod`.
7. **The pane.** The application's main window as a `ui.Widget`:
   `Layout` reports the pane's size back as a `Configure`, and focus
   becomes a `ui.Focus`. The first step with anything to look at.
8. **Floating windows.** Popups and dialogs as layers over the grid.
   The coordinate mapping this needs is already written and tested in
   `spike/xpra/place.go`; what is left is the layers and the clipping.
9. **The session.** Opening one, what a pane shows while it connects,
   what happens when the connection drops, the pointer cursor and the
   window icon in the pane title.

### Where it would go wrong

**Step 6 was the sleeper and it went off, then turned out to be
survivable.** Typing works; what is left is the keymap upload. The
lesson worth keeping is that every partial answer looked like a broken
protocol.

**Step 8 is where the design might still be wrong.** The mapping is
tested, but only against a fake server written to agree with it. A real
toolkit placing a real menu is the thing that would say.

**And from step 3 onward this is a second product.** A display protocol
client owns a compatibility matrix for ever, maintained beside a
terminal. That warning is made further up this document about writing an
X server, and it is only a little softer here.
