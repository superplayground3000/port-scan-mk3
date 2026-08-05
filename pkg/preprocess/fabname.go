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

// windowsInvalidNameChars are the printable characters Windows forbids in a
// file or directory name. Path separators are excluded here on purpose: they
// get their own check so the operator is told the name must be a single
// component rather than being told a character is invalid.
const windowsInvalidNameChars = `<>:"|?*`

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
// A name is rejected when it is empty, carries a path separator of either
// platform (which also covers absolute, drive-relative and UNC paths), is "."
// or "..", contains a control character or a character Windows forbids, ends
// with a dot or a space, or is a Windows reserved device name — the latter
// case-insensitively and including extension variants such as "con.txt", which
// Windows resolves as a device too.
//
// Every returned error wraps [ErrInvalidFabName].
func ValidateFabName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: must not be empty", ErrInvalidFabName)
	}

	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w %q: must be a single directory name, not a path (it contains a path separator)", ErrInvalidFabName, name)
	}

	if name == "." || name == ".." {
		return fmt.Errorf("%w %q: must name a directory, not a relative path element", ErrInvalidFabName, name)
	}

	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("%w %q: contains control character 0x%02X", ErrInvalidFabName, name, r)
		}
		if strings.ContainsRune(windowsInvalidNameChars, r) {
			return fmt.Errorf("%w %q: %q is not allowed in a Windows path component", ErrInvalidFabName, name, r)
		}
	}

	if last := name[len(name)-1]; last == '.' || last == ' ' {
		return fmt.Errorf("%w %q: must not end with a dot or a space (Windows strips them, silently renaming the directory)", ErrInvalidFabName, name)
	}

	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if _, reserved := reservedDeviceNames[strings.ToLower(stem)]; reserved {
		return fmt.Errorf("%w %q: %q is a name Windows reserves for a device", ErrInvalidFabName, name, stem)
	}

	return nil
}
