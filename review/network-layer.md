# The network layer

Packages `remote` (SSH), `serve` (gridterm to gridterm), `agent` (the
pane handover) and `mcp` (JSON-RPC over stdio), plus the main-package
call sites that reach them. Every non-test file read. `go vet` is clean
on all four.

## Summary

This layer is in much better shape than the main package. The hard
things -- host key checking, timer races, cancellation of dialogs, close
ordering, the take-over protocol's authentication -- are done carefully
and explained. The problems are a small number of specific bugs, three
of which can freeze the window, and a body of duplicated logic between
`remote` and `windows.go` that has already produced one inconsistency
(two different timeouts for the same agent).

Findings 1 to 3 are the ones a user hits. Finding 1 was verified by
reading the code: it is real and reachable.

## Findings, most serious first

### 1. A lock held across a blocking write, and `Close` wants it -- the window freezes

`remote/shell.go:181-186` (`Write`), `remote/shell.go:223-227`
(`closeAll`). **Verified.**

`Write` holds `writeMu` for the whole of `s.stdin.Write(b)`. That is a
write to an SSH channel, and it blocks on the channel's flow-control
window -- indefinitely, if the far program has stopped reading its input.
`closeAll` then takes the same lock. The write runs on the terminal's
`writeLoop` goroutine, but `Terminal.Close` (`ui/term/term.go:212-218`)
runs on the goroutine that draws. So closing that pane, closing the
machine, or quitting parks the drawing goroutine for good.

`serve/client.go:425-428` documents this exact hazard and deliberately
avoids sharing the lock with Close: *"a lock shared with Close is how a
pane stops being closeable"*. `remote` does the opposite.

Closed by 0b03bf8 Stop the window freezing on a machine that has stopped answering.

### 2. Channel opens run on the drawing goroutine with nothing bounding them

`remote/shell.go:91` (`NewSession`), `remote/files.go:42,63-76`
(`NewSession`, `RequestSubsystem`, `sftp.NewClientPipe`),
`remote/tunnel.go:278` (`client.Listen`).

Each is a request-and-wait round trip to the far machine with no
context, no deadline and no timer. The callers are UI commands on the
draw goroutine (`browse.go:120`, `tunnels.go:215`, `machines.go:828`).
A host that is TCP-alive but not answering -- a suspended VM, a wedged
sshd, a dropped path with no reset -- freezes the whole window on "open
a file pane". `Conn.reach` (`remote/conn.go:192-196`) shows the right
shape, a `context.WithTimeout` per operation, and none of these three
use it.

Closed by 0b03bf8 Stop the window freezing on a machine that has stopped answering.

### 3. The agent and MCP client has no read bound at all

`agent/client.go:172`, `mcp/mcp.go:138`.

