package validate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type configurationStub struct {
	values config.ValidateValues
	err    error
}

func (s configurationStub) Resolve() (config.ValidateValues, error) {
	return s.values, s.err
}

func mustValidateConfig(t *testing.T, values config.ValidateValues) config.ValidateConfig {
	t.Helper()
	if values.CIDRIPCol == "" {
		values.CIDRIPCol = "ip"
	}
	if values.CIDRIPCidrCol == "" {
		values.CIDRIPCidrCol = "ip_cidr"
	}
	if values.Format == "" {
		values.Format = "human"
	}
	cfg, err := config.NewValidate(values)
	if err != nil {
		t.Fatalf("NewValidate() error = %v", err)
	}
	return cfg
}

func TestInputs_WhenConfigurationCannotResolve_ReturnsInvalidResultBeforeFileAccess(t *testing.T) {
	result := Inputs(configurationStub{err: errors.New("configuration is not initialized")})
	if result.Valid {
		t.Fatalf("Inputs() = %+v, want invalid result", result)
	}
	if !strings.Contains(result.Detail, "configuration is not initialized") {
		t.Fatalf("Detail = %q, want configuration error", result.Detail)
	}
}

func TestInputs_WhenFilesAreValid_ReturnsValidResult(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Inputs(mustValidateConfig(t, config.ValidateValues{
		CIDRFile:      cidr,
		PortFile:      port,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
	}))

	if !result.Valid || result.Detail != "ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestInputs_WhenCIDRRowsInvalid_ReturnsInvalidDetail(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	port := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.1.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(port, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Inputs(mustValidateConfig(t, config.ValidateValues{
		CIDRFile:      cidr,
		PortFile:      port,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
	}))

	if result.Valid {
		t.Fatalf("expected invalid result, got %+v", result)
	}
	if !strings.Contains(result.Detail, "outside ip_cidr") {
		t.Fatalf("unexpected detail: %s", result.Detail)
	}
}

func TestInputs_WhenRichCSVAndPortFileMissing_ReturnsValidResult(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "rich.csv")
	if err := os.WriteFile(cidr, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,8080,accept,P-1,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Inputs(mustValidateConfig(t, config.ValidateValues{
		CIDRFile:      cidr,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
	}))
	if !result.Valid {
		t.Fatalf("expected valid result, got %+v", result)
	}
}

func TestInputs_WhenDefaultCSVAndPortFileMissing_ReturnsInvalidDetail(t *testing.T) {
	tmp := t.TempDir()
	cidr := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidr, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.0/24,a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Inputs(mustValidateConfig(t, config.ValidateValues{
		CIDRFile:      cidr,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
	}))
	if result.Valid {
		t.Fatalf("expected invalid result, got %+v", result)
	}
	if !strings.Contains(result.Detail, "-port-file is required") {
		t.Fatalf("unexpected detail: %s", result.Detail)
	}
}
