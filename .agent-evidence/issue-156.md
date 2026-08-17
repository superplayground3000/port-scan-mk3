# Issue 156 evidence

Puts `ISIG` back after raw mode, so Ctrl+C cancels an interactive scan again.

## The mechanism

`term.MakeRaw` is a `cfmakeraw` replica. It clears `ECHO|ECHONL|ICANON|ISIG|IEXTEN`
from the terminal `Lflag`.

`ISIG` is the flag that makes the terminal driver convert the INTR character
(Ctrl+C, byte `0x03`) into `SIGINT`, QUIT (Ctrl+\) into `SIGQUIT`, and SUSP
(Ctrl+Z) into `SIGTSTP`. With `ISIG` clear, the driver stops doing that and hands
the raw byte to the program instead.

`pkg/speedctrl/keyboard.go` reads one byte at a time and acts only on `' '`, so
it discards `0x03`. `state.WithSIGINTCancel` then waits for a signal that never
arrives. The operator cannot stop a scan from the terminal that started it.

## The fix follows a pattern already in this package

`StartKeyboardLoop` already walks back one piece of `MakeRaw`'s over-reach. It
calls `enableOutputPostProcessing`, which re-sets `OPOST | ONLCR`, because
`MakeRaw` broke output newline handling.

The signal fix is the same shape. `enableSignalCharacters(fd)` is a new
per-platform function beside the output one, wired through a new injectable var
`keyboardEnableSignalCharacters`, and called immediately after
`enableOutputPostProcessing`. Its failure path restores the terminal and returns
the error, matching the existing path exactly.

- `terminal_signal_linux.go` and `terminal_signal_darwin.go` set `ISIG` on `Lflag`.
- `terminal_signal_windows.go` sets `ENABLE_PROCESSED_INPUT`, the Windows
  analogue.
- `terminal_signal_other.go` is a no-op for platforms this package does not
  support.

`ICANON` stays clear, so the space bar still toggles the pause on the first byte
with no Enter key.

## Why the existing tests could not catch this

Every `speedctrl` test replaced `keyboardMakeRaw` with a fake returning
`&term.State{}`, so no real terminal was ever configured. Every signal test
elsewhere sends the signal from a program, whose standard input is not a
terminal, so `StartKeyboardLoop` returned before `MakeRaw` and raw mode never
started.

The defect existed only in an interactive terminal, and no gate ran in one. A new
test built only from those fakes would repeat the blind spot, so the agreed seam
required a real pseudo-terminal.

## Seam

Agreed with the maintainer before any test was written:

1. A real pseudo-terminal test of the platform function.
2. A wiring test through the injectable var.

`golang.org/x/sys` was already a direct dependency, so the pty needed no new
module. The test opens `/dev/ptmx`, unlocks the slave, and opens `/dev/pts/N`.

It does **not** skip. If the pty cannot be opened on Linux it calls `t.Fatalf`. A
silent skip would produce a test that never runs, which is the failure mode that
let this defect ship.

## RED proof

The red run neutralised **only** the fix, keeping the symbol so the failure is a
real assertion failure rather than a compile error, and keeping the test file as
committed:

```text
git diff --stat
 pkg/speedctrl/terminal_signal_linux.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)
```

```text
=== RUN   TestEnableSignalCharacters_OnRawTerminal_SetsISIG
    terminal_signal_linux_test.go:71: expected ISIG set after enableSignalCharacters, got Lflag=0xa30
--- FAIL: TestEnableSignalCharacters_OnRawTerminal_SetsISIG (0.00s)
--- PASS: TestEnableSignalCharacters_OnRawTerminal_KeepsICANONClear (0.00s)
--- PASS: TestEnableSignalCharacters_OnInvalidDescriptor_ReturnsError (0.00s)
--- PASS: TestEnableSignalCharacters_OnRawTerminal_StillDeliversSpaceAtOnce (0.00s)
```

`ISIG` is bit `0x1`, and `0xa30 & 0x1 == 0`, so the reported `Lflag` confirms the
flag was absent. The other three tests stayed green, which is correct: they cover
`ICANON`, the error path, and space delivery, none of which this line affects.

