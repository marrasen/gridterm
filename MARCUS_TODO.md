What happens when I close the gridterm window? Will the app close all connections and processes gracefully?
I'm asking because I found hundreds of cmd.exe running on my system. I don't know if that's processes detached from 
gridterm or not but I'm guessing it is.

## Findings, 2026-09-17

Short answer: yes, they are gridterm's. Closing the window gracefully does the
right thing. The leak happens when gridterm dies *without* closing the window —
a crash, a panic, or a hard kill during development.

### What is on the machine right now

369 `cmd.exe` processes in total:

- 365 orphans. The parent process is gone.
- 2 live panes in the two running gridterm instances.
- 1 from a Claude Code session.
- 1 `cmd.exe /d /s /c vite` from 13 September.

The 365 orphans are gridterm's. The evidence:

- Each has the bare command line `C:\WINDOWS\system32\cmd.exe`, with no
  arguments. That is what `defaultShell` produces from `COMSPEC`.
- There are also 365 orphaned `conhost.exe` processes. Both sets share the
  **same 193 dead parent PIDs**. So 193 gridterm runs each left behind about two
  ConPTY-and-shell pairs.
- The creation dates track development: 1 on 13 Sep, 1 on 14 Sep, 128 on 15 Sep,
  236 on 16 Sep, 3 on 17 Sep.

They cost about 2.6 GB of working set, 365 threads and 30,660 handles. They burn
almost no CPU: 4.6 seconds in total across all 365 since 13 September. They are
idle, not spinning.

One caveat. The parent PIDs are gone, so the link to gridterm cannot be proved
directly. The matching orphan counts, the shared parent PIDs and the dates make
it close to certain.

### The graceful close path is correct

`local.Close` in `session/local.go:204` does the right thing on Windows:

1. `release` frees the pseudoconsole, which hangs the child up.
2. `closeReleased` shuts the pipes.
3. It waits `hangupGrace` (250 ms) for the child to be reaped.
4. If the child is still there, it kills it.

`main.go:114` calls `panes.Close()` on shutdown, which reaches every session. So
a normal window close does not leak.

### The real gap: nothing survives gridterm dying abruptly

Every one of those four steps runs inside the gridterm process. If gridterm never
gets to run them, the child `cmd.exe` is left with no console input and sits
there forever. That happens on:

- a panic,
- a `taskkill`, or the IDE stopping a `go run`,
- a rebuild-and-restart loop during development.

Windows has an answer for exactly this: assign each child to a **Job Object**
with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. The job handle dies with the gridterm
process whatever kills it, and the kernel then terminates everything in the job.
This needs no cooperation from gridterm's own shutdown path, so it holds even
during a crash.

Worth doing at the same time -- **this turned out to be wrong**, see below:

- `p.Kill()` at `session/local.go:228` terminates only `cmd.exe`, not its
  descendants. A pane running a build would leave the build behind. A job object
  fixes this case too.

### A second, smaller leak

The live gridterm PID 680780 has a `conhost.exe` child (712524) with no
`cmd.exe` under it. Its shell has exited but the console host is still running.
That is the same problem seen from the other end, and worth a look on its own.

### Cleanup command

This kills only the orphans and leaves live panes alone. Marcus has run it.

```powershell
$all = @{}; Get-CimInstance Win32_Process | ForEach-Object { $all[[int]$_.ProcessId] = $_ }
Get-CimInstance Win32_Process -Filter "Name='cmd.exe'" |
  Where-Object { -not $all[[int]$_.ParentProcessId] } |
  ForEach-Object { Stop-Process -Id $_.ProcessId -Force }
```

## What was done, 2026-09-17

The fix is in. Each Windows shell now starts inside a job object with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, so the kernel takes the shell down
whenever gridterm's handles close, however gridterm ended.

### Two things above were wrong

**A build running in a pane was never left behind.** It shares the pane's
pseudoconsole, and `releaseTerminal` closes that on the way through `Close`, so
the build already died. The only descendants that outlive a pane are the ones
started with a console of their own -- `start`, a GUI program, an installer.
Killing those is not a leak being fixed, it is taking away work the user
deliberately detached. So `Close` now takes kill-on-close back off the job
before closing it, and kills the shell alone as it always did. Two reviewers
caught this independently.

**The leak could not be reproduced.** A probe that starts three panes through
the real `session` package and is then hard-killed leaves nothing behind. It was
tried as a console program and as a GUI one, with idle shells and with busy
ones. When gridterm dies its pipe handles close, the console host notices and
exits, and the shell goes with it. That cascade worked every time.

So the path that made the 365 orphans is still unknown. The job object closes
the gap anyway: it does not depend on the cascade, which evidently failed on
this machine 193 times.

### A question for Marcus

**Should a pane that cannot be put in a job open anyway?** Today it does not:
`StartLocal` returns the error and, for the first pane, `main.go` calls
`log.Fatal`. Launched from Explorer there is no console, so the user sees
gridterm not start and no reason why. Both reviewers argued for logging it and
opening the pane. Against that: carrying on brings back this exact leak, in
silence. The one reachable failure -- a shell that exited before the job could
hold it -- is handled and is not an error. What is left needs Windows to refuse
a nested job, which needs a build older than gridterm's own ConPTY requirement.

### Still open

- The orphaned `conhost.exe` with no `cmd.exe` under it, from the section above.
- A shell can start something in the microseconds between `CreateProcess` and
  the job assignment, and that escapes the job for good. go-pty closes the
  thread handle, so there is no way to start the shell suspended and resume it
  after the assignment without patching go-pty.
