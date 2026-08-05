package preprocess

import (
	"errors"
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
