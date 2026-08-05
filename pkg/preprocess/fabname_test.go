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
