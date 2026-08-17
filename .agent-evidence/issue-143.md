# Issue 143 evidence

## Test-first evidence

The adapter tracer passed numeric and textual non-finite values to the current adapters. The current code returned successful samples with `NaN`, `+Inf`, or `-Inf`.

The same tracer passed `math.MaxFloat64`. The normalization operation changed this finite input to `+Inf`.

The OAuth tracer used finite and non-finite `Percent` entries in one source. The current code skipped the non-finite entry and returned the other finite value.

The multi-source tracer used one finite source and one mixed source. The current code returned a successful complete poll.

The monitor tracer used custom `PressureSource` implementations. It produced these failures before the fix:

```text
failure 2 changed API pause from false to true
failure 1 changed API pause from true to false
poll 0 ... err:error(nil), failureCount:0 ... want consecutive failure 1
the mixed third failure did not become fatal
timed out ... non-finite source result to keep the overall streak
```

## Bounded performance evidence

The baseline commit was `5680fd6`. The test system used Linux amd64 and an AMD RYZEN AI MAX+ 395 processor.

The command ran each existing pressure adapter benchmark six times with memory statistics:

```bash
GOTOOLCHAIN=go1.24.4 go test ./pkg/scanapp -run '^$' -bench 'BenchmarkPressureSample(Simple|OAuthMulti)/current$' -benchmem -count=6
```

| Benchmark | Phase | `ns/op`, six runs | `B/op`, six runs | `allocs/op`, six runs |
|---|---|---|---|---|
| SimpleHTTP | Before | 18119, 17826, 18499, 19069, 24176, 22286 | 7005, 7004, 7007, 7005, 7002, 7005 | 82, 82, 82, 82, 82, 82 |
| SimpleHTTP | After | 18468, 17784, 18450, 18557, 18280, 20690 | 7006, 7003, 7006, 7005, 7007, 7006 | 82, 82, 82, 82, 82, 82 |
| OAuthMulti | Before | 37622, 36063, 35858, 35519, 36372, 36701 | 17061, 17060, 17059, 17060, 17055, 17054 | 198, 198, 198, 198, 198, 198 |
| OAuthMulti | After | 37670, 37654, 41840, 40819, 44177, 40814 | 17058, 17064, 17070, 17068, 17058, 17065 | 198, 198, 198, 198, 198, 198 |

The first OAuth block showed a 12.7% median time increase. Work stopped for an immediate isolated interleaved rerun.

| Benchmark | Phase | `ns/op`, six reruns | `B/op`, six reruns | `allocs/op`, six reruns |
|---|---|---|---|---|
| OAuthMulti | Before | 43936, 42584, 39971, 41402, 37343, 36190 | 17060, 17061, 17067, 17064, 17064, 17062 | 198, 198, 198, 198, 198, 198 |
| OAuthMulti | After | 37575, 39576, 36237, 38373, 39564, 36140 | 17060, 17058, 17054, 17057, 17061, 17063 | 198, 198, 198, 198, 198, 198 |

The rerun changed the OAuth median from 40.69 microseconds to 37.97 microseconds. Across all 12 runs, the median increased by approximately 5.3%.

SimpleHTTP median time decreased by approximately 1.7%. Allocations did not change, and allocated bytes changed by less than 0.1%.

The isolated rerun and the combined result are inside the 10% performance block.

## Focused test result

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/pressure  0.008s
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  4.663s
```

## Quality gates

`GOTOOLCHAIN=go1.24.4 make verify` exited 0:

```text
coverage gate passed: 85.2%

=== RESULT ===
All selected quality gates passed.
```

`GOTOOLCHAIN=go1.24.4 make verify-e2e` exited 0 after isolated Docker cleanup:

```text
Network e2e_e2e-net Removed

=== RESULT ===
All selected quality gates passed.
```
