package scanapp

// ensureFDLimit is a no-op on Windows: there is no POSIX RLIMIT_NOFILE to read
// or raise, so there is nothing for a pre-flight check to compare the worker
// count against.
//
// That makes the worker ceiling, not this function, the protection on Windows.
// config.MaxWorkers is validated at parse time and re-applied by
// effectiveWorkerCount, which is what keeps a worker count from reaching the
// scarce Windows resources: the process's handle table, the ~16k default
// dynamic port range, and — in preping — one ping child process per worker.
// Raising a limit here was considered and rejected: the Windows equivalents are
// machine-wide registry settings, not per-process limits a scanner should
// mutate on the operator's host.
func ensureFDLimit(workers int) error {
	return nil
}
