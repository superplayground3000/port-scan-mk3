package cidrutil

import (
	"bytes"
	"log"
	"slices"
	"strings"
	"testing"
)

func TestParseDenyCSV(t *testing.T) {
	content := "dst_network_segment,decision\n10.0.0.0/8,deny\n192.168.0.0/16,accept\n"
	entries, err := ParseDenyCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 deny entry, got %d", len(entries))
	}
	if entries[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entries[0].Network)
	}
}

func TestParseOpenCSV(t *testing.T) {
	content := "segment,status\n10.0.0.0/8,closed\n192.168.0.0/16,open\n172.16.0.0/12,open\n"
	entries, err := ParseOpenCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 open entries, got %d", len(entries))
	}
	if entries[0].Network != "192.168.0.0/16" {
		t.Errorf("expected 192.168.0.0/16, got %s", entries[0].Network)
	}
	if entries[1].Network != "172.16.0.0/12" {
		t.Errorf("expected 172.16.0.0/12, got %s", entries[1].Network)
	}
}

func TestParseDenyCSVStreaming(t *testing.T) {
	// Verify streaming reader works with io.Reader interface
	content := "dst_network_segment,decision\n10.0.0.0/8,deny\n"
	reader := strings.NewReader(content)

	r := NewDenyCSVReader(reader)
	entries, err := r.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 deny entry, got %d", len(entries))
	}
	if entries[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entries[0].Network)
	}
}

func TestParseOpenCSVStreaming(t *testing.T) {
	// Verify streaming reader works with io.Reader interface for open entries
	content := "segment,status\n192.168.0.0/16,open\n"
	reader := strings.NewReader(content)

	r := NewOpenCSVReader(reader)
	entries, err := r.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 open entry, got %d", len(entries))
	}
	if entries[0].Network != "192.168.0.0/16" {
		t.Errorf("expected 192.168.0.0/16, got %s", entries[0].Network)
	}
}

