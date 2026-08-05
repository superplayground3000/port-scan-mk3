package preprocess

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestValidateFabName_AcceptsValidNames is the other half of the contract: the
// rules must not cost operators names Windows genuinely supports, including
// non-ASCII ones and interior spaces.
func TestValidateFabName_AcceptsValidNames(t *testing.T) {
	allowed := []string{
		"dc-east",
		"fab_01",
		"FAB-1",
		"fab 12 東京",
		"фабрика-1",
		"planta-méxico",
		"fab.v2",
		"dc east 1",
		"a",
		"2026",
		"fab(1)",
		"fab#1&2",
		"fab'name",
		"fab~1",
		"データセンター",
	}

	for _, name := range allowed {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFabName(name); err != nil {
				t.Errorf("ValidateFabName(%q) = %v, want nil: Windows supports this name", name, err)
			}
		})
	}
}

// TestOutputPath_AcceptedFabNameStaysInsideBaseDir is the containment property:
// for every fab name the validator accepts, the output path resolves inside the
// base directory.
//
// It deliberately checks containment with filepath.Rel rather than by rebuilding
// the expected path with filepath.Join — production uses Join, so a Join-based
// expectation would agree with the code by construction and could never fail.
// Rel answers a different question ("how do I walk from base to this path?"),
// and any escape shows up as a leading "..".
func TestOutputPath_AcceptedFabNameStaysInsideBaseDir(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	base := filepath.Join(string(filepath.Separator)+"data", "out")

	names := []string{"dc-east", "fab 12 東京", "fab.v2", "fab_01", "2026", "a"}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if err := ValidateFabName(name); err != nil {
				t.Fatalf("ValidateFabName(%q) = %v, want nil (fixture must be an accepted name)", name, err)
			}

			rel, err := filepath.Rel(base, OutputPath(base, name, ts))
			if err != nil {
				t.Fatalf("filepath.Rel: %v", err)
			}
			if filepath.IsAbs(rel) {
				t.Fatalf("output path for fab %q is not relative to base %q (rel = %q)", name, base, rel)
			}
			for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
				if part == ".." {
					t.Fatalf("output path for fab %q escapes base %q (rel = %q)", name, base, rel)
				}
			}
		})
	}
}

// TestOutputPath_TraversalFabNameEscapesWithoutValidation records why the
// validator has to exist. OutputPath is a plain path join and offers no
// containment of its own: handed "../escape" it happily produces a path outside
// the base directory. The guarantee comes from ValidateFabName refusing that
// name first, which this test also asserts.
func TestOutputPath_TraversalFabNameEscapesWithoutValidation(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	base := filepath.Join(string(filepath.Separator)+"data", "out")
	const traversal = "../escape"

	rel, err := filepath.Rel(base, OutputPath(base, traversal, ts))
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if !strings.HasPrefix(filepath.ToSlash(rel), "../") {
		t.Fatalf("OutputPath(%q, %q, ts) unexpectedly stayed inside the base dir (rel = %q); "+
			"if OutputPath gained containment of its own, this test and its premise need revisiting",
			base, traversal, rel)
	}

	if err := ValidateFabName(traversal); err == nil {
		t.Fatalf("ValidateFabName(%q) = nil: nothing stops the escaping path above", traversal)
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
