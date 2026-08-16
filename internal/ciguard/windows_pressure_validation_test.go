package ciguard

import (
	"strings"
	"testing"
)

// windowsPressureValidationScript runs the Native Windows validation that issue
// #79 records, and issue #99 tracks. The pressure API test family flakes on a
// Windows host, so the run is expected to fail sometimes. That failure IS the
// evidence, which is why the contract asserted here is mostly about keeping the
// log rather than about passing.
const windowsPressureValidationScript = "scripts/windows_pressure_validation.ps1"

// windowsValidationWorkflow dispatches the script by hand. It is deliberately
// not part of the CI gate: a repeated flake hunt must not block every push.
const windowsValidationWorkflow = ".github/workflows/windows-validation.yml"

func readPressureValidationScript(t *testing.T) string {
	t.Helper()
	body, err := readRepoFile(".", windowsPressureValidationScript)
	if err != nil {
		t.Fatalf("the Windows pressure validation script is missing: %v", err)
	}
	return body
}

func readValidationWorkflow(t *testing.T) string {
	t.Helper()
	body, err := readRepoFile(".", windowsValidationWorkflow)
	if err != nil {
		t.Fatalf("the Windows validation workflow is missing: %v", err)
	}
	return body
}

// TestWindowsPressureValidation_RunsTheRecordedCommand pins the command shape
// that issue #79 asks for. A run without -race or without repetition cannot
// observe the reported flake, so it would produce evidence that proves nothing.
func TestWindowsPressureValidation_RunsTheRecordedCommand(t *testing.T) {
	script := readPressureValidationScript(t)
	for _, required := range []string{
		"-race",
		"-shuffle=on",
		"^TestPollPressureAPI_",
		"./pkg/scanapp",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("the validation script does not pass %q to go test", required)
		}
	}
	if !strings.Contains(script, "-count=$Count") {
		t.Error("the validation script hardcodes the repetition count instead of taking it from $Count")
	}
	if !strings.Contains(script, "[int]$Count = 100") {
		t.Error("the validation script does not default $Count to the 100 repetitions issue #79 records")
	}
}

// TestWindowsPressureValidation_KeepsEvidenceWhenTheRunFails is the point of
// the whole script. A reproduction must survive, so the log has to be written
// and reported before any nonzero status returns.
func TestWindowsPressureValidation_KeepsEvidenceWhenTheRunFails(t *testing.T) {
	script := readPressureValidationScript(t)
	if !strings.Contains(script, "$OutputDir") {
		t.Fatal("the validation script does not take an output directory for its evidence")
	}
	logWrite := strings.Index(script, "pressure-family.log")
	if logWrite < 0 {
		t.Fatal("the validation script does not record the pressure family output")
	}
	failure := strings.Index(script, "exit $exitCode")
	if failure < 0 {
		t.Fatal("the validation script does not return the observed status")
	}
	if failure < logWrite {
		t.Error("the validation script returns its status before it records the evidence")
	}
	if !strings.Contains(script, "windows-environment.txt") {
		t.Error("the validation script does not record the Windows and Go versions issue #79 asks for")
	}
}

// TestWindowsValidationWorkflow_DispatchesByHandAndAlwaysUploads keeps the
// workflow thin and keeps the artifact. Without `if: always()` the interesting
// run — the failing one — would upload nothing.
func TestWindowsValidationWorkflow_DispatchesByHandAndAlwaysUploads(t *testing.T) {
	workflow := readValidationWorkflow(t)
	if !strings.Contains(workflow, "workflow_dispatch:") {
		t.Error("the validation workflow is not dispatched by hand")
	}
	for _, forbidden := range []string{"pull_request:", "push:"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("the validation workflow reacts to %q; a repeated flake hunt must not gate every change", forbidden)
		}
	}
	if !strings.Contains(workflow, "runs-on: windows-latest") {
		t.Error("the validation workflow does not run on a Native Windows host")
	}
	for _, required := range []string{
		"scripts/windows_setup_mingw.ps1",
		windowsPressureValidationScript,
		"actions/upload-artifact",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("the validation workflow does not use %q", required)
		}
	}
	upload := strings.Index(workflow, "actions/upload-artifact")
	always := strings.Index(workflow, "if: always()")
	if always < 0 || always > upload {
		t.Error("the validation workflow does not upload its evidence when the run fails")
	}
}
