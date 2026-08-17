# Interrupt handling: which terminations are graceful, and which are not

`port-scan` treats an OS interrupt as a graceful stop. The `scan` command uses
`WithInterruptEscalation`. The other commands use `WithSIGINTCancel`.

A graceful stop means all of the following, and operators can rely on it:

- `port-scan` stops input parsing, runtime rebuild, dispatch waits, and task sends.
- Queued probes do not start. Started probes finish with their original timeout.
- The result loop writes results from the started probes.
- The snapshot rewinds each chunk to its lowest unwritten task.
- `port-scan` writes the resume snapshot to the bucket file.
- `port-scan` closes every output handle, so you can rename, move or delete
  `scan_results-*.csv` and `opened_results-*.csv` immediately.
- the process exits **130**.

Everything else terminates the process without that sequence. This page records
which terminations are graceful. The difference is invisible from the command
line, and it decides what an operator must do next.

After the first interrupt, the command explains the emergency-exit option.
Press Ctrl+C or Ctrl+Break again to force exit code `130`.

The second interrupt does not wait for probes or snapshot persistence. It does
not promise finalized output handles. The previous snapshot stays protected by
the temp-file and rename sequence until replacement.

## Windows

The Windows console does not have signals. It raises *control events*, and the
Go runtime translates them (`runtime.ctrlHandler`). `CTRL_C_EVENT` and
`CTRL_BREAK_EVENT` both become `SIGINT`, which Go programs receive as
`os.Interrupt`. `CTRL_CLOSE_EVENT`, `CTRL_LOGOFF_EVENT` and
`CTRL_SHUTDOWN_EVENT` become `SIGTERM`. Go never delivers a distinct "SIGBREAK",
and no such signal exists in the standard library. So a subscription to
`os.Interrupt` is the complete and only way to handle Ctrl+Break.

That runtime mapping describes only what Go surfaces when the event occurs.
Whether Windows raises the event at all is a separate question. For the logoff
and shutdown events, the answer is normally "not for a program like this one".
Windows delivers them to services, not to interactive console processes, and it
terminates those processes before it sends the signal.

The rows below split by *what event Windows raises*, because that decides
whether the scan stops gracefully. The name of the button that the operator
pressed does not decide it. The most important distinction is between the end of
the console **window** (which raises an event) and the end of the **process**
(which does not).

| How the scan is stopped | What Windows raises | Result |
|---|---|---|
| **Ctrl+C** in the console | `CTRL_C_EVENT` → `os.Interrupt` | **Graceful.** Exit 130, resume snapshot written. |
| **Ctrl+Break** in the console | `CTRL_BREAK_EVENT` → `os.Interrupt` | **Graceful.** Exit 130, resume snapshot written. |
| `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, group)` from a parent process | `CTRL_BREAK_EVENT` → `os.Interrupt` | **Graceful.** These two are the only events `GenerateConsoleCtrlEvent` can raise at all. |
| Closing the console **window** (the **X**), or ending that window from Task Manager | `CTRL_CLOSE_EVENT` → `SIGTERM`, **not subscribed** | Not graceful. A subscription to this event is also unreliable — see below. |
| Ending the **process** from Task Manager, `taskkill /F`, End process tree | Nothing — `TerminateProcess` runs no user code | Not graceful. |
| Task Scheduler **End task** (`schtasks /end`) / task timeout | Nothing documented. Windows terminates the process tree of the task | Not graceful. |
| **Logoff** or **shutdown** while the scan runs interactively | Nothing reaches it. Windows delivers `CTRL_LOGOFF_EVENT` and `CTRL_SHUTDOWN_EVENT` to *services* only, and it terminates interactive processes before that point | Not graceful. |
| Service **stop** under a service wrapper (NSSM, WinSW, srvany) | Wrapper-dependent | **Graceful only if** the wrapper is configured to send a console control event to the process group first. Wrappers that terminate directly are not. |
| Power loss, bugcheck | Nothing | Not graceful. |

### Why Ctrl+Break matters more than it looks

Wrapper scripts, job schedulers and CI harnesses commonly run a long scan with
`CREATE_NEW_PROCESS_GROUP`. For such a process, **Windows disables Ctrl+C for
that group**. In that setup, Ctrl+Break is not an alternative to Ctrl+C. It is
the *only* console interrupt that can reach the scan.

### Why the console-close event is not subscribed

The console-window close is the one non-Ctrl case that *does* raise an event
this program can subscribe to. `CTRL_CLOSE_EVENT` reaches Go as `SIGTERM`, and
the runtime blocks in its handler to give the cleanup a chance. A subscription
is still not worth the cost, for two reasons.

First, the cleanup window is not ours. When the handler starts, Windows
terminates the process on its own short deadline (single-digit seconds). A scan
that must close the writers and serialize a snapshot can fail to finish inside
that deadline. A half-written snapshot from a teardown is worse than none.

Second, `SIGTERM` is not Windows-only. A subscription to it changes what `kill`,
`docker stop` and a systemd stop mean on Linux, and it gives no message. It
makes a termination into a graceful cancel across every deployment. That is a
real behavior change for non-Windows users. It belongs in its own change with
its own tests, not in console handling.

