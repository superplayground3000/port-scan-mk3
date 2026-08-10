# ScanApp Test Naming Readability Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在不改測試邏輯與斷言的前提下，將 `pkg/scanapp` 全部測試名稱統一為 `Test<Function>_<Scenario>_<Expected>`。

**Architecture:** 本次只改 `func Test...` 宣告名稱，不碰測試內容。先用命名規則檢查建立「先失敗再通過」的驗證閘門，再分檔案做純重命名，最後跑 `go test ./pkg/scanapp` 驗證行為不變。整體採最小 diff 策略以降低 merge 衝突。

**Tech Stack:** Go 1.24.x, `go test`, `rg`, Git

---

技能參考：@superpowers/test-driven-development, @superpowers/verification-before-completion

### Task 1: 建立命名驗證閘門與對照表

**Files:**
- Modify: `docs/plans/2026-03-02-scanapp-test-naming-readability-implementation.md`
- Inspect: `pkg/scanapp/scan_test.go`
- Inspect: `pkg/scanapp/scan_helpers_test.go`
- Inspect: `pkg/scanapp/scan_observability_test.go`
- Inspect: `pkg/scanapp/resume_path_test.go`
- Inspect: `pkg/scanapp/batch_output_test.go`

**Step 1: 執行命名規則 pre-check（預期失敗）**

```bash
rg '^func Test' pkg/scanapp/*_test.go | rg -v 'Test[A-Za-z0-9]+_[A-Za-z0-9]+_[A-Za-z0-9]+'
```

Expected: 有輸出（代表目前仍有不符合規則的測試名稱）。

**Step 2: 固定舊名到新名對照（完整）**

```text
pkg/scanapp/scan_test.go
- TestRun_ResumeFromStateFile
  -> TestRun_WhenResumeStateFileProvided_ContinuesFromNextIndex
- TestRun_ResumeFallbackPathOnCancel
  -> TestRun_WhenCanceledWithoutResumePath_SavesFallbackResumeState
- TestRun_PressureAPIFailsThreeTimes
  -> TestRun_WhenPressureAPIFailsThreeTimes_ReturnsFatalErrorAndSavesResumeState
- TestFetchPressure
  -> TestFetchPressure_WhenResponseShapesVary_ReturnsParsedPressureOrError
- TestParsePortRows
  -> TestParsePortRows_WhenRowsContainTCPOnly_ReturnsPortsOrError
- TestBuildRuntime_DefaultPortsFromInput
  -> TestBuildRuntime_WhenChunkPortsEmpty_UsesDefaultInputPorts
- TestShouldSaveOnDispatchErr
  -> TestShouldSaveOnDispatchErr_WhenDispatchErrorVaries_ReturnsExpectedDecision
- TestLogger_TextAndJSON
  -> TestScanLogger_WhenTextOrJSONEnabled_FormatsOutputByMode
- TestPollPressureAPI_PauseResumeTransition
  -> TestPollPressureAPI_WhenPressureCrossesThreshold_TogglesPauseAndLogsTransition
- TestStartManualPauseMonitor_LogsStateChange
  -> TestStartManualPauseMonitor_WhenManualPauseChanges_LogsStateTransitions
- TestRun_ScansOnlyIPsListedByIPColumn
  -> TestRun_WhenIPColumnListsSubset_ScansOnlyListedIPs
- TestRun_WritesOpenedResultsCSV
  -> TestRun_WhenScanCompletes_WritesOpenRecordsToOpenedResultsCSV

pkg/scanapp/scan_helpers_test.go
- TestIndexToRuntimeTarget_Errors
  -> TestIndexToRuntimeTarget_WhenInputsInvalid_ReturnsErrors
- TestBuildCIDRGroups_ErrorAndSortPaths
  -> TestBuildCIDRGroups_WhenInputsVary_ReturnsErrorsAndSortedTargets
- TestBuildRuntime_TotalCountMismatch
  -> TestBuildRuntime_WhenTotalCountMismatch_ReturnsError
- TestReadCIDRFileAndReadPortFile_Errors
  -> TestReadCIDRFileAndReadPortFile_WhenFileMissing_ReturnsError
- TestLoadOrBuildChunks_Resume
  -> TestLoadOrBuildChunks_WhenResumePathProvided_LoadsStateFromFile
- TestResumePathPreference
  -> TestResumePath_WhenMultipleSourcesProvided_UsesPriorityOrder
- TestEnsureFDLimit_HugeWorkers
  -> TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError
- TestFetchPressure_MissingFieldAndUnsupportedType
  -> TestFetchPressure_WhenFieldMissingOrTypeUnsupported_ReturnsError
- TestPollPressureAPI_FirstTwoFailuresDoNotFatal
  -> TestPollPressureAPI_WhenFirstTwoRequestsFail_DoesNotReturnFatalError

pkg/scanapp/scan_observability_test.go
- Test_observability_progress_summary_events
  -> TestRun_WhenObservabilityJSONEnabled_EmitsProgressAndCompletionEvents

pkg/scanapp/resume_path_test.go
- TestResumePath_ResolveRules
  -> TestResumePath_WhenConfigVariantsProvided_ResolvesExpectedPath

pkg/scanapp/batch_output_test.go
- TestResolveBatchOutputPaths_NoCollision
  -> TestResolveBatchOutputPaths_WhenNoExistingFiles_UsesBaseTimestampNames
- TestResolveBatchOutputPaths_WithCollision
  -> TestResolveBatchOutputPaths_WhenExistingFilesCollide_AppendsIncrementingSuffix
```

