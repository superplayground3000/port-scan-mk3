package preprocesscfg

import "testing"

func TestRichHeader_ColumnCount(t *testing.T) {
	h := RichHeader()
	if len(h) != 10 {
		t.Fatalf("expected 10 columns, got %d", len(h))
	}
}

func TestRichHeader_FirstAndLastColumns(t *testing.T) {
	h := RichHeader()
	if h[0] != ColSrcIP {
		t.Errorf("first column: expected %s, got %s", ColSrcIP, h[0])
	}
	if h[9] != ColReason {
		t.Errorf("last column: expected %s, got %s", ColReason, h[9])
	}
}
