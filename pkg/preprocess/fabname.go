package preprocess

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidFabName is the sentinel wrapped by every ValidateFabName failure.
// Callers classify rejections with errors.Is rather than by matching message
// text.
var ErrInvalidFabName = errors.New("invalid fab name")

// reservedDeviceNames holds the DOS device names Windows reserves in every
// directory, lowercased for case-insensitive lookup. The set is the one
// documented for current Windows (CON, PRN, AUX, NUL, COM0-COM9, LPT0-LPT9);
// COM0 and LPT0 are included even though older references list only 1-9.
var reservedDeviceNames = func() map[string]struct{} {
	names := map[string]struct{}{
		"con": {}, "prn": {}, "aux": {}, "nul": {},
	}
	for d := '0'; d <= '9'; d++ {
		names["com"+string(d)] = struct{}{}
		names["lpt"+string(d)] = struct{}{}
	}
	return names
}()

// ValidateFabName reports whether name is usable as a single directory
// component of the output tree.
//
// Output produced on Linux is routinely consumed on Windows, so the rules are
// the strictest of both platforms and are enforced on every GOOS. Rejection is
// always explicit: nothing is silently sanitized, because a quietly rewritten
// fab name would put results in a directory the operator did not ask for.
//
// A name is rejected when it is a Windows reserved device name (see
// reservedDeviceNames), case-insensitively and including extension variants
// such as "con.txt" — Windows resolves those as devices too.
//
// Every returned error wraps [ErrInvalidFabName].
func ValidateFabName(name string) error {
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if _, reserved := reservedDeviceNames[strings.ToLower(stem)]; reserved {
		return fmt.Errorf("%w %q: %q is a name Windows reserves for a device", ErrInvalidFabName, name, stem)
	}

	return nil
}
