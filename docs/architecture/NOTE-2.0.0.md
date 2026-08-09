# Architecture diagram note (2.0.0)

The static block-flow diagram in `diagram.html` has been updated for the 2.0.0
three-step pipeline (`pre-ping` → `generate-buckets` → `scan`, with `scan`
requiring `-resume`).

**Not yet regenerated** (left for a follow-up with the diagram tooling, since
they are exported/binary artifacts that should not be hand-edited):

- `port-scan-mk3-architecture.drawio` (draw.io source)
- `port-scan-mk3-architecture.html` and `drawio-assets.html` (exports of the above)

These still depict the pre-2.0.0 monolithic `scan` (single `port-scan scan`
entry that pinged and built chunks inline). When regenerating, model the three
subcommands as separate stages with durable file hand-offs:

```
rich.csv → pre-ping → unreachable_results-<ts>.csv
rich.csv + unreachable.csv → generate-buckets → bucket snapshot (== resume Snapshot)
rich.csv + bucket snapshot → scan (-resume, no ping) → scan_results / opened_results
```

See §5.1 of the 2026-07-22 pipeline-split design plan in `docs/plans/` for the
authoritative data-flow, and `docs/release-notes/2.0.0.md` for the flag
relocation. That plan keeps the 2.x name of the first step in its filename,
because it is a historical record.
