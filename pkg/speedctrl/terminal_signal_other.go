//go:build !darwin && !linux && !windows

package speedctrl

// enableSignalCharacters does nothing on platforms with no terminal support in
// this package. See the linux and darwin files for the ISIG behavior it
// restores.
func enableSignalCharacters(fd int) error {
	_ = fd
	return nil
}