`Client.say` sets no deadline after the dial; the only bound on an
answer is `Close` from another goroutine, which the MCP process has
nothing to call. A window that accepts, greets and then stops answering
parks the tool call for ever. Eight of those fill `answersAtOnce`, the
ninth is answered inline, reading stops, and `Serve`'s `defer
running.Wait()` means the process never exits even on end of file.
The dial itself is bounded at five seconds (`agent/client.go:47`); the
conversation after it is not.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 4. Server-chosen text goes into the pane's terminal unfiltered

`ask.go:266` → `connlog.go:84-93` → `connlog.go:145-152`, against
`console.go:70-84`.

The console path runs banner and keyboard-interactive text through
`plainly()` because it goes to a real terminal that obeys escape
sequences. The pane path writes the same server-controlled bytes into
`connLog`, which the pane parses as a terminal stream. A hostile server
can move the cursor, overwrite the lines above it -- including "its host
key is accepted" -- recolour, and set the window title. OSC 52 is inert
because the clipboard hook is never wired, so this is spoofing, not
clipboard theft. The far end's refusal text in `serve/client.go:314`
has the same gap and arrives prefixed "gridterm: ", so it reads as this
window's own words.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 5. House rule broken: an unreadable default key file is silently skipped

`remote/auth.go:424-439`, via `remote/auth.go:446-448`.

`readIdentity` returns the `os.ReadFile` error; `identities` drops it
for any path that was not explicitly named. Not returned, not logged,
not even passed to `Saying`. An `~/.ssh/id_ed25519` that cannot be read
-- wrong owner after a restore, an ACL, a mount that went away --
produces a connection that quietly offers fewer keys and then fails
with "no supported methods remain". The comment justifies skipping a
stale `id_rsa`, but the code cannot tell "not there" from "cannot be
read": `fs.ErrNotExist` and `EACCES` take the same branch.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 6. A documented check on attach is not implemented

`serve/server.go:312-318`, `serve/client.go:222-233`, `serve/wire.go:55`.

Both doc comments say the whole description is sent back and the
server checks that the place still holds what the client was told. The
wire carries only the ID, the `Attacher` takes only the id, and
`attach.go:47-58` looks up by ID and checks nothing else. Today the
risk is theoretical -- registry ids are a monotonic counter, never
reused -- but two comments assert a defence that does not exist, which
is how it stays absent when ids become anything else.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 7. Close discipline: the discards that hide something

The rule is that a close error is reported. These swallow a real signal:

- `remote/shell.go:225` -- `_ = s.stdin.Close()`. This *is* the polite
  hangup; if it fails the far shell never sees EOF, and `closeAll`
  returns only `sess.Close()`'s error.
- `serve/host.go:308-309` -- `CloseWrite` and `Close` in `endSession`,
  with `s.onError` right there and used three lines above.
- `serve/control.go:171,183` -- the control channel's close, with
  `onError` available.
- `remote/conn.go:260` -- `_ = client.Close()` for a connection
  cancelled mid-handshake. `windows.go:171-173` reports the identical
  case; this one does not.
- `remote/tunnel.go:460-461` -- per-stream closes dropped, while
  `closeAll` (`remote/tunnel.go:609-613`) gathers the same closes for
  live streams.
- `serve/identity.go:168` -- a bare `f.Close()` on the host-key write
  path.
- `windows.go:369` -- a bare `defer closer.Close()` on the agent socket.
- `remote/auth.go:522,530` -- the agent socket's close after a failed or
  timed-out read; the one place a wedged agent socket would show.

Defensible, and mostly said so in a comment: `remote/dial.go:187,216,
234`, `serve/client.go:128,152,155,160,164`, `serve/server.go:249,281,
287,299,345,355,366`, `agent/server.go:161`, `remote/book.go:510,517`,
`remote/shell.go:173,239`.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 8. House rule: disk errors with a fallback substituted

- `remote/book.go:496-498` -- `filepath.EvalSymlinks` failing falls back
  to the original path. Usually harmless; a permissions failure mid-path
  silently reverts to the behaviour the comment was written to prevent,
  replacing a symlink instead of what it points at.
- `remote/hostkey.go:199` -- `defer os.Remove(tmp.Name())` on the staged
  known_hosts. A remove that fails leaves a copy of the user's host keys
  in the system temp directory with nothing said.
- `remote/hostkey.go:236` -- close after a read, where a close error is
  the only hint the file was truncated underneath.
- `mcp/mcp.go:191-200` -- `Encode`'s error dropped. The comment ("reading
  will find the same thing") holds for a broken pipe and not for a
  failed write on a still-open stream; the client waits for an id that
  will never be answered.

Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.

### 9. Duplicated logic, and where it has already diverged

- **`AgentKeys` vs the agent rung** (`remote/auth.go:505-534` vs
  `:286-324`). Same job -- list the agent's keys, bound the wait, close
  the socket to unblock it, remember the failure -- written twice, with
  **two different timeouts**: 5 s for one, 10 s for the other. The same
  agent gets twice as long depending on whether you open a machine or
  take over a window.
- **`keysFor` vs `authMethods`** (`windows.go:400-484` vs
  `remote/auth.go:233-351`). `keysFor` re-implements the whole ladder,
  including a verbatim copy of the "leaving the SSH agent alone…"
  message. It also differs in kind: it offers everything in one
  `[]ssh.Signer`, which is exactly the "only the first publickey method
  is tried" problem `remote/auth.go:88-91` exists to solve.
- **Two ways to read known_hosts.** `remote/hostkey.go:249-267` parses
  every line itself, then writes the survivors to a temp file so
  `knownhosts.New` can parse them again.
- **`closeFilesOver`** (`windows.go:623-651`) and `remote/files.go:106-127`
  are the same drain-then-close with the same 250 ms grace, named
  `filesGrace` in one place and `drainGrace` in another.
- **Coupling by string.** `remote/shell.go:33-35` decides a far end is a
  gridterm by `strings.Contains(err.Error(), "session@gridterm")` --
  sniffing `serve/wire.go:18`'s constant through an error message.
  Rename the channel and `remote` silently stops recognising it.

Closed by 1707651 Fold the duplicated pieces the review named into one of each.
Closed by e1de8df Keep the take-over's passphrase dialog and cancel working under one ladder.

### 10. What bounds each blocking operation

Bounded: TCP dial (20 s + ctx); version and key exchange (20 s + ctx);
agent listing (10 s or 5 s, closing the socket); agent signing (60 s,
no blame); SOCKS greeting and request (30 s); tunnel stream dial (20 s);
serve dial and handshake (15 s + 20 s + `AfterFunc`); serve-side
handshake (30 s); agent hello (10 s); agent wait (5 min cap); shell and
files close drain (250 ms).

Bounded by nothing:

- Authentication after the host key -- deliberate and correct, a
  passphrase dialog is behind it; but `main.go:411` passes
  `context.Background()` for the `-ssh` startup path, so there is no
  cancellation at all there.
- Session, channel and subsystem opens -- finding 2.
- `Files.closeAll`'s second wait (`remote/files.go:119`). **Speculation**
  that closing the channel always unblocks it; nothing guarantees it.
- `dialAgent` -- `net.Dial("unix")` and `os.OpenFile` on the named pipe
  have no timeout. A pipe server that accepts and stalls blocks before
  any of the agent timers start.
- `agent.Client.say` -- finding 3.

### 11. Concurrency, the rest

- **`sync.Once` around a blocking call.** `remote/shell.go:200`
  `waitOnce.Do(s.sess.Wait)`: a second `Wait` blocks *inside* `Do` for
  the life of the session rather than returning the cached answer.
  Documented as "idempotent", which reads as "returns immediately". Same
  shape at `remote/shell.go:216` and `remote/files.go:94`.
- **Off by one.** `remote/tunnel.go:383` checks `held() > maxStreams`
  after holding, so the cap is 257.
- **SOCKS slot exhaustion.** 256 idle connections to a dynamic tunnel's
  port occupy every slot for 30 s at a time. Anything on the machine can
  do it; a tunnel on 0.0.0.0 widens that to the network.
- **Timers are the best-handled area.** `remote/auth.go:190-219` handles
  an `AfterFunc` racing its replacement with a generation counter, which
  `Stop`'s return value cannot express; `remote/dial.go:135-151` and
  `serve/server.go:299,341` read `Stop` and act on `false`.
- **Locks across blocking calls** are avoided everywhere except finding
  1, each time with a comment saying why.
- **Every hand-off channel is buffered to one with exactly one sender**,
  so no sender can block on a reader that left. The one goroutine that
  can park for ever (`walkAway`) is documented and priced.

### 12. Security, the rest

- **Host key.** No path proceeds unverified. `loadKnownHosts` is skipped
  only when the caller set `HostKeyCallback` explicitly, which only
  tests do; a nil callback is left nil so x/crypto refuses; trust is
  never offered while any known_hosts line failed to parse; one
  implementation serves both the SSH and the take-over paths. The dial
  retry only narrows host key algorithms to what known_hosts already
  holds.
- **Take-over auth** is careful work: key-only; `VerifiedPublicKeyCallback`
  ties the name to the key that signed; certificates refused in the file
  and in the handshake; SHA-1 RSA and DSA dropped; an `authorized_keys`
  line with `from=` or `command=` fails the whole file rather than being
  honoured or dropped.
- **What a hostile far end can do to a client.** The refusal text
  (finding 4). `serve.Open`'s label, note, host, kind and state go into
  the sidebar with no length cap and no control-character filter --
  `remote/host.go:150-181` applies exactly that check to names the user
  types, so the standard exists and is not applied to the wire. A 4 MB
  label fits under the scanner limit. Spoofing, not execution.
- **Unvalidated address into `known_windows`.** The take-over dialog's
  address reaches `knownhosts.Line` with no equivalent of `badInHost`.
  Today the resolver refuses such a name first -- which
  `remote/target.go:44-45` itself calls "luck, not a check".
  **Speculation** that it is reachable; the check costs one line.
- **Agent handover.** The hello-first rule is the right defence and the
  right shape. The loopback listener is reachable by every session on a
  Windows machine (stated honestly at `agent/agent.go:26-29`). After the
  greeting the read deadline is cleared and never reinstated, so an idle
  connection holds one of eight slots indefinitely.
- **MCP.** A client can reach exactly nothing it was not handed. Two
  handovers to one agent are pooled in one process, so a code for
  window A keeps window B's connection alive -- the design, but worth
  knowing.
- **`handshakesAtOnce = 8`**: eight sockets that say nothing lock every
  legitimate client out for 30 s.

### 13. API shape

- Exported for exactly one caller in main: `remote.UsualKeys`,
  `remote.AgentKeys`, `remote.HostKeyCheck`, `remote.ErrAgentSilent`,
  `remote.GridtermWindowKind`. Each exists because `windows.go`
  re-implements a ladder `remote` already has. Fold `keysFor` into
  `remote` and four of the five stop needing to be exported.
- `remote` knows about the window: `GridtermWindowKind` is a string the
  dialog must match; `remote.ServePort` is `serve`'s port living in
  `remote`; `Host.Window` makes the SSH package carry a flag meaning
  "this is not an SSH host at all".
- The wire format is pinned to `meter.State.String()` and
  `conns.Kind.String()` with nothing asserting it.
- `remote.Config.HostKeyCallback` exists only for tests and is labelled
  as a hole; an unexported field set through `export_test.go` would
  remove it from the public surface.

## What is sound

- **`remote.Book` is the model the rest should be held to.** Re-reads
  before every mutation; refuses to save when it could not load, in one
  place; validates that what it writes can be read back; temp file,
  `Sync`, rename, remove on every failure; unknown fields, repeated
  keys, trailing bytes and the version each checked, each with a
  sentence saying which loss it prevents. No fallbacks anywhere.
- **Host key handling.** "Never seen this host" and "the key changed"
  are separated and worded differently; a failure to record a key fails
  the connection, because a question asked repeatedly stops being read.
- **`cancelAsk`** makes "the user said no" a guarantee rather than a
  race, and keeps the first reason.
- **Close semantics on `Conn`.** Riders closed in parallel before the
  transport, errors joined, a second caller waits rather than reporting
  a success that has not happened. The rider contract is stated and
  honoured by all three implementations.
- **`serve`'s watcher.** Latest snapshot only, never a queue; the drawing
  goroutine takes a small lock and never writes a socket.
- **Sizes from the wire are clamped by the end that has to honour
  them**, and only there, with the reason.
- **Refusals go on the channel's error stream, not into the program's
  bytes**, and the exit status is sent before the channel closes.
- **`ssh.DiscardRequests` on every accepted channel**, each time with the
  same one-line reason.
- **The host key on disk:** mode check on read, `CreateTemp` plus `Link`
  rather than rename so two windows cannot each believe they made the
  machine's identity, `%LOCALAPPDATA%` so a roaming profile does not
  copy the private key to a file server.
- **The comments say what breaks and when, not what the code does.**
  Nearly every non-obvious decision carries the failure it was written
  against. That is why this review could be specific.
