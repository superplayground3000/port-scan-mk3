# Interrupt handling: which terminations are graceful, and which are not

`port-scan` treats exactly one class of event as a *graceful stop*: an OS
interrupt signal, subscribed in `pkg/state/signal.go` (`WithSIGINTCancel`) and
wired into `preping`, `generate-buckets` and `scan` by
`cmd/port-scan/command_handlers.go`.

A graceful stop means all of the following, and operators can rely on it:

- the scan context is canceled and in-flight dials are allowed to finish or time
  out normally — no dial deadline or dispatch delay is shortened;
- a resume snapshot is written to the bucket file, recording the dispatch cursor
  and the output paths (`pkg/scanapp/resume_manager.go`);
- every output handle is closed, so `scan_results-*.csv` and
  `opened_results-*.csv` can be renamed, moved or deleted immediately;
- the process exits **130**.

Everything else terminates the process without that sequence. This page records
which is which, because the difference is invisible from the command line and
decides what an operator has to do next.

## Windows

The Windows console does not have signals. It raises *control events*, and the
Go runtime translates them (`runtime.ctrlHandler`): `CTRL_C_EVENT` and
`CTRL_BREAK_EVENT` both become `SIGINT`, surfaced to Go programs as
`os.Interrupt`; `CTRL_CLOSE_EVENT`, `CTRL_LOGOFF_EVENT` and
`CTRL_SHUTDOWN_EVENT` become `SIGTERM`. Nothing in Go is ever delivered as a
distinct "SIGBREAK" — no such signal exists in the standard library — so
subscribing to `os.Interrupt` is the complete and only way to handle Ctrl+Break.

That runtime mapping describes only what Go *would* surface. Whether the event
is raised in the first place is a separate question, and for the logoff and
shutdown events the answer is normally "not for a program like this one":
Windows delivers them to services, not to interactive console processes, which
are terminated before the signal is sent.

The rows below split by *what event Windows actually raises*, because that — not
the name of the button the operator pressed — decides whether the scan gets to
shut down cleanly. The distinction that matters most is between ending the
console **window** (which raises an event) and ending the **process** (which
does not).

| How the scan is stopped | What Windows raises | Result |
|---|---|---|
| **Ctrl+C** in the console | `CTRL_C_EVENT` → `os.Interrupt` | **Graceful.** Exit 130, resume snapshot written. |
| **Ctrl+Break** in the console | `CTRL_BREAK_EVENT` → `os.Interrupt` | **Graceful.** Exit 130, resume snapshot written. |
| `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, group)` from a parent process | `CTRL_BREAK_EVENT` → `os.Interrupt` | **Graceful.** These two are the only events `GenerateConsoleCtrlEvent` can raise at all. |
| Closing the console **window** (the **X**), or ending that window from Task Manager | `CTRL_CLOSE_EVENT` → `SIGTERM`, **not subscribed** | Not graceful. Even subscribed it would be unreliable — see below. |
| Ending the **process** from Task Manager, `taskkill /F`, End process tree | Nothing — `TerminateProcess` runs no user code | Not graceful. |
| Task Scheduler **End task** (`schtasks /end`) / task timeout | Nothing documented; the task's process tree is terminated | Not graceful. |
| **Logoff** or **shutdown** while the scan runs interactively | Nothing reaches it. `CTRL_LOGOFF_EVENT` and `CTRL_SHUTDOWN_EVENT` are delivered only to *services*; interactive processes are already terminated by then | Not graceful. |
| Service **stop** under a service wrapper (NSSM, WinSW, srvany) | Wrapper-dependent | **Graceful only if** the wrapper is configured to send a console control event to the process group first. Wrappers that terminate directly are not. |
| Power loss, bugcheck | Nothing | Not graceful. |

### Why Ctrl+Break matters more than it looks

