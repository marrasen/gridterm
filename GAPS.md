# Known gaps

What is not there yet, said plainly. The
[issues](https://github.com/marrasen/kakel/issues) are the working
list; this is the part worth knowing before you try to use kakel for
something.

- **Split with runs no command.** The "Split with" choice offers a
  terminal on each machine; the "Command on <machine>…" lines, which
  would run a command in the new half, are still to come.
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
- **OSC 52 clipboard reads** are never answered: replying would let any
  program that can write to the terminal read the clipboard out.
- **A watched pane is not resized to suit the watcher.** The screen is
  drawn on the machine it is running on as well, and shrinking somebody
  else's shell to fit a pane they are not looking at would reach further
  than watching was asked to. So the size travels instead and the row
  says what it is; a screen wider than the pane showing it wraps.
- **Two kakel windows have to be the same build.** What one window
  says to another uses SSH's own encoding, which is positional: there is
  no room for a field one end knows and the other does not. A window of
  another build is refused by name rather than half understood.
- **The files of a machine the served window reached** are not offered.
  A window serves the files of the machine it is running on. Something
  it reached over SSH of its own is another hop, and nothing proxies it
  yet.
- **An agent is handed a screen, not a session.** It reads what is on
  the pane and types into it, the way a person looking over your
  shoulder would. A shell with shell integration on tells it when a
  command finished, what it exited with, and where that command's output
  began, so it can read the output on its own. A shell without it leaves
  it watching for the prompt to come back, which is a guess. Clearing the
  screen stops the agent reading what was above it; the screen it took
  away goes into the history, so you can still scroll up to all of it.
- **The port an agent reaches is the machine's, not the session's.** A
  loopback port on Windows is reachable by every session on the machine,
  not only by the one that opened it. Nothing gets past it without the
  code, which is not guessable and which you give out yourself, and
  anything that does not say what it is at once is hung up on. But it is
  a port, and it is open while a share has a pane in it.
- **Sixel and the Kitty graphics protocol** are not implemented. OSC
  1337 is the one this reads.
- **Only four megabytes of picture travel with a screen.** A pane may
  hold sixty-four pictures of sixteen megabytes each, and a whole screen
  is sent every time a window starts watching. The pictures past the
  budget are left out, and the watcher sees the text with a gap. A
  picture sent while somebody is already watching is not affected: the
  sequence carrying it is part of what the program said.
- **A path on a server is found one round trip late.** The machine is
  asked when the pointer first reaches the text, and the answer is what
  underlines it. Hold still for a moment and it lights up.
- **An OSC payload other than a picture is capped at a kilobyte.** The
  parser keeps that much and throws the rest away. The two sequences
  that carry a picture are read before it sees them, so they are whole;
  a clipboard write longer than a kilobyte is cut short.
- **An APC, PM or SOS string with no terminator grows without bound.**
  The parser buffers it before the emulator sees anything, so it cannot
  be capped from here; it needs a fix in `danielgatis/go-vte`, which
  already caps OSC the same way.
- `-e` splits its argument on spaces, with no quoting.
