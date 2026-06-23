package scanapp

// ensureFDLimit is a no-op on Windows since Windows doesn't use
// POSIX file descriptor limits. Windows handle limits are much
// higher by default and managed differently by the OS.
func ensureFDLimit(workers int) error {
	return nil
}
