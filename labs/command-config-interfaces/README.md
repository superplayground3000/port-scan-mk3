# Command configuration interface prototype

This throwaway prototype compares three internal Go interfaces. It does not
change the production CLI or packages.

Run it from the repository root:

```sh
go run ./labs/command-config-interfaces
```

Select one design or show all designs. The program shows the proposed public
API, caller work, error boundary, migration cost, and main trade-off.

The comparison uses these fixed constraints:

- Keep command names, accepted flags, defaults, ranges, and exit classes.
- Give each workflow only the values that it uses.
- Reject an uninitialized configuration before file or network work.
- Keep parsing in `pkg/config` and orchestration in the workflow packages.
- Let the next major release remove `Config`, `Parse`, and `ParseFor` without
  compatibility adapters.

The `validate` command is a compatibility exception. It currently uses the
legacy parser and accepts its full flag surface. A replacement parser must
accept and validate those flags, but it can discard values that validation
does not use.

The recommended design is option 3. It gives each command a direct parser and
an opaque concrete value. It has more named entry points than the other
designs, but it keeps command rules local and gives handlers the least work.

