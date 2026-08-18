# Issue #173 — 5.0.0 release notes

Branch `docs/173-release-notes-5.0.0`. Commits `2e34397`, `7e336ee`, `f6c3204`.

## 1. What the change does

- Creates `docs/release-notes/5.0.0.md` (355 lines).
- Restores `docs/release-notes/4.0.0.md` to its content at tag `v4.0.0`
  (105 lines) with `git checkout v4.0.0 -- docs/release-notes/4.0.0.md`.

No code changed.

## 2. Why the 4.0.0 note was restored (the #165 overlap)

`v4.0.0` is tagged at `4febd75`. At that tag the note is 105 lines with 6
headings. Before this change, master's copy was 220 lines with 11 headings.
PR #152 added 115 lines describing work the released binary does not contain.

Everything in `git diff v4.0.0..master` ships in one release. #158 removed
`RunOptions.ProgressInterval`, so constitution VII makes that release MAJOR.
There is no 4.1.0 for the #152 material to land in, so it moves to 5.0.0.md.

The user chose this option explicitly over leaving the duplication for #165.

### Proof that nothing shipped in 4.0.0 was deleted

Every feature described by the removed lines is absent at the tag:

| Feature in removed text | At `v4.0.0` |
|---|---|
| `-target-count-limit` | absent |
| `-output-flush-results` | absent |
| `-cidr-input-size-limit-gb` | absent |
| `-snapshot-size-limit-gb` | absent |
| `-pressure-response-size-limit-mb` | absent |
| `rich_deny_excluded` | absent |
| `errNonFinitePressure` | absent |
| `target_semantics_version` | absent |
| `make verify-performance` | absent |

Cancellation needed a closer look, because `pkg/state/signal.go`,
`WithSIGINTCancel`, exit `130`, and `docs/interrupt-handling.md` all DO exist at
the tag. The removed "Cancellation safety" section is still post-tag content:
`v4.0.0:pkg/state/signal.go` contains no `forceExit(130)`, and master's
`pkg/state/signal.go:131` does. The second-interrupt escalation and the chunk
rewind (`e0b53b2`) are post-tag. 5.0.0.md now says so explicitly.

## 3. Completeness of "Breaking Go API changes"

`git log` alone cannot prove an API section complete, and a `func`/`type` grep
cannot see struct fields — which is exactly what `ProgressInterval` was. So the
check ran `go doc -all` on both ends and compared exported field NAME SETS,
which is immune to gofmt column realignment:

```
git worktree add --detach /tmp/psmk3-tag400 v4.0.0
# go doc -all ./pkg/<p> in each tree, for the ten public packages
pkg/scanapp: REMOVED exported field names -> ['ProgressInterval']
--- scan complete ---
```

Ten packages covered: `config`, `scanapp`, `state`, `task`, `input`, `pressure`,
`writer`, `cidrutil`, `validate`, `scanner`. `RunOptions.ProgressInterval` is
the only removed exported field in the whole public surface.

Removed function signatures reported by a first, cruder pass were false
positives: `NewDenyCSVReader`, `NewOpenCSVReader`, and the two `ReadAll` methods
changed only their parameter and receiver NAMES (`r` to `input`, `dr` to
`reader`), verified at `pkg/cidrutil/parser.go:180-200` against
`v4.0.0:pkg/cidrutil/parser.go:137-215`. The signatures are identical.

## 4. Three things the #173 list and the old 4.0.0.md text both missed

The survey used `git log v4.0.0..master`, which #173 names as the authority over
its own list. It found:

1. **`c29d313`** — three consecutive pressure API failures stop a scan, but the
   result loop never read the queued error, so the run exited `0`. This is the
   #59 defect class one channel over. It is an exit-code change, so 5.0.0.md
   carries a CAUTION for pipelines that read only the exit code.
2. **`0b2b8f7`** — restoring `ISIG` for #156 also revives QUIT. Ctrl+\ now dumps
   core, writes no resume snapshot, and can leave the console in raw mode. The
   old text would have advertised the Ctrl+C fix with no mention of the trade.
3. **The ten resource-limit flags were never named**, only described by their
   defaults. That is #165 acceptance item 3.

## 5. Flag verification

All 13 new flags are named with default and the meaning of `0`. Names and
per-command scope came from the real usage output of a built binary, not from
the flag registrations:

```
GOTOOLCHAIN=go1.24.4 go build -o /tmp/ps-check ./cmd/port-scan
/tmp/ps-check <command> -h
```

- `pre-ping` — the 2 CIDR flags
- `validate` — the CIDR and port flags (4)
- `generate-buckets` — CIDR, port, snapshot (8)
- `scan` — all 10

The first draft omitted `validate`. The usage output caught it. Cross-checked
against `docs/cli/flags.md:51-107`.

Every number in the moved text was re-verified against master rather than
trusted: `pkg/task/expansion_limits.go:16,18` (10000000, 16 GB),
`expansion_limits.go:20-21` (the `1000000000 + candidates * 1500` estimate),
`pkg/input/limits.go:14-20`, `pkg/state/state.go:44-50`,
`pkg/pressure/pressure.go:20-22`, and `pkg/config/scan_config.go:228` (the
1000-result batch).

## 6. Writing standard

`simple-english`, pragmatic mode. Self-check run:

- Sentences over the 25-word descriptive limit: **0 of 188** (code fences
  stripped, inline code counted as one word per rule 8.6).
- Banned modals `should`/`would`/`may`/`might`/`could`: 2 found, both `could`,
  both rewritten to `can` per the modal ladder.
- Semicolons, contractions, `has been`/`have been`, slop list: none.
- Every CAUTION follows rule 7.2: command or condition first, risk second.
- All four internal anchors resolve. LF line endings, no trailing whitespace.

## 7. Gates

```
=== RESULT ===
All selected quality gates passed.
coverage gate passed: 85.5%
```

`make verify` exit 0. `tests/repohygiene` re-run after the later doc edits: ok.

e2e trigger: NOT triggered. No scan pipeline, writer, or pressure-control code
changed — this change is two markdown files.
Performance trigger: NOT triggered. No hot path changed.

## 8. Review status

Two fresh-context reviewers were dispatched on a different Claude model
(`sonnet`), one for facts against the code and one for spec and writing
standard. **Codex was not used: it has been rate-limited since 2026-08-16 and
resets 2026-08-20.** This is G2 rank 2, not rank 1. The same downgrade applies
to PRs #172, #174, and #176 in this session.

## 9. Not done

- `README.md` still says "Version 4.0.0 keeps the command-line interface
  stable" and shows `port-scan version v4.0.0` in its verification examples.
  That text belongs to the release cut, not to writing the note, so it is
  untouched. It must change when `v5.0.0` is tagged.
- #165 acceptance items 1-4 are met. Item 1's other half, the `--fab-name`
  narrowing tracked in #160, is untouched and stays open.
