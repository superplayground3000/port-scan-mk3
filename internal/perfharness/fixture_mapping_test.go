package perfharness_test

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestFullFixtureMatrixDeclaresProductionCaseAliases(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	if len(contract.FixtureCases) != len(contract.FullFixtures) {
		t.Fatalf("fixture mappings = %d, want %d", len(contract.FixtureCases), len(contract.FullFixtures))
	}
	for index, mapping := range contract.FixtureCases {
		if mapping.Fixture != contract.FullFixtures[index] {
			t.Fatalf("fixture mapping %d = %+v, want %+v", index, mapping.Fixture, contract.FullFixtures[index])
		}
		if len(mapping.CaseNames) == 0 {
			t.Fatalf("fixture mapping %d has no production case alias", index)
		}
		for _, name := range mapping.CaseNames {
			if name == "" {
				t.Fatalf("fixture mapping %d has an empty case alias", index)
			}
		}
	}
}
