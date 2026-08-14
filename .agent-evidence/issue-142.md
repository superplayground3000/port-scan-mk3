# Issue 142 evidence

## Test-first evidence

The first tracer test used late official headers and a short second record. All four public parser paths caused this panic:

```text
panic: runtime error: index out of range [3] with length 2
```

The fail-fast tracer then used malformed CSV and invalid CIDR records. All four paths returned earlier valid entries without an error:

```text
expected an input error, got entries []cidrutil.CIDREntry{...}
```

The header tracer found seven contract errors. Partial official headers, empty input, blank input, and a one-field legacy header did not fail correctly.

The whitespace tracer used whitespace-only lines before and after the header. The parser used the first whitespace-only line as the header.

The command tracer ran the compiled binary with invalid deny and open inputs. Stderr was empty for both inputs. The open-input case also wrote a stdout header.

## Bounded benchmark method

Each benchmark parses 100,000 valid records. The command ran each public entry point six times with memory statistics:

```bash
GOTOOLCHAIN=go1.24.4 go test ./pkg/cidrutil -run '^$' -bench 'Benchmark(ParseDenyCSV|DenyCSVReader|ParseOpenCSV|OpenCSVReader)100K$' -benchmem -benchtime=1x -count=6
```

The baseline commit was `2c0c185`. The test system used Linux amd64 and an AMD RYZEN AI MAX+ 395 processor.

| Entry point | Phase | `ns/op`, six runs | `B/op`, six runs | `allocs/op`, six runs |
|---|---|---|---|---|
| `ParseDenyCSV` | Before | 96231868, 99063704, 87187478, 70268103, 62632448, 64270053 | 462439328, 462427824, 462433664, 462427968, 462427824, 462427824 | 1596359, 1596338, 1596350, 1596341, 1596340, 1596338 |
| `ParseDenyCSV` | After | 22121432, 20655298, 23668486, 20483195, 17442176, 19532922 | 25744112, 25744208, 25744144, 25749712, 25744128, 25744192 | 600044, 600046, 600044, 600052, 600043, 600045 |
| `DenyCSVReader.ReadAll` | Before | 63596538, 65926151, 65497797, 62778723, 68789246, 73565735 | 462489336, 462489576, 462489912, 462489416, 462489576, 462489576 | 1596339, 1596341, 1596344, 1596340, 1596342, 1596342 |
| `DenyCSVReader.ReadAll` | After | 17765212, 19318138, 15469513, 19533713, 15063300, 15155323 | 25744112, 25744160, 25744112, 25744160, 25744304, 25744144 | 600044, 600045, 600044, 600046, 600047, 600044 |
| `ParseOpenCSV` | Before | 67868639, 66332354, 70641764, 70117159, 69062490, 68600813 | 462427592, 462427688, 462427720, 462427704, 462427848, 462427768 | 1596335, 1596339, 1596339, 1596340, 1596340, 1596337 |
| `ParseOpenCSV` | After | 15562948, 15521941, 15263957, 15468551, 15077447, 15284456 | 25744120, 25744088, 25744248, 25744104, 25744088, 25744184 | 600046, 600045, 600047, 600044, 600044, 600045 |
| `OpenCSVReader.ReadAll` | Before | 66904878, 66438834, 67631292, 68766814, 69549234, 65666083 | 462489504, 462489312, 462489280, 462489136, 462489264, 462489072 | 1596341, 1596340, 1596339, 1596338, 1596339, 1596336 |
| `OpenCSVReader.ReadAll` | After | 15623291, 16854403, 20925376, 15923074, 15572306, 15695306 | 25744216, 25744264, 25744168, 25744104, 25744088, 25744200 | 600047, 600048, 600047, 600045, 600045, 600047 |

Median time decreased by 73.9% to 77.7% across the four entry points. Allocated bytes decreased by approximately 94.4%.

Allocations decreased by approximately 62.4%. No time or allocated-byte result increased, so the 10% regression block did not apply.

## Focused test result

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/cidrutil       0.023s
ok  github.com/xuxiping/port-scan-mk3/cmd/cidr-compare  0.554s
```
