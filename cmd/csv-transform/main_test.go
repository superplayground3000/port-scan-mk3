package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTransformConfig_FromFlags_MissingRequired(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Unsetenv("TRANSFORM_INPUT")
	os.Unsetenv("TRANSFORM_OUTPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs(nil) = %v, want non-nil error", cfg)
	}
}

func TestParseConfig_EnvVarOverride(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()

	os.Setenv("TRANSFORM_INPUT", "/env/input.csv")
	os.Setenv("TRANSFORM_OUTPUT", "/env/output.csv")

	cfg, err := ParseConfigFromArgs(nil)
	if err != nil {
		t.Fatalf("ParseConfigFromArgs(nil) unexpected error: %v", err)
	}
	if cfg.Input != "/env/input.csv" {
		t.Errorf("cfg.Input = %q, want %q", cfg.Input, "/env/input.csv")
	}
	if cfg.Output != "/env/output.csv" {
		t.Errorf("cfg.Output = %q, want %q", cfg.Output, "/env/output.csv")
	}
}

func TestParseConfig_MissingOutput(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Setenv("TRANSFORM_INPUT", "/some/input.csv")
	os.Unsetenv("TRANSFORM_OUTPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs with only --input = %v, want error", cfg)
	}
	if cfgErr, ok := err.(*ConfigError); ok {
		if cfgErr.Code != 2 {
			t.Errorf("ConfigError.Code = %d, want 2", cfgErr.Code)
		}
	} else {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
}

func TestParseConfig_MissingInput(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Setenv("TRANSFORM_OUTPUT", "/some/output.csv")
	os.Unsetenv("TRANSFORM_INPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs with only --output = %v, want error", cfg)
	}
	if cfgErr, ok := err.(*ConfigError); ok {
		if cfgErr.Code != 2 {
			t.Errorf("ConfigError.Code = %d, want 2", cfgErr.Code)
		}
	} else {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
}

// TestRunMain_FlagArgsParsed reproduces the argv[0] bug:
// main() passes os.Args (including the program name at index 0) to runMain,
// which forwarded it verbatim to ParseConfigFromArgs. flag.FlagSet.Parse stops
// at the first non-flag token, so args[0] (the binary name) caused it to parse
// zero flags and always exit 2 ("input is required").
func TestRunMain_FlagArgsParsed(t *testing.T) {
	// Ensure env vars don't interfere.
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Unsetenv("TRANSFORM_INPUT")
	os.Unsetenv("TRANSFORM_OUTPUT")

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.csv")
	outputPath := filepath.Join(tmpDir, "out.csv")

	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(inputPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write temp CSV: %v", err)
	}

	// Simulate exactly what main() does: pass the full argv including program name.
	args := []string{"csv-transform", "--input", inputPath, "--output", outputPath}
	rc := runMain(args, io.Discard, io.Discard)
	if rc != 0 {
		t.Fatalf("runMain with full argv (including program name) returned %d, want 0", rc)
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("output file was not created by runMain")
	}
}
