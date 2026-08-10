# Deep scan runtime prototype

This throwaway prototype tests the ownership and ordering of one deep scan
runtime module. It does not scan a network or change production code.

Run it from the repository root:

```sh
go run ./labs/deep-scan-runtime
```

Select a scenario. The program shows the state after each lifecycle action.
Use the output to check cancellation, channel drain, output commit, resume
rewind, snapshot save, summary, and final error order.

## Compared designs

### A. Private concrete runtime

Keep `scanRuntime` inside `pkg/scanapp`. Keep `scanapp.Run` as the public
facade. The facade resolves configuration, builds confirmed adapters, and
calls one private operation:

```go
type scanRuntime struct {
	// Private inputs, adapters, and owned state.
}

func newScanRuntime(input scanRuntimeInput, adapters scanRuntimeAdapters) *scanRuntime
func (r *scanRuntime) execute(context.Context) error
```

The runtime owns preparation, output opening, worker and dispatcher startup,
channel drain, cancellation, rewind, snapshot save, summary, and close order.
Keep only confirmed remote and device seams, such as `DialFunc`,
`PressureSource`, and a private keyboard adapter. Use real temporary files in
normal tests. Keep a private writer fault seam for rare output failures.

This is the recommended design. It gives one owner to the coupled lifecycle.
It does not add a runtime interface when only one runtime exists.

### B. Port-rich runtime

Give the runtime interfaces for task execution, output sessions, snapshot
storage, pressure, keyboard, dashboard, and time. This design makes rare
failures easy to script. It also turns internal lifecycle parts into many
small contracts. Those contracts expose more concepts and make the runtime
shallower. When a second real adapter justifies an interface, consider adding
that interface.

### C. New runtime package

Move an exported engine to `pkg/scanruntime`. This creates a strong package
boundary and permits another command to call the engine. Today, only
`scanapp.Run` calls this lifecycle. The new package must export or translate
private scan types. It spreads changes across packages without a second real
caller. Defer this design until another caller or runtime exists.

## Required behavior

- Open outputs before workers and the dispatcher start.
- Let the dispatcher close the task channel.
- Let the executor close the result and executor-error channels.
- Drain produced results and the executor-error channel after cancellation.
- Use the first observed pressure, executor, or output error as the runtime
  error. Simultaneous sources keep their current scheduler-dependent order.
- Count a result only after both output writes succeed.
- Rewind every unwritten task before snapshot creation.
- Return a snapshot save error before a runtime or dispatcher error.
- Return a runtime error before a dispatcher error.
- Emit one completion summary after snapshot persistence.
- Until a separate behavior decision changes the rule, ignore output close
  errors.

## Test surface

Protect ordinary behavior through the configuration parser and `scanapp.Run`.
Keep internal tests only for deterministic fault injection, race behavior,
platform adapters, and performance. After workflow tests protect the same
behavior, remove direct lifecycle-helper tests.

This prototype is for a design decision. Do not merge it into production.