A process created with `CREATE_NEW_PROCESS_GROUP` — how wrapper scripts, job
schedulers and CI harnesses commonly launch a long scan — has **Ctrl+C disabled
by Windows for that group**. In that setup Ctrl+Break is not an alternative to
Ctrl+C, it is the *only* console interrupt that can reach the scan at all.

### Why the console-close event is not subscribed

Closing the console window is the one non-Ctrl case that *does* raise an event
this program could subscribe to: `CTRL_CLOSE_EVENT` reaches Go as `SIGTERM`, and
the runtime blocks in its handler to give cleanup a chance. It is still not worth
subscribing, for two reasons.

First, the cleanup window is not ours. Once the handler is entered Windows
terminates the process on its own short deadline (single-digit seconds), and a
scan that has to close writers and serialise a snapshot may or may not finish
inside it. A snapshot half-written during teardown would be worse than none.

Second, `SIGTERM` is not Windows-only. Subscribing to it would silently change
what `kill`, `docker stop` and a systemd stop mean on Linux, turning a
termination into a graceful cancel across every deployment. That is a real
behaviour change for non-Windows users and belongs in its own change with its
own tests, not as a side effect of console handling.

Logoff and shutdown are not in that trade-off at all: an interactive scan never
receives those events, so there is nothing to subscribe to.

If the shutdown-window case ever becomes worth covering, it needs an explicit
decision recorded here plus tests on both platforms.

## Linux and other POSIX platforms

`SIGINT` (Ctrl+C) is the only subscribed signal. `SIGTERM`, `SIGHUP` and
`SIGQUIT` all take their default action and terminate the scan abruptly; the
isolated e2e suite exercises the graceful path with `timeout -s INT`
(`e2e/run_e2e.sh`).

## What to do after a non-graceful termination

The bucket file is only rewritten at the end of a run
(`pkg/scanapp/resume_manager.go`; `generate-buckets` writes the initial one), so
after an abrupt kill it still holds the cursor from **before** that run.

The consequence is precise, and it is the same one issue #51 documents for a
failed output write: re-running the identical `scan -resume` command covers
every target, so **no target is skipped** — but the rows the killed run already
wrote are still on disk and will be written again when the resumed run appends.
Reconcile both `scan_results-*.csv` and `opened_results-*.csv` before consuming
them, or re-run `generate-buckets` and scan into a fresh output path for a clean
single file.

A graceful stop has neither problem: its snapshot records exactly what was
dispatched, and the resumed run appends to the same file with no gap and no
duplicate.

That is worth stating precisely, because the dispatch cursor advances when a
task is *enqueued*, not when its row is written — the same asymmetry that made
issue #51 a data-loss bug. It is safe here because the cursor advances only
*after* a task has been handed to a worker (`pkg/scanapp/task_dispatcher.go`
advances `NextIndex` after the send succeeds, and returns without advancing when
the context is already canceled), and because workers drain every task they
received and the result loop keeps running until the result channel closes. So
on a cancel, every task counted by the cursor has produced a row. The invariant
that does *not* hold is the one issue #51 covers: if writing the output fails,
the snapshot is deliberately not saved at all.

## Where this is verified

- `pkg/state/signal_windows_test.go` builds the real `port-scan.exe`, starts a
  paced loopback-only scan in its own process group, and interrupts it with a
  genuine `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, …)` — then asserts exit
  130, a snapshot that loads and is mid-flight, partial (not complete) results,
  and that every output handle was released. It runs on the native Windows CI
  gate (`.github/workflows/ci.yml`).
- `pkg/state/signal_unix_test.go` delivers a real `SIGINT` to the test process
  and asserts the context cancels — the POSIX counterpart.
- `pkg/state/signal_test.go` pins the subscription list itself, including the
  `signal.Notify` trap that an *empty* list subscribes to every signal.

The rows in the table above marked "No" are deliberately **not** simulated by a
test. A test that "proves" a `TerminateProcess` is handled gracefully could only
do so by not really terminating the process, which would assert something the
product does not do.