Logoff and shutdown are not in that trade-off: an interactive scan never
receives those events, so there is nothing to subscribe to.

If the shutdown-window case ever becomes worth the work, it needs an explicit
decision here and tests on both platforms.

## Linux and other POSIX platforms

`SIGINT` (Ctrl+C) is the only subscribed signal. `SIGTERM`, `SIGHUP` and
`SIGQUIT` all take their default action and terminate the scan abruptly. The
isolated e2e suite exercises the graceful path with `timeout -s INT`
(`e2e/run_e2e.sh`).

## The raw keyboard mode must keep the signal characters

Every graceful path on this page starts with a signal. The signal exists only
because the terminal makes it. An interactive `scan` puts the terminal into raw
mode for the space-bar pause (`pkg/speedctrl/keyboard.go`), and `term.MakeRaw`
clears the `ISIG` flag. A terminal without `ISIG` does not make `SIGINT` from
the INTR character (Ctrl+C, byte `0x03`). It sends the byte to the program, the
keyboard loop discards it, and the operator cannot stop the scan.

`StartKeyboardLoop` therefore calls `enableSignalCharacters` immediately after
`term.MakeRaw` and puts `ISIG` back. Windows has the same problem with the
console flag `ENABLE_PROCESSED_INPUT`, and the same call puts that flag back.
Do not remove this step. Issue #156 records the failure it prevents.

`ICANON` stays clear, so the space bar still toggles the pause on the first
byte.

### Ctrl+\ also becomes live again, and it is not graceful

`ISIG` is one flag for all three signal characters. Putting it back therefore
also restores QUIT (Ctrl+\) and SUSP (Ctrl+Z), not only INTR.

`port-scan` subscribes to `os.Interrupt` alone (`pkg/state/signal.go`). It does
not trap `SIGQUIT`. So Ctrl+\ takes the default action: the process dumps core
and stops at once. The scan writes no resume snapshot, and the goroutine that
restores the terminal never runs, so the console can stay in raw mode. Type
`reset` to recover it.

Before the `ISIG` fix, Ctrl+\ did nothing at all, because the keyboard loop read
the byte and discarded it. This is a deliberate trade. Ctrl+C is the documented
way to stop a scan, and one flag controls all three characters.

Use Ctrl+C. Read the section below if a run ended without a snapshot.

## What to do after a non-graceful termination

`port-scan` rewrites the bucket file only at the end of a run
(`pkg/scanapp/resume_manager.go`, and `generate-buckets` writes the first one).
Thus, after an abrupt kill, the file still holds the cursor from **before** that
run.

The consequence is precise, and it is the same one that issue #51 documents for
a failed output write. If you run the identical `scan -resume` command again, it
covers every target, so **no target is skipped**. But the rows that the killed
run already wrote are still on disk, and the resumed run appends them a second
time. Reconcile both `scan_results-*.csv` and `opened_results-*.csv` before you
use them. For one clean file, run `generate-buckets` again and scan into a fresh
output path.

A graceful stop can repeat a persisted row after a rewind. It cannot skip an
unwritten task. A resumed run appends to the same output files.

The dispatch cursor advances after a task enters the queue. Cancellation can
leave that task unwritten. The runtime marks such tasks and rewinds each chunk
before it saves the snapshot.

## Where this is verified

- `pkg/state/signal_windows_test.go` builds the real `port-scan.exe` and starts
  a paced loopback-only scan in its own process group. It then interrupts the
  scan with a genuine `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, …)`. It asserts
  exit 130, a snapshot that loads and is mid-flight, partial (not complete)
  results, and the release of every output handle. It runs on the native Windows
  CI gate (`.github/workflows/ci.yml`).
- `pkg/state/signal_unix_test.go` delivers a real `SIGINT` to the test process
  and asserts the context cancels — the POSIX counterpart.
- `cmd/port-scan/interrupt_unix_test.go` sends two real `SIGINT` events. It
  makes sure that the first message and the second exit request are correct.
- `pkg/state/interrupt_escalation_test.go` makes sure that the platform-neutral
  first and second interrupt state is correct.
- `pkg/speedctrl/terminal_signal_linux_test.go` opens a real pseudo-terminal,
  applies `term.MakeRaw`, and asserts that `ISIG` is clear before
  `enableSignalCharacters` and set after it. It also asserts that a space byte
  is still readable at once.
- `pkg/speedctrl/terminal_signal_windows_test.go` asserts that the console keeps
  its other raw-mode flags and gains `ENABLE_PROCESSED_INPUT`. It runs on the
  native Windows CI gate. Nobody has yet pressed Ctrl+C on a Windows console to
  confirm the result; issue #99 tracks that check.
- `pkg/state/signal_test.go` pins the subscription list itself. It covers the
  `signal.Notify` trap: an *empty* list subscribes to every signal.

No test deliberately simulates the rows marked "No" in the table above. A test
that "proves" the graceful handling of a `TerminateProcess` can do so only if it
does not really terminate the process. Such a test asserts something that the
product does not do.
