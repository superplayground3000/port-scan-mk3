package preprocess_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocess"
)

func TestOutputPathForFabName_RejectsUnusableValues(t *testing.T) {
	var zeroValue preprocess.FabName
	ignoredErrorValue, _ := preprocess.ParseFabName("../escape")

	tests := []struct {
		name    string
		fabName preprocess.FabName
	}{
		{name: "direct zero value", fabName: zeroValue},
		{name: "ignored parser error", fabName: ignoredErrorValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := preprocess.OutputPathForFabName(t.TempDir(), tt.fabName, time.Time{})
			if !errors.Is(err, preprocess.ErrInvalidFabName) {
				t.Fatalf("OutputPathForFabName error = %v, want ErrInvalidFabName", err)
			}
		})
	}
}
