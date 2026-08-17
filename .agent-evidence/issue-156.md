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
pass on a terminal that never had `ISIG` cleared and would prove nothing.

## Proof that the flag does what this issue claims

Setting a bit is not the same as fixing the behavior. A separate throwaway probe,
not committed, measured what actually happens to the INTR byte on a real pty:

| Terminal state | INTR byte `0x03` written to the master |
| --- | --- |
| `ISIG` clear, which is what `MakeRaw` leaves | **delivered to the reader as raw data**, `n=1 buf=0x03` |
| `ISIG` set, which is what the fix restores | **not delivered**; the driver consumed it and signals instead |

That is the reported defect and its fix, measured at the terminal layer rather
than argued from the flag rules. The keyboard loop receiving `0x03` as data, and
discarding it because it only handles `' '`, is exactly the first row.

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

## What is NOT proven

**Nobody has pressed Ctrl+C.** No automated gate in this repository runs in an
interactive console, so no test here can press a key on a real terminal. The
evidence above proves the terminal is configured correctly and that the INTR byte
stops reaching the reader once it is. It does not prove the complete operator
experience.

**Windows is entirely unobserved.** `terminal_signal_windows.go` is implemented
and unit-tested, and the unit test runs on the native Windows gate. Nobody has
pressed Ctrl+C on a Windows console. The reasoning that `ENABLE_PROCESSED_INPUT`
is the correct analogue, and that Ctrl+Break is unaffected because
`CTRL_BREAK_EVENT` does not use that flag, comes from the flag rules and not from
an observation.

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
