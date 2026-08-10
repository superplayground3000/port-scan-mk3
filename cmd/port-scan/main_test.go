package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainValidate_JSONOutput(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.0/30,10.0.0.0/30,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"validate", "-cidr-file", cidr, "-port-file", port, "-format", "json"}, out, errOut)

	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("expected json output, got %s", out.String())
	}
}

func TestMainValidate_CustomCIDRColumnNames(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("source_ip,source_cidr,foo\n10.0.0.1,10.0.0.0/24,x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{
		"validate",
		"-cidr-file", cidr,
		"-port-file", port,
		"-cidr-ip-col", "source_ip",
		"-cidr-ip-cidr-col", "source_cidr",
		"-format", "json",
	}, out, errOut)

	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("expected json output, got %s", out.String())
	}
}

func TestMainValidate_WhenValidationFails_ReturnsExit1AndJSONDetail(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.1.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"validate", "-cidr-file", cidr, "-port-file", port, "-format", "json"}, out, errOut)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":false`) {
		t.Fatalf("expected invalid json output, got %s", out.String())
	}
	if !strings.Contains(out.String(), `detail`) {
		t.Fatalf("expected detail in json output, got %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr for validation failure, got %s", errOut.String())
	}
}

func TestMainValidate_WhenConfigParseFails_ReturnsExit2AndWritesStderr(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := runMain([]string{"validate", "-cidr-file", "", "-port-file", ""}, out, errOut)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty stdout on config parse failure, got %s", out.String())
	}
	if !strings.Contains(errOut.String(), "-cidr-file is required") {
		t.Fatalf("expected parse error on stderr, got %s", errOut.String())
	}
}

func TestHandleValidateCommand_WhenJSONValidationSucceeds_ReturnsExit0(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.0/30,10.0.0.0/30,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := handleValidateCommand([]string{"-cidr-file", cidr, "-port-file", port, "-format", "json"}, out, errOut)

	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("expected json output, got %s", out.String())
	}
}

func TestHandleValidateCommand_WhenConfigParseFails_ReturnsExit2AndWritesStderr(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := handleValidateCommand([]string{"-cidr-file", "", "-port-file", ""}, out, errOut)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty stdout on config parse failure, got %s", out.String())
	}
	if !strings.Contains(errOut.String(), "-cidr-file is required") {
		t.Fatalf("expected parse error on stderr, got %s", errOut.String())
	}
}

func TestHandleValidateCommand_WhenLegacyFlagValueIsInvalid_ReturnsExit2(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	code := handleValidateCommand([]string{"-cidr-file", "targets.csv", "-workers", "0"}, out, errOut)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty stdout on config parse failure, got %s", out.String())
	}
	if !strings.Contains(errOut.String(), "-workers") {
		t.Fatalf("expected worker error on stderr, got %s", errOut.String())
	}
}

func TestHandleValidateCommand_WhenJSONValidationSucceeds_PreservesDetailAndKeepsStderrEmpty(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := handleValidateCommand([]string{"-cidr-file", cidr, "-port-file", port, "-format", "json"}, out, errOut)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, errOut.String())
	}
	resp := mustDecodeValidationJSON(t, out.String())
	if !resp.Valid || resp.Detail != "ok" {
		t.Fatalf("unexpected validation response: %+v", resp)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr on successful validation, got %s", errOut.String())
	}
}

func TestMainValidate_WhenRichCSVAndPortFileMissing_ReturnsExit0(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "rich.csv")
	if err := os.WriteFile(cidr, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,8080,accept,P-1,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"validate", "-cidr-file", cidr, "-format", "json"}, out, errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("expected valid json output, got %s", out.String())
	}
}

func TestMainValidate_WhenDefaultCSVAndPortFileMissing_ReturnsExit1(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"validate", "-cidr-file", cidr, "-format", "json"}, out, errOut)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"valid":false`) {
		t.Fatalf("expected invalid json output, got %s", out.String())
	}
	if !strings.Contains(out.String(), "-port-file is required") {
		t.Fatalf("expected missing port-file detail, got %s", out.String())
	}
}
