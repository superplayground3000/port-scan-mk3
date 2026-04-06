package main

import (
	"os"
	"testing"
)

func TestTransformConfig_FromFlags_MissingRequired(t *testing.T) {
	// Simulate calling ParseConfig with no flags set.
	// We save original env vars and restore after.
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
	// Set env vars and ensure they override defaults.
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

	os.Setenv("TRANSFORM_INPUT", "/env/input.xlsx")
	os.Setenv("TRANSFORM_OUTPUT", "/env/output.csv")

	cfg, err := ParseConfigFromArgs(nil)
	if err != nil {
		t.Fatalf("ParseConfigFromArgs(nil) unexpected error: %v", err)
	}
	if cfg.Input != "/env/input.xlsx" {
		t.Errorf("cfg.Input = %q, want %q", cfg.Input, "/env/input.xlsx")
	}
	if cfg.Output != "/env/output.csv" {
		t.Errorf("cfg.Output = %q, want %q", cfg.Output, "/env/output.csv")
	}
}