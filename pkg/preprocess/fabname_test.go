package preprocess

import (
	"errors"
	"fmt"
	"testing"
)

// TestValidateFabName_RejectsWindowsReservedDeviceNames covers the DOS device
// names Windows still reserves in every directory. Windows resolves them as
// devices regardless of the extension, so "con.txt" is as unusable as "con";
// the match is also case-insensitive.
func TestValidateFabName_RejectsWindowsReservedDeviceNames(t *testing.T) {
	reserved := []string{
		"CON", "PRN", "AUX", "NUL",
		"con", "prn", "aux", "nul",
		"cOn", "NuL",
		"COM1", "COM5", "COM9", "com1", "com9",
		"LPT1", "LPT5", "LPT9", "lpt1", "lpt9",
		// Extension variants: the device is matched on the stem.
		"con.txt", "NUL.csv", "com1.log", "lpt9.CSV",
		"aux.tar.gz",
	}

	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			err := ValidateFabName(name)
			if err == nil {
				t.Fatalf("ValidateFabName(%q) = nil, want a rejection: the name is a Windows device", name)
			}
			if !errors.Is(err, ErrInvalidFabName) {
				t.Errorf("ValidateFabName(%q) error %v does not wrap ErrInvalidFabName", name, err)
			}
		})
	}
}

// TestValidateFabName_RejectsWindowsInvalidCharacters covers the characters
// Windows forbids in a file name. They are rejected on every platform because
// output written on Linux is routinely copied to Windows, where such a
// directory cannot be created at all.
func TestValidateFabName_RejectsWindowsInvalidCharacters(t *testing.T) {
	tests := []struct {
		name  string
		fab   string
		badCh string
	}{
		{name: "less than", fab: "fab<1", badCh: "<"},
		{name: "greater than", fab: "fab>1", badCh: ">"},
		{name: "colon", fab: "fab:1", badCh: ":"},
		{name: "double quote", fab: `fab"1`, badCh: `"`},
		{name: "pipe", fab: "fab|1", badCh: "|"},
		{name: "question mark", fab: "fab?1", badCh: "?"},
		{name: "asterisk", fab: "fab*1", badCh: "*"},
		{name: "leading colon", fab: ":fab", badCh: ":"},
		{name: "trailing asterisk", fab: "fab*", badCh: "*"},
		{name: "wildcard only", fab: "*", badCh: "*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFabName(tt.fab)
			if err == nil {
				t.Fatalf("ValidateFabName(%q) = nil, want a rejection: %q is invalid in a Windows path component", tt.fab, tt.badCh)
			}
			if !errors.Is(err, ErrInvalidFabName) {
				t.Errorf("ValidateFabName(%q) error %v does not wrap ErrInvalidFabName", tt.fab, err)
			}
		})
	}
}

// TestValidateFabName_RejectsControlCharacters checks the whole 0x00-0x1F
// range rather than a sample, so no single control character can slip through
// into a directory name.
func TestValidateFabName_RejectsControlCharacters(t *testing.T) {
	for c := 0; c <= 0x1F; c++ {
		t.Run(fmt.Sprintf("0x%02X", c), func(t *testing.T) {
			fab := "fab" + string(rune(c)) + "1"
			err := ValidateFabName(fab)
			if err == nil {
				t.Fatalf("ValidateFabName(%q) = nil, want a rejection: contains control character 0x%02X", fab, c)
			}
			if !errors.Is(err, ErrInvalidFabName) {
				t.Errorf("ValidateFabName(%q) error %v does not wrap ErrInvalidFabName", fab, err)
			}
		})
	}
}

// TestValidateFabName_RejectsTrailingDotsAndSpaces covers names Windows
// silently rewrites: it strips trailing dots and spaces when creating a
// directory, so "fab " and "fab" would collide and the operator would get a
// directory they did not name. Rejecting is preferred over sanitizing.
func TestValidateFabName_RejectsTrailingDotsAndSpaces(t *testing.T) {
	tests := []struct {
		name string
		fab  string
	}{
		{name: "trailing space", fab: "fab "},
		{name: "trailing dot", fab: "fab."},
		{name: "trailing tab-free double space", fab: "fab  "},
		{name: "trailing dot after space", fab: "fab ."},
		{name: "trailing space after dot", fab: "fab. "},
		{name: "dots only", fab: "..."},
		{name: "space only", fab: " "},
		{name: "dot then spaces", fab: "dc-east.  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFabName(tt.fab)
			if err == nil {
				t.Fatalf("ValidateFabName(%q) = nil, want a rejection: Windows strips trailing dots and spaces", tt.fab)
			}
			if !errors.Is(err, ErrInvalidFabName) {
				t.Errorf("ValidateFabName(%q) error %v does not wrap ErrInvalidFabName", tt.fab, err)
			}
		})
	}
}

// TestValidateFabName_RejectsMultiComponentAndTraversalNames covers the names
// that would move the output tree somewhere the operator did not ask for:
// anything carrying a path separator (either platform's), an absolute or
// drive-relative path, or a relative element that walks up or stays put.
func TestValidateFabName_RejectsMultiComponentAndTraversalNames(t *testing.T) {
	tests := []struct {
		name string
		fab  string
	}{
		{name: "forward slash", fab: "fab/sub"},
		{name: "backslash", fab: `fab\sub`},
		{name: "leading forward slash", fab: "/fab"},
		{name: "leading backslash", fab: `\fab`},
		{name: "trailing forward slash", fab: "fab/"},
		{name: "parent traversal", fab: ".."},
		{name: "parent traversal with slash", fab: "../escape"},
		{name: "parent traversal with backslash", fab: `..\escape`},
		{name: "nested traversal", fab: "fab/../../escape"},
		{name: "current directory", fab: "."},
		{name: "absolute unix path", fab: "/etc/cron.d"},
		{name: "absolute windows path", fab: `C:\Windows\Temp`},
		{name: "drive relative path", fab: "C:fab"},
		{name: "bare drive", fab: "C:"},
		{name: "UNC path", fab: `\\server\share`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFabName(tt.fab)
			if err == nil {
				t.Fatalf("ValidateFabName(%q) = nil, want a rejection: it is not a single safe path component", tt.fab)
			}
			if !errors.Is(err, ErrInvalidFabName) {
				t.Errorf("ValidateFabName(%q) error %v does not wrap ErrInvalidFabName", tt.fab, err)
			}
		})
	}
}

// TestValidateFabName_RejectsEmptyName keeps the emptiness rule inside the
// validator: the CLI already refuses an empty --fab-name, but a library caller
// gets the same answer instead of an empty path component.
func TestValidateFabName_RejectsEmptyName(t *testing.T) {
	err := ValidateFabName("")
	if err == nil {
		t.Fatal(`ValidateFabName("") = nil, want a rejection`)
	}
	if !errors.Is(err, ErrInvalidFabName) {
		t.Errorf(`ValidateFabName("") error %v does not wrap ErrInvalidFabName`, err)
	}
}

// TestValidateFabName_AcceptsLeadingSpacesAndInteriorDots pins the boundary of
// the trailing-dot rule: Windows only strips at the end, so interior dots and
// a leading space stay legal and must not be rejected as collateral.
func TestValidateFabName_AcceptsLeadingSpacesAndInteriorDots(t *testing.T) {
	allowed := []string{"fab.v2", "dc.east.1", " fab", "fab .v2", "1.2.3"}

	for _, name := range allowed {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFabName(name); err != nil {
				t.Errorf("ValidateFabName(%q) = %v, want nil: only trailing dots and spaces are stripped by Windows", name, err)
			}
		})
	}
}

// TestValidateFabName_AcceptsNamesThatMerelyLookReserved guards the reserved
// check against over-matching: only the exact device stems are reserved, so a
// longer word that starts with one must still be accepted.
func TestValidateFabName_AcceptsNamesThatMerelyLookReserved(t *testing.T) {
	allowed := []string{
		"console", "printer", "auxiliary", "nullarbor",
		"com", "lpt", "com10", "lpt10", "com1x",
		"con-1", "nul_fab", "prn2",
	}

	for _, name := range allowed {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFabName(name); err != nil {
				t.Errorf("ValidateFabName(%q) = %v, want nil: the name is not a reserved device", name, err)
			}
		})
	}
}