**Step 3: Commit（規劃文件更新）**

```bash
git add docs/plans/2026-03-02-scanapp-test-naming-readability-implementation.md
git commit -m "docs(plan): define scanapp test rename map"
```

### Task 2: 重命名 `scan_test.go` 測試函式

**Files:**
- Modify: `pkg/scanapp/scan_test.go`（只改 `func Test...` 宣告）
- Test: `pkg/scanapp/scan_test.go`

**Step 1: 套用函式名稱重命名**

```go
// before
func TestFetchPressure(t *testing.T) {

// after
func TestFetchPressure_WhenResponseShapesVary_ReturnsParsedPressureOrError(t *testing.T) {
```

```go
// before
func TestRun_WritesOpenedResultsCSV(t *testing.T) {

// after
func TestRun_WhenScanCompletes_WritesOpenRecordsToOpenedResultsCSV(t *testing.T) {
```

**Step 2: 只跑 `scan_test.go` 驗證**

Run: `go test ./pkg/scanapp -run 'TestRun_When|TestFetchPressure_When|TestParsePortRows_When|TestBuildRuntime_When|TestShouldSaveOnDispatchErr_When|TestScanLogger_When|TestPollPressureAPI_When|TestStartManualPauseMonitor_When' -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/scanapp/scan_test.go
git commit -m "test(scanapp): rename scan_test cases to function-scenario-expected format"
```

### Task 3: 重命名 `scan_helpers_test.go` 測試函式

**Files:**
- Modify: `pkg/scanapp/scan_helpers_test.go`（只改 `func Test...` 宣告）
- Test: `pkg/scanapp/scan_helpers_test.go`

**Step 1: 套用函式名稱重命名**

```go
// before
func TestBuildCIDRGroups_ErrorAndSortPaths(t *testing.T) {

// after
func TestBuildCIDRGroups_WhenInputsVary_ReturnsErrorsAndSortedTargets(t *testing.T) {
```

```go
// before
func TestPollPressureAPI_FirstTwoFailuresDoNotFatal(t *testing.T) {

// after
func TestPollPressureAPI_WhenFirstTwoRequestsFail_DoesNotReturnFatalError(t *testing.T) {
```

**Step 2: 只跑 helpers 相關測試驗證**

