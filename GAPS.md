# Known gaps

What is not there yet, said plainly. The
[issues](https://github.com/marrasen/kakel/issues) are the working
list; this is the part worth knowing before you try to use kakel for
something.

- **A fallback glyph is always upright.** The system fonts consulted for
  runes the main font lacks are shared by every style, so CJK, braille
  and heavy box drawing stay regular even in bold or italic text.
- **Variable fonts render at their default instance.** gunim's text
  package sets no variation axes, so asking such a font for its bold
  weight gets the default one.
- **Blinking text** is parsed and ignored. The cursor blinks when a
  program asks; the text does not.
- **Emoji ZWJ sequences and flags** show only their first glyph; the
  rest of the cluster is dropped rather than stacked in one cell.
- **OSC 52 clipboard reads** are never answered: replying would let any
  program that can write to the terminal read the clipboard out.
- **Two kakel windows have to be the same build.** What one window
  says to another uses SSH's own encoding, which is positional: there is
  no room for a field one end knows and the other does not. A window of
  another build is refused by name rather than half understood.
- **A remote forward cannot go through a kakel window.** A tunnel or a
  SOCKS proxy to a machine beyond a window listens here, and works. A
  port that listens on the far machine needs a connection of this
  window's own to ask for it, so it is refused.
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
- **A command is split on spaces, with no quoting.** That goes for
  `-e` and for Run Command alike.
