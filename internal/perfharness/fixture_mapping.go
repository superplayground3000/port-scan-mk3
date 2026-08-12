package perfharness

import "fmt"

func fixtureCaseMappings(fixtures []FixtureSpec) []FixtureCaseMapping {
	mappings := make([]FixtureCaseMapping, 0, len(fixtures))
	for _, fixture := range fixtures {
		mapping := FixtureCaseMapping{Fixture: fixture}
		switch fixture.Family {
		case FamilyRecordHeavy, FamilyPortHeavy:
			mapping.CaseNames = []string{fixtureCaseName(fixture)}
		case FamilyCandidateHeavy:
			mapping.CaseNames = []string{"candidate-heavy/pre-ping"}
		case FamilyTaskHeavy:
			mapping.CaseNames = []string{"task-heavy/bucket-generation"}
		case FamilyOutputHeavy:
			for _, flush := range []int{1, 1000, 0} {
				mapping.CaseNames = append(mapping.CaseNames, fmt.Sprintf("output-heavy/results-%d/flush-%d", fixture.Scale.ExpectedOutputs, flush))
			}
		case FamilySnapshotHeavy:
			loadName, saveName := SnapshotCaseNames(fixture)
			mapping.CaseNames = []string{loadName, saveName}
		case FamilyResumeHeavy:
			mapping.CaseNames = []string{fmt.Sprintf("production-resume/%d", fixture.CompletionPercent)}
		case FamilyRichRecordMixed, FamilyRichUniqueKey, FamilyRichHotKey, FamilyRichPrecheck:
			mapping.CaseNames = []string{fixtureCaseName(fixture), "production-rich/" + string(fixture.Family)}
		case FamilyRichDeny:
			mapping.CaseNames = []string{fixtureCaseName(fixture), "production-rich-deny/" + fixture.Shape}
		}
		mappings = append(mappings, mapping)
	}
	return mappings
}
