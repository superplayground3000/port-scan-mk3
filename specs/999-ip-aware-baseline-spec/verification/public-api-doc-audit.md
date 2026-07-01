# Public API Doc Audit

- Date: 2026-03-02
- Feature: 001-ip-aware-baseline-spec
- Scope: `pkg/input`, `pkg/scanapp`, `pkg/writer`, `pkg/state`

## Findings

The following exported symbols lacked Go doc comments before remediation:

- `pkg/input`: `CIDRRecord`, `PortSpec`, `LoadCIDRs`, `LoadCIDRsWithColumns`, `(*CIDRRecord).Parse`, `LoadPorts`, `ValidateIPRows`
- `pkg/scanapp`: `DialFunc`, `RunOptions`, `Run`
- `pkg/writer`: `Record`, `CSVWriter`, `NewCSVWriter`, `(*CSVWriter).Write`, `(*CSVWriter).WriteHeader`, `OpenOnlyWriter`, `NewOpenOnlyWriter`, `(*OpenOnlyWriter).Write`, `(*OpenOnlyWriter).WriteHeader`
- `pkg/state`: `Save`, `Load`, `WithSIGINTCancel`

## Remediation Status

- All listed symbols now include Go doc comments describing inputs, outputs, and/or failure behavior.
- Verified by source inspection in the files above.
