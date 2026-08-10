# SPEC-08: Speed Control Specification

## Overview

Speed control has two modules:

```text
pkg/speedctrl   pause state and dispatch gate
pkg/pressure    HTTP and OAuth pressure adapters
pkg/scanapp     consumer interface and polling lifecycle
```

Manual pause and pressure pause are independent. Dispatch waits while either
pause is active.

## 1. Pause controller

`speedctrl.Controller` protects its state with a mutex. It exposes one gate
channel to the dispatcher.

```go
ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(true))
ctrl.SetAPIPaused(true)
ctrl.SetManualPaused(false)
<-ctrl.Gate()
```

The gate blocks when any enabled pause source is active. It is ready when all
enabled pause sources are inactive.

## 2. Consumer-owned pressure interface

`pkg/scanapp` owns the interface that it consumes:

```go
type PressureSource interface {
    Sample(context.Context) (pressure.Sample, error)
}
```

`RunOptions.PressureSource` can replace the production adapter. Tests use this
seam for scripted samples. The monitor does not use type assertions.

## 3. Pressure result model

`pkg/pressure` owns the result vocabulary:

```go
type Sample struct {
    Maximum float64
    Sources []SourceResult
}

type SourceResult struct {
    Name     string
    Pressure float64
    Err      error
}
```

Each sample owns a new source-result slice. OAuth source results stay in
configuration order even though requests run concurrently.

If one source fails, the adapter returns the available source results and an
aggregate error. The monitor records source telemetry before aggregate status.

## 4. HTTP adapters

`pressure.NewSimpleHTTP` creates an unauthenticated adapter for one endpoint.
It requires a valid HTTP URL and an explicit `http.Client`.

`pressure.NewOAuthMulti` creates an authenticated adapter for one or more data
endpoints. It requires an auth URL, data URLs, client ID, client secret, and an
explicit `http.Client`.

Each OAuth data endpoint has its own token cache and mutex. Requests for
different sources can run concurrently.

## 5. Validated pressure policy

`config.ScanValues` contains one opaque `config.PressurePolicy`. The policy has
three variants:

- Disabled pressure polling.
- One simple HTTP endpoint.
- One OAuth auth endpoint and one or more data endpoints.

Partial OAuth values cannot leave `pkg/config`. `scanapp.Run` resolves the
policy and creates the selected adapter.

## 6. Polling lifecycle

`pollPressureAPI` receives an interval, a `PressureSource`, runtime options,
the controller, the logger, and one error channel.

For each tick, the monitor:

1. Calls `PressureSource.Sample` once.
2. If the context is canceled, stops without a failure.
3. Sends the sample and source results to telemetry.
4. Increments the consecutive failure count after an error.
5. Stops the run after the third consecutive failure.
6. Resets the failure count after a successful sample.
7. Updates the API pause state from the successful maximum.

The default pressure limit is 60. `RunOptions.PressureLimit` can replace it for
an embedded caller. Pressure equal to the limit pauses dispatch.

## 7. Manual pause

`speedctrl.StartKeyboardLoop` runs only for a supported terminal. It switches
the terminal to raw mode, reads pause commands, and restores terminal state on
exit.

`startManualPauseMonitor` reports manual pause transitions. It stops when the
run context ends.

## 8. CLI flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `-pressure-api` | `http://localhost:8080/api/pressure` | Simple endpoint |
| `-pressure-interval` | `5s` | Poll interval |
| `-disable-api` | `false` | Disable pressure polling |
| `-pressure-use-auth` | `false` | Select OAuth mode |
| `-pressure-auth-url` | empty | OAuth token endpoint |
| `-pressure-data-url` | empty | Comma-separated data endpoints |
| `-pressure-client-id` | empty | OAuth client ID |
| `-pressure-client-secret` | empty | OAuth client secret |

The scan parser accepts a duration such as `5s` or an integer number of
seconds. It rejects non-positive intervals and incomplete OAuth values.

## 9. Extension rules

- Add pressure transports in `pkg/pressure`.
- Keep the consumer interface in `pkg/scanapp`.
- Return source results with an aggregate error.
- Keep transport setup outside the command handler.
- Keep pause state inside `speedctrl.Controller`.