func TestParseDenyCSVMalformedLine(t *testing.T) {
	content := "dst_network_segment,decision\n\"unclosed quote\n"
	entries, err := ParseDenyCSV(content)
	if err == nil {
		t.Fatal("expected a malformed CSV error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
	if !strings.Contains(err.Error(), "deny input record 2 line 2 column 17: malformed CSV") {
		t.Errorf("error = %q, want deny record location", err)
	}
}

func TestParseDenyCSVShortLine(t *testing.T) {
	content := "dst_network_segment,decision\nonly-one-column\n"
	entries, err := ParseDenyCSV(content)
	if err == nil {
		t.Fatal("expected a short-record error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
}

func TestParseDenyCSVInvalidCIDR(t *testing.T) {
	content := "dst_network_segment,decision\nnot-a-cidr,deny\n"
	entries, err := ParseDenyCSV(content)
	if err == nil {
		t.Fatal("expected an invalid CIDR error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
	if !strings.Contains(err.Error(), "deny input record 2 line 2 column 1: invalid CIDR") {
		t.Errorf("error = %q, want deny record location", err)
	}
}

func TestParseOpenCSVMalformedLine(t *testing.T) {
	content := "segment,status\n\"unclosed quote\n"
	entries, err := ParseOpenCSV(content)
	if err == nil {
		t.Fatal("expected a malformed CSV error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
	if !strings.Contains(err.Error(), "open input record 2 line 2 column 17: malformed CSV") {
		t.Errorf("error = %q, want open record location", err)
	}
}

func TestParseOpenCSVShortLine(t *testing.T) {
	content := "segment,status\nonly-one\n"
	entries, err := ParseOpenCSV(content)
	if err == nil {
		t.Fatal("expected a short-record error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
}

func TestParseOpenCSVInvalidCIDR(t *testing.T) {
	content := "segment,status\nnot-valid,open\n"
	entries, err := ParseOpenCSV(content)
	if err == nil {
		t.Fatal("expected an invalid CIDR error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
	if !strings.Contains(err.Error(), "open input record 2 line 2 column 1: invalid CIDR") {
		t.Errorf("error = %q, want open record location", err)
	}
}

func TestCSVEntryPointsRejectShortLateIndexRecord(t *testing.T) {
	tests := []struct {
		name    string
		content string
		parse   func(string) ([]CIDREntry, error)
	}{
		{
			name:    "deny string",
			content: "metadata,decision,unused,dst_network_segment\nvalue,deny\n",
			parse:   ParseDenyCSV,
		},
		{
			name:    "deny reader",
			content: "metadata,decision,unused,dst_network_segment\nvalue,deny\n",
			parse: func(content string) ([]CIDREntry, error) {
				return NewDenyCSVReader(strings.NewReader(content)).ReadAll()
			},
		},
		{
			name:    "open string",
			content: "status,metadata,unused,segment\nopen,value\n",
			parse:   ParseOpenCSV,
		},
		{
			name:    "open reader",
			content: "status,metadata,unused,segment\nopen,value\n",
			parse: func(content string) ([]CIDREntry, error) {
				return NewOpenCSVReader(strings.NewReader(content)).ReadAll()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := tt.parse(tt.content)
			if err == nil {
				t.Fatalf("expected a short-record error, got entries %#v", entries)
			}
			if entries != nil {
				t.Errorf("entries = %#v, want nil", entries)
			}
			if !strings.Contains(err.Error(), "record 2") {
				t.Errorf("error %q does not identify CSV record 2", err)
			}
			if !strings.Contains(err.Error(), "line 2 column 1") {
				t.Errorf("error %q does not identify the short record location", err)
			}
			if !strings.Contains(err.Error(), "requires 4 fields, got 2") {
				t.Errorf("error %q does not contain the field counts", err)
			}
		})
	}
}

func TestCSVEntryPointsAreEquivalent(t *testing.T) {
	contentByRole := map[string]string{
		"deny": "dst_network_segment,decision\n10.0.0.0/8,deny\nnot-a-cidr,deny\n",
		"open": "segment,status\n10.0.0.1/32,open\nnot-a-cidr,open\n",
	}
	validContentByRole := map[string]string{
		"deny": "dst_network_segment,decision,extra\n10.0.0.0/8,DENY,value\n192.168.0.0/16,accept,value\n",
		"open": "segment,status,extra\n10.0.0.1/32,OPEN,value\n192.168.0.1/32,closed,value\n",
	}
	tests := []struct {
		role        string
		parseString func(string) ([]CIDREntry, error)
		parseReader func(string) ([]CIDREntry, error)
	}{
		{
			role:        "deny",
			parseString: ParseDenyCSV,
			parseReader: func(content string) ([]CIDREntry, error) {
				return NewDenyCSVReader(strings.NewReader(content)).ReadAll()
			},
		},
		{
			role:        "open",
			parseString: ParseOpenCSV,
			parseReader: func(content string) ([]CIDREntry, error) {
				return NewOpenCSVReader(strings.NewReader(content)).ReadAll()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			stringEntries, stringErr := tt.parseString(validContentByRole[tt.role])
			readerEntries, readerErr := tt.parseReader(validContentByRole[tt.role])
			if stringErr != nil || readerErr != nil {
				t.Fatalf("valid parse errors: string %v, reader %v", stringErr, readerErr)
			}
			if !slices.Equal(stringEntries, readerEntries) {
				t.Errorf("valid entries differ: string %#v, reader %#v", stringEntries, readerEntries)
			}
			if len(stringEntries) != 1 {
				t.Errorf("valid entries = %#v, want one filtered entry", stringEntries)
			}

			stringEntries, stringErr = tt.parseString(contentByRole[tt.role])
			readerEntries, readerErr = tt.parseReader(contentByRole[tt.role])
			if stringErr == nil || readerErr == nil {
				t.Fatalf("expected both entry points to fail, got string %v and reader %v", stringErr, readerErr)
			}
			if stringEntries != nil || readerEntries != nil {
				t.Fatalf("partial entries: string %#v, reader %#v", stringEntries, readerEntries)
			}
			if stringErr.Error() != readerErr.Error() {
				t.Errorf("errors differ: string %q, reader %q", stringErr, readerErr)
			}
		})
	}
}

func TestCSVQuotedMultilineAndBlankLines(t *testing.T) {
	content := "   \n\nsegment,status,note\n\t\n10.0.0.1/32,open,\"first line\nsecond line\"\n   \n"
	entries, err := NewOpenCSVReader(strings.NewReader(content)).ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Network != "10.0.0.1/32" {
		t.Fatalf("entries = %#v, want one multiline CSV entry", entries)
	}
}

func TestCSVParserDoesNotUseGlobalLoggerOrExposeRawRecord(t *testing.T) {
	previousOutput := log.Writer()
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
	})

	content := "dst_network_segment,decision,private_metadata\nnot-a-cidr,deny,do-not-report-this-field\n"
	entries, err := ParseDenyCSV(content)
	if err == nil {
		t.Fatal("expected an invalid CIDR error")
	}
	if entries != nil {
		t.Errorf("entries = %#v, want nil", entries)
	}
	if logOutput.Len() != 0 {
		t.Errorf("global log output = %q, want empty output", logOutput.String())
	}
	if strings.Contains(err.Error(), "do-not-report-this-field") {
		t.Errorf("error exposes the complete raw record: %q", err)
	}
}

func TestCSVEntryPointsStopAtFirstInvalidRecord(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		parse      func(string) ([]CIDREntry, error)
		wantDetail string
	}{
		{
			name:       "deny string malformed CSV",
			content:    "dst_network_segment,decision\n10.0.0.0/8,deny\n\"unterminated,deny\n",
			parse:      ParseDenyCSV,
			wantDetail: "malformed CSV",
		},
		{
			name:    "deny reader invalid CIDR",
			content: "dst_network_segment,decision\n10.0.0.0/8,deny\nnot-a-cidr,deny\n",
			parse: func(content string) ([]CIDREntry, error) {
				return NewDenyCSVReader(strings.NewReader(content)).ReadAll()
			},
			wantDetail: "invalid CIDR",
		},
		{
			name:       "open string malformed CSV",
			content:    "segment,status\n10.0.0.1/32,open\n\"unterminated,open\n",
			parse:      ParseOpenCSV,
			wantDetail: "malformed CSV",
		},
		{
			name:    "open reader invalid CIDR",
			content: "segment,status\n10.0.0.1/32,open\nnot-a-cidr,open\n",
			parse: func(content string) ([]CIDREntry, error) {
				return NewOpenCSVReader(strings.NewReader(content)).ReadAll()
			},
			wantDetail: "invalid CIDR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := tt.parse(tt.content)
			if err == nil {
				t.Fatalf("expected an input error, got entries %#v", entries)
			}
			if entries != nil {
				t.Errorf("entries = %#v, want nil", entries)
			}
			if !strings.Contains(err.Error(), "record 3") {
				t.Errorf("error %q does not identify CSV record 3", err)
			}
			if !strings.Contains(err.Error(), tt.wantDetail) {
				t.Errorf("error %q does not contain %q", err, tt.wantDetail)
			}
		})
	}
}

func TestCSVHeaderContract(t *testing.T) {
	tests := []struct {
		name        string
		parse       func(string) ([]CIDREntry, error)
		content     string
		wantNetwork string
		wantError   string
	}{
		{
			name:        "deny official headers at arbitrary indexes",
			parse:       ParseDenyCSV,
			content:     "decision,metadata,dst_network_segment\ndeny,value,10.0.0.0/8\n",
			wantNetwork: "10.0.0.0/8",
		},
		{
			name: "open official headers at arbitrary indexes through reader",
			parse: func(content string) ([]CIDREntry, error) {
				return NewOpenCSVReader(strings.NewReader(content)).ReadAll()
			},
			content:     "metadata,status,segment\nvalue,open,192.168.0.0/16\n",
			wantNetwork: "192.168.0.0/16",
		},
		{
			name:        "deny legacy columns",
			parse:       ParseDenyCSV,
			content:     "network,action\n10.0.0.0/8,deny\n",
			wantNetwork: "10.0.0.0/8",
		},
		{
			name: "open legacy columns through reader",
			parse: func(content string) ([]CIDREntry, error) {
				return NewOpenCSVReader(strings.NewReader(content)).ReadAll()
			},
			content:     "network,result\n192.168.0.0/16,open\n",
			wantNetwork: "192.168.0.0/16",
		},
		{
			name:      "deny only CIDR header",
			parse:     ParseDenyCSV,
			content:   "dst_network_segment,action\n10.0.0.0/8,deny\n",
			wantError: "partial official header",
		},
		{
			name:      "deny only filter header",
			parse:     ParseDenyCSV,
			content:   "network,decision\n10.0.0.0/8,deny\n",
			wantError: "partial official header",
		},
		{
			name:      "open only CIDR header",
			parse:     ParseOpenCSV,
			content:   "segment,result\n10.0.0.1/32,open\n",
			wantError: "partial official header",
		},
		{
			name:      "open only filter header",
			parse:     ParseOpenCSV,
			content:   "network,status\n10.0.0.1/32,open\n",
			wantError: "partial official header",
		},
		{
			name:      "empty input",
			parse:     ParseDenyCSV,
			wantError: "missing header",
		},
		{
			name:      "blank input",
			parse:     ParseOpenCSV,
			content:   "\n\n",
			wantError: "missing header",
		},
		{
			name:      "legacy header has one field",
			parse:     ParseDenyCSV,
			content:   "network\n10.0.0.0/8\n",
			wantError: "header requires at least 2 fields, got 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := tt.parse(tt.content)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected %q error, got entries %#v", tt.wantError, entries)
				}
				if entries != nil {
					t.Errorf("entries = %#v, want nil", entries)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("error = %q, want detail %q", err, tt.wantError)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(entries) != 1 || entries[0].Network != tt.wantNetwork {
				t.Fatalf("entries = %#v, want network %q", entries, tt.wantNetwork)
			}
		})
	}
}