The committed test also asserts the **negative** half, that `ISIG` is clear
immediately after `MakeRaw` and before the fix runs. Without it the test would
pass on a terminal that never had `ISIG` cleared and would prove nothing. That
assertion is load bearing: a fresh pty measures `Lflag=0x8a3b`, which has `ISIG`
set, so a test that skipped `MakeRaw` would pass with no fix present. The
assertion did **not** fire during the red run, which confirms `MakeRaw` really
cleared the flag and the test was measuring the right thing.

The test calls `term.MakeRaw` directly rather than through the injectable var, so
no fake can defeat it.

### The space-bar test discriminates too

A relaxed or mistaken fix could quietly restore canonical mode, so that test was
mutated as well. Changing the fix to `state.Lflag |= unix.ISIG | unix.ICANON`
made `TestEnableSignalCharacters_OnRawTerminal_StillDeliversSpaceAtOnce` block in
`unix.Read` until the package timeout:

```text
FAIL	github.com/xuxiping/port-scan-mk3/pkg/speedctrl	20.023s
```

A hang is a poor failure mode, which is why the committed test polls with a two
second bound first and reports the regression instead of blocking.

### Existing tests were not weakened

Five pre-existing `StartKeyboardLoop` tests needed the new injectable var stubbed,
exactly as they already stub `keyboardEnableOutputPostProcessing`. Without it they
call the real ioctl on fake descriptors and fail with `inappropriate ioctl for
device`.

The whole `pkg/speedctrl` diff contains **zero deleted lines**. No assertion was
changed, removed, or weakened.

## Proof that the flag does what this issue claims

Setting a bit is not the same as fixing the behavior. A separate throwaway probe,
not committed, measured what actually happens to the INTR byte on a real pty:

| Terminal state | INTR byte `0x03` written to the master |
| --- | --- |
| `ISIG` clear, which is what `MakeRaw` leaves | **delivered to the reader as raw data**, `n=1 buf=0x03` |
| `ISIG` set, which is what the fix restores | **not delivered to the reader**; the driver consumed it |

Be precise about what this measures. The byte reaching or not reaching the reader
is measured. That the driver then raises `SIGINT` is **not** measured here,
because no process sat in the pty's foreground process group. The end-to-end
proof below closes that half.

The keyboard loop receiving `0x03` as data, and discarding it because it only
handles `' '`, is exactly the first row.

## End-to-end proof: Ctrl+C now cancels a scan

An independent reviewer built an A/B harness under a real controlling
pseudo-terminal, using `fork`, `setsid`, and `TIOCSCTTY` so the child truly owns
the terminal. It scanned closed ports on `127.0.0.1` only, so constitution V
holds. It writes the Ctrl+C byte to the master side and watches the child.

| Binary | Result after the Ctrl+C byte |
| --- | --- |
| fixed, `e7ed761` | `exited=True rc=130 elapsed=0.1s` |
| pre-fix `master`, `8debb99` | `exited=False after 6s`, Ctrl+C did not cancel the scan |

The harness discriminates, so it measures the fix rather than the environment.
Exit `130` is the documented graceful code from `README.md:157`.

**The operator experience is therefore verified on Linux.** An earlier version of
this file said nobody had pressed Ctrl+C. That was true when written and is now
out of date.

## Quality gates

```text
GOTOOLCHAIN=go1.24.4 make verify

coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

```text
GOTOOLCHAIN=go1.24.4 make verify-e2e

