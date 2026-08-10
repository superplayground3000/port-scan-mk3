# Pressure source seam prototype

This throwaway terminal prototype compares three pressure source interfaces.
It does not change the production scanner.

Run the prototype from the repository root:

```sh
go run ./labs/pressure-source-seam
```

Select an interface design. Then select a pressure scenario. The program shows
the complete sample and monitor state after each poll.

The prototype keeps these rules fixed:

- A successful multi-source poll returns the maximum pressure.
- Any source error makes the complete poll fail.
- A failed poll still returns all source results.
- Source results stay in configuration order.
- A successful poll resets the failure streak.
- The third consecutive failure stops the monitor.
- A pressure value at the threshold pauses the scan.
- A failed poll does not change the controller state.

The recommended design is option 3. `scanapp` owns the one-method consumer
interface. A deep `pkg/pressure` module owns the result model and the two remote
adapters.