Run: `go test ./pkg/scanapp -run 'TestIndexToRuntimeTarget_When|TestBuildCIDRGroups_When|TestBuildRuntime_WhenTotalCountMismatch|TestReadCIDRFileAndReadPortFile_When|TestLoadOrBuildChunks_When|TestResumePath_WhenMultipleSourcesProvided|TestEnsureFDLimit_When|TestFetchPressure_WhenFieldMissingOrTypeUnsupported|TestPollPressureAPI_WhenFirstTwoRequestsFail' -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/scanapp/scan_helpers_test.go
git commit -m "test(scanapp): rename helper-focused tests for readability"
```

### Task 4: 重命名其餘 `pkg/scanapp` 測試檔

**Files:**
- Modify: `pkg/scanapp/scan_observability_test.go`（只改 `func Test...` 宣告）
- Modify: `pkg/scanapp/resume_path_test.go`（只改 `func Test...` 宣告）
- Modify: `pkg/scanapp/batch_output_test.go`（只改 `func Test...` 宣告）

**Step 1: 套用名稱重命名**

```go
// pkg/scanapp/scan_observability_test.go
// before
func Test_observability_progress_summary_events(t *testing.T) {

// after
func TestRun_WhenObservabilityJSONEnabled_EmitsProgressAndCompletionEvents(t *testing.T) {
```

```go
// pkg/scanapp/resume_path_test.go
// before
func TestResumePath_ResolveRules(t *testing.T) {

// after
func TestResumePath_WhenConfigVariantsProvided_ResolvesExpectedPath(t *testing.T) {
```

```go
// pkg/scanapp/batch_output_test.go
// before
func TestResolveBatchOutputPaths_NoCollision(t *testing.T) {

// after
func TestResolveBatchOutputPaths_WhenNoExistingFiles_UsesBaseTimestampNames(t *testing.T) {
```

**Step 2: 只跑新增命名的三個檔案測試**

Run: `go test ./pkg/scanapp -run 'TestRun_WhenObservabilityJSONEnabled|TestResumePath_WhenConfigVariantsProvided|TestResolveBatchOutputPaths_When' -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/scanapp/scan_observability_test.go pkg/scanapp/resume_path_test.go pkg/scanapp/batch_output_test.go
git commit -m "test(scanapp): normalize remaining test names"
```

### Task 5: 完整驗證命名規則與套件測試

**Files:**
- Test: `pkg/scanapp/*.go`

**Step 1: 執行命名規則 post-check（預期通過）**

```bash
rg '^func Test' pkg/scanapp/*_test.go | rg -v 'Test[A-Za-z0-9]+_[A-Za-z0-9]+_[A-Za-z0-9]+'
```

Expected: 無輸出。

**Step 2: 執行套件完整測試**

Run: `go test ./pkg/scanapp -count=1`
Expected: PASS

**Step 3: 可選全專案回歸**

Run: `go test ./... -count=1`
Expected: PASS

### Task 6: 收尾提交

**Files:**
- Modify: `pkg/scanapp/scan_test.go`
- Modify: `pkg/scanapp/scan_helpers_test.go`
- Modify: `pkg/scanapp/scan_observability_test.go`
- Modify: `pkg/scanapp/resume_path_test.go`
- Modify: `pkg/scanapp/batch_output_test.go`

**Step 1: 檢查最終差異僅有函式名稱**

Run: `git diff -- pkg/scanapp/scan_test.go pkg/scanapp/scan_helpers_test.go pkg/scanapp/scan_observability_test.go pkg/scanapp/resume_path_test.go pkg/scanapp/batch_output_test.go`
Expected: 只看到 `func Test...` 名稱變更，無測試邏輯差異。

**Step 2: 產生最終提交**

```bash
git add pkg/scanapp/scan_test.go pkg/scanapp/scan_helpers_test.go pkg/scanapp/scan_observability_test.go pkg/scanapp/resume_path_test.go pkg/scanapp/batch_output_test.go
git commit -m "test(scanapp): standardize test names for readability"
```