=== RESULT ===
All selected quality gates passed.
```

Both exit 0. The e2e run was made because `pkg/speedctrl` is started from the
scan runtime. It cannot exercise this change, because e2e has no terminal and
`StartKeyboardLoop` returns before `MakeRaw`, so it is a regression check rather
than evidence for the fix.

## Validation triggers

- **Unit:** covered above, including a real pseudo-terminal.
- **e2e:** run, and green. See the note above about what it can and cannot show.
- **Performance:** not triggered. This runs once at scan start, not on a hot
  path, and it performs two ioctls.

## Ctrl+\ becomes live again, and it is not graceful

`ISIG` is one flag for all three signal characters, so restoring it also restores
QUIT (Ctrl+\) and SUSP (Ctrl+Z).

`port-scan` traps `os.Interrupt` alone (`pkg/state/signal.go`). It does not trap
`SIGQUIT`. Ctrl+\ therefore takes the default action: the process dumps core and
stops at once, with no resume snapshot, and the goroutine that restores the
terminal never runs, so the console can stay in raw mode.

Before this fix Ctrl+\ did nothing, because the keyboard loop discarded the byte.
This is a real behavior change introduced here, and it was found in review rather
than by the implementer.

It is accepted rather than fixed. One flag controls all three characters, and
disabling QUIT alone would mean also writing `c_cc[VQUIT]`, which is more
machinery than issue #156 asks for. Ctrl+Z is benign, because a shell with job
control saves and restores the job's terminal settings.

`docs/interrupt-handling.md` now states this, including `reset` as the recovery
step.

## What is NOT proven

**Windows is unobserved at runtime.** `terminal_signal_windows.go` is implemented
and unit-tested, and the unit test runs on the native Windows gate. Nobody has
pressed Ctrl+C on a Windows console. That `ENABLE_PROCESSED_INPUT` is the correct
analogue, and that Ctrl+Break is unaffected because `CTRL_BREAK_EVENT` does not
use that flag, comes from the flag rules and from reading `x/term`, not from an
observation.

The handle conversion was checked in review. `keyboardFD` returns
`int(os.Stdin.Fd())`, and on Windows `os.File.Fd()` returns the kernel handle
rather than a C runtime descriptor. `x/term`'s own `isTerminal` and `makeRaw` use
`windows.Handle(fd)` with that same value, so this call is exactly as correct as
the `MakeRaw` that precedes it. If the handle were wrong, `IsTerminal` would
return false and the keyboard loop would never start.

**darwin is unobserved at runtime.** `terminal_signal_darwin.go` is compile
verified only. The pty test is `//go:build linux`, because darwin needs
`TIOCPTYGRANT`, `TIOCPTYUNLK`, and `TIOCPTYGNAME` rather than the Linux
`TIOCSPTLCK` and `TIOCGPTN` pair, so the build tags do not make it free, and no
darwin host is available.

Both gaps belong to
[#99](https://github.com/superplayground3000/port-scan-mk3/issues/99) Group C,
which exists for evidence no runner can produce.

## Documentation

`docs/interrupt-handling.md` gained a section explaining why the step exists and
telling a future reader not to remove it, plus entries in its test list. The
Windows entry states plainly that nobody has pressed the key yet.

`README.md:157` and `docs/interrupt-handling.md:46` needed no correction. They
already promised the right behavior; the code did not deliver it.

## Relation to #145

[#145](https://github.com/superplayground3000/port-scan-mk3/issues/145) states
that the first Ctrl+C starts graceful cancellation. That contract assumed Ctrl+C
reaches the process. Before this change it did not, so a complete and correct
#145 would still have left the operator unable to cancel from the terminal.

## Independent review

Cross-provider review was unavailable: Codex hit a provider usage limit, with
credits returning 2026-08-20. Rule G2 ranks reviewers as different provider, then
different Claude model, then any fresh-context agent, so this used the second
rank: a different Claude model with no knowledge of the implementing
conversation.

**State this plainly when citing this file: the change has one same-provider,
different-model review round. It has no cross-provider round.**

Verdict: **APPROVE**, no blocking issues. The reviewer reproduced every claim in
its own throwaway worktrees rather than accepting it, including the red proof, the
INTR-byte measurement from scratch, and both gates.

It went beyond the brief in two ways that changed this file:

1. It **built its own end-to-end A/B harness** under a real controlling
   pseudo-terminal and closed the gap this file had declared unclosable. The
   result is recorded above. An independent implementation that discriminates
   against the pre-fix binary is stronger evidence than the harness named in the
   issue.
2. It **found the Ctrl+\ side effect**, which the implementer had not noted. That
   is now documented in `docs/interrupt-handling.md` and in this file.

It also flagged two sentences here that claimed more than the commands proved.
Both are corrected: the summary line is now backed by the end-to-end result, and
the INTR table no longer asserts that a signal was raised, because only the byte
delivery was measured.
