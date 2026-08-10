# Architecture views

[`docs/apps/port-scan/DESIGN.md`](../apps/port-scan/DESIGN.md) is the current
architecture source for `port-scan`. The files in this directory are derived
views. Their status shows whether each view matches the current source.

| View | Status | Version | Source | Reason |
| --- | --- | --- | --- | --- |
| `diagram.html` | Outdated | 3.0.1 | `docs/apps/port-scan/DESIGN.md` | The view still shows shared configuration parsing and the pre-4.0 scan runtime. |
| `port-scan-mk3-architecture.drawio` | Outdated | Before 2.0.0 | `docs/apps/port-scan/DESIGN.md` | The view shows the old combined scan workflow. |
| `port-scan-mk3-architecture.html` | Outdated | Before 2.0.0 | `port-scan-mk3-architecture.drawio` | The export has the same old workflow as its source. |
| `drawio-assets.html` | Outdated | Before 2.0.0 | `port-scan-mk3-architecture.drawio` | The assets belong to the old draw.io export. |

`NOTE-2.0.0.md` records why the detailed draw.io views did not change for
version 2.0.0. It is a historical note, not a current architecture view.
