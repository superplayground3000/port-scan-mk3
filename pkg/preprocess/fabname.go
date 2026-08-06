package preprocess

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidFabName is the sentinel error that every ValidateFabName failure
// wraps. Callers classify a rejection with errors.Is, not with a match on the
// message text.
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

// ValidateFabName returns nil when name is usable as a single directory
// component of the output tree.
//
// Linux output is routinely used on Windows, so the rules are the strictest of
// both platforms, and ValidateFabName applies them on every GOOS. Rejection is
// always explicit. ValidateFabName never sanitizes a name in silence, because a
// rewritten fab name puts results in a directory that the operator did not ask
// for.
//
// ValidateFabName rejects a name in these conditions:
//
//   - The name is empty.
//   - The name contains a path separator of either platform. This condition also
//     covers absolute paths, drive-relative paths, and UNC paths.
//   - The name is "." or "..".
//   - The name contains a control character, or a character that Windows
//     forbids.
//   - The name ends with a dot or a space.
//   - The name is a Windows reserved device name. This test ignores case, and it
//     also catches extension variants such as "con.txt", which Windows resolves
//     as a device too.
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

	// Windows matches a device name against the component's stem -- the text
	// before the first dot -- after trimming trailing spaces and dots from it.
	// So "con .txt" resolves to CON just as "con.txt" does, and the trailing
	// space is not caught by the rule above because the name ends in ".txt".
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " .")
	if _, reserved := reservedDeviceNames[strings.ToLower(stem)]; reserved {
		return fmt.Errorf("%w %q: %q is a name Windows reserves for a device", ErrInvalidFabName, name, stem)
	}

	return nil
}
