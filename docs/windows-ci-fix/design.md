# Windows CI 修正設計

追蹤 issue: #47 · 基準 commit: `e50dcd1` (2.1.0) · 撰寫日期: 2026-08-01

> **狀態(2026-08-02)**
> - **Part 1 完成。** PR #48 / #49 / #50 已合併,Windows CI 在 master 上首次全綠
>   (零 FAIL、零 SKIP)。PR #52 移除了 Windows job 的 `continue-on-error`,
>   它現在是**會擋合併的 gate**。
> - **Part 2 進行中。** 缺口 1/2/3/4 的 seam 已與使用者議定並開工。
> - **副產物:** 審查期間發現一個真實的既有 production bug,見 issue #51
>   (寫入失敗後 `ScannedCount` 仍被遞增,續掃可能靜默丟棄結果)。
>
> 下方內容維持撰寫當時的分析與決策紀錄,不隨進度改寫,以保留判斷依據。

## 背景與目標

`master` 的 **Cross-platform build + test (Windows)** CI job 目前是紅的，共 10 個失敗
（3 個因 PR #43 的負載增加而新浮現，7 個在 #43 之前就存在）。Linux quality gate、
Docker e2e、gitleaks 全綠。

本設計分兩部分，且**兩者目的不同、不可互相取代**：

- **Part 1 — 還債**：讓現有 10 個測試在 Windows 上正確地綠。這是修測試品質，
  **不是修產品 bug**。完成後 Windows CI 才有資格當一道真正的 gate。
- **Part 2 — 補覆蓋**：針對 Windows 真正的風險區域新增測試。Part 1 全綠**並不
  代表** 2.1.0 在 Windows 上安全，因為真正危險的地方目前一個測試都沒有。

## 已驗證的事實（先讀這段，避免重複調查）

程式碼實際讀過後確認的結論，含證據位置：

| 事項 | 結論 | 證據 |
|---|---|---|
| `OutputPath` 路徑組合 | **正確**，用 `filepath.Join` | `pkg/preprocess/output.go:19` |
| `resumePath` 路徑組合 | **正確**，用 `filepath.Dir` + `filepath.Join` | `pkg/scanapp/resume_path.go:16-20` |
| `ensureFDLimit` 雙平台 | **刻意分歧**：unix 檢查 RLIMIT_NOFILE，windows 是 no-op | `fdlimit_unix.go` (`//go:build !windows`) / `fdlimit_windows.go:6-8` |
| append 時的 CRLF header | **已處理**，`TrimRight(firstLine, "\r\n")` | `pkg/scanapp/output_files.go:111` |
| 正式程式碼手工拼路徑 | **沒有**。`strings.Split(.., "/")` 只出現在拆解 `80/tcp` 埠字串 | `pkg/input/ports.go:39`、`pkg/scanapp/scan_helpers.go:55`、`cmd/csv-transform/transform.go:19` |
| 結果檔寫入方式 | `os.Create` / `os.OpenFile(O_RDWR\|O_CREATE)`，**沒有** `.tmp`+rename | `pkg/scanapp/output_files.go:60,69` |
| `/32` 是否被 broadcast 過濾掉 | **不會**，`/31` 和 `/32` 明確跳過 | `pkg/task/broadcast.go:30-32` |

**重要推論**：Windows 上最典型的兩個地雷——「`.tmp` + rename 撞上開啟中的檔案」
與「CRLF 讓 header 比對失敗」——這個 repo **都已經避開了**。所以 Part 2 不需要
針對它們寫測試，應該把力氣放在下面真正的缺口。

---

# Part 1 — 還債：讓 Windows CI 值得信任

10 個失敗分成四組，每組成因不同、修法不同。

## A 組：測試把 Linux 斜線寫死（3 個）

- `TestOutputPath`、`TestOutputPath_SpecialCharsInFab` — `pkg/preprocess/output_test.go:15,24`
- `TestResumePath_WhenMultipleSourcesProvided_UsesPriorityOrder` — `pkg/scanapp/scan_helpers_test.go:700`
  （只有第三段斷言 `"/tmp/"+defaultResumeStateFile` 會壞，前兩段是純字串傳遞，跨平台沒問題）

**成因**：正式程式用 `filepath.Join`，在 Windows 回傳 `\data\out\...`；測試的期待值
寫死成 `/data/out/...`，字串比對不相等。

**修法**：斷言改成比對 `filepath.ToSlash(got)`，期待值維持現在的字面字串。

```go
got := filepath.ToSlash(OutputPath("/data/out", "dc-east", ts))
expected := "/data/out/dc-east/20260416T153000Z/input.csv"
```

**為什麼不用 `expected := filepath.Join(...)`**：那會讓測試和正式程式呼叫同一個
函式，變成恆真的套套邏輯（tautology）——`Join` 若有 bug，測試也跟著錯，等於沒測。
`ToSlash` 只正規化分隔符，期待值仍是人類可讀的字面路徑，結構斷言的價值保留。

**風險**：低。純測試改動，不碰正式程式。

## B 組：Windows 不認得沒有副檔名的執行檔（3 個）

- `TestEndToEnd` — `cmd/cidr-compare/integration_test.go:13,34`
- `TestCLIRequiredFlags`、`TestEnvVarFallback` — `cmd/cidr-compare/main_test.go:14,22,37,63`

**成因**：測試先 `go build -o cidr-compare-test .`，再 `exec.Command("./cidr-compare-test")`。
Windows 依副檔名（`PATHEXT`）判斷可執行性，無副檔名的檔案不被視為程式，
因此報 `executable file not found in %PATH%`。**產品程式在 Windows 上從未被啟動過**，
這三個測試目前等於零覆蓋。

**修法**：抽出單一 test helper，三處共用（現在是三份重複的 build 程式碼）。

```go
// cmd/cidr-compare/helper_test.go
func buildTestBinary(t *testing.T) string {
    t.Helper()
    name := "cidr-compare-test"
    if runtime.GOOS == "windows" {
        name += ".exe"
    }
    bin := filepath.Join(t.TempDir(), name)   // 不再污染套件目錄
    out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
    if err != nil {
        t.Fatalf("build failed: %v\n%s", err, out)
    }
    return bin                                 // 絕對路徑，不需要 "./" 前綴
}
```

呼叫端一律 `exec.Command(bin, args...)`。

**附帶效益**：改用 `t.TempDir()` 後可移除 `defer os.Remove(...)`，也消除多個測試
同時 build 同一個檔名互相覆寫的隱性競態。

**價值**：這組修好後，會成為**目前唯一真正在 Windows 上端到端跑起完整 CLI**
的測試，投資報酬率最高，建議優先。

**風險**：低—中。要確認 `go build` 在 CI 的 Windows runner 上工作目錄正確。

## C 組：平台行為本來就不同（1 個）

- `TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError` — `pkg/scanapp/scan_helpers_test.go:741`

**成因**：測試要求「十億個 worker 必須回傳錯誤」，但 Windows 沒有 RLIMIT_NOFILE，
`fdlimit_windows.go` 刻意回傳 `nil`。**兩邊的正式程式都是對的**，是測試沒有分平台。

**修法**：把測試依 build tag 拆成兩個檔案，**兩個平台各自斷言自己的契約**：

- `fdlimit_unix_test.go`（`//go:build !windows`）— 保留現有斷言：超量 worker 回傳錯誤。
- `fdlimit_windows_test.go`（`//go:build windows`）— 斷言已文件化的 no-op 契約：
  無論 worker 數多大都回傳 `nil`，且不 panic。

**明確禁止**：直接加 `if runtime.GOOS == "windows" { t.Skip() }`。那是把契約
「消失」而不是「驗證」，屬於 `20-judgment-rubric.md` R5 的錯誤方向。

**附帶產出（需你決定，不在本次範圍內）**：這組暴露了一個真實產品議題——
Windows 上對誇張的 `-workers` 值**完全沒有任何保護**。Windows 雖無 fd 限制，
但有 handle 與暫時埠（預設約 16k）上限。是否要為 Windows 加一道上限檢查，
是產品決策，建議另開 issue，不要夾在本次修正裡。

## D 組：時間抓太緊，Windows 計時器較粗（3 個，即 #43 後新浮現的那三個）

- `TestDashboardRuntime_StartRefreshLoopUntilStopped` — `pkg/scanapp/dashboard_runtime_test.go:68`
  （10ms 間隔，80ms 內要 render 兩次，且帶 `t.Parallel()`）
- `TestPollPressureAPI_FractionalPressureRoundsUp_TriggersPause` — `pkg/scanapp/pressure_monitor_test.go:326`
  （10ms 輪詢，`time.Sleep(30ms)` 後斷言已暫停）
- `TestRun_WhenCanceled_ResumeStateReflectsAllCompletedScans` — `pkg/scanapp/scan_test.go:1302`
  （80ms 後 cancel，斷言 `ScannedCount > 0`）

**成因**：Windows 預設計時器精度約 **15.6ms**，遠粗於 Linux。要求「10ms 一次」
實際可能 15.6ms 才一次；CI 上又同時跑大量測試搶 CPU。這類測試量到的是
**機器當下的忙碌程度**，不是程式正確性。#43 新增了多個測試檔（`output_state_test.go`、
`csv_writer_appending_test.go`、`csv_writer_canonical_test.go`），Windows runner 更忙，
原本勉強及格的餘裕就不夠了——**不是 #43 的程式改壞的**。

**修法原則：把「睡固定時間再檢查」換成「等到事件發生，最多等很久」。**
這樣正常機器仍然很快結束，慢機器只是多等一下，只有真的壞掉才會失敗。

新增共用 helper（放 `internal/testkit`）：

```go
// WaitFor 反覆檢查 cond，直到成立或逾時；逾時才失敗。
func WaitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if cond() { return }
        time.Sleep(2 * time.Millisecond)
    }
    t.Fatalf("timed out after %s waiting for: %s", timeout, msg)
}
```

逐一套用：

1. **dashboard**：renderer stub 在第 2 次 render 時 `close(rendered)`；主體改成
   `select { case <-rendered: case <-time.After(5*time.Second): t.Fatal(...) }`。
   斷言強度不變（仍要求至少 render 兩次），但餘裕從 80ms 變 5s。
2. **pressure**：HTTP handler 被打到時送訊號；改用 `WaitFor(t, 5*time.Second, ...,
   ctrl.IsPaused)` 取代 `time.Sleep(30ms)`。
3. **cancel（重點，見下）**。

### D-3：`TestRun_WhenCanceled...` 需要兩處改動

這是唯一踩在 #43 新功能（Ctrl+C 續掃耐久性）上的測試，值得單獨處理。

**改動一：取消時機改為事件驅動。** 現在是「固定睡 80ms 再 cancel」，在慢機器上
可能一個掃描都還沒完成就取消，於是 `ScannedCount` 為 0 而失敗。改為
**觀察到第一筆結果後才 cancel**（透過既有的 hook／輪詢 resume 檔），
讓「至少掃到一筆」成為前置條件而非賭注。

**改動二：改用真實的本地 listener。** 目前掃 `127.0.0.0/30`（即 127.0.0.0～127.0.0.3）。
Linux 把整個 `127.0.0.0/8` 當 loopback，連到 127.0.0.2 會立刻被拒絕；
**Windows 通常只綁定 127.0.0.1**，連到 127.0.0.2 的行為可能不同（可能拖到逾時）。

> ⚠️ 這一點是**尚未證實的假設**，無法在 Linux 上驗證，必須由 Windows CI 判定。

建議改成 `net.Listen("tcp", "127.0.0.1:0")` 起一個真的 listener，掃 `127.0.0.1/32`
搭配該埠。已確認 `/32` 不會被 broadcast 過濾掉（`pkg/task/broadcast.go:30-32`），
這樣兩個平台的行為都是確定的。

**改動三：強化斷言。** 現在只斷言 `ScannedCount > 0`，非常寬鬆——它只證明
「有掃到東西」，證不了「續掃狀態是正確的」。建議改為斷言
**`ScannedCount` 與輸出檔實際資料列數一致**，這才真正驗證 2.1.0 的耐久性契約。

> 這條屬於「加強測試」，不是「放寬測試」，符合 R5。若強化後在 Linux 上就失敗，
> 那代表找到真 bug，改走 TDD 流程修正式程式。

## 明確禁止的捷徑

修 Part 1 時以下作法一律不接受（`20-judgment-rubric.md` R5 / `60-development-guidelines.md`）：

- ❌ 對 Windows 無差別 `t.Skip()` — 讓契約消失，不是驗證。
- ❌ 放寬斷言（例如把 `>= 2` 改成 `>= 1`）換綠燈。
- ❌ 把測試移出覆蓋率統計或加進 `EXCLUDE_PATTERN`（需使用者核准）。
- ❌ 用「拉長 sleep」當作時間問題的解法 — 只是把賭注押大，測試仍不確定，且拖慢 CI。
  正解是等事件，不是睡更久。

## PR 切分與順序

刻意切成三個小 PR，因為**Windows CI 是我們唯一的 Windows 驗證管道**，
每個 PR 都要能獨立由 CI 判定成敗。

| PR | 內容 | 預期效果 | 風險 |
|---|---|---|---|
| **A** | A 組 + C 組（斜線、build tag 拆分） | 10 → 6 綠，純機械改動 | 低 |
| **B** | B 組（`.exe` helper） | 6 → 3 綠，且首次真正在 Windows 跑起 CLI | 低—中 |
| **C** | D 組（事件驅動等待 + 強化 cancel 斷言） | 3 → 0，Windows CI 全綠 | 中（含未證實的 127.0.0.2 假設） |

完成後另開一個 PR 把 **Windows job 設為 required check**，否則它會再次腐化
（`90-letter-to-future-sessions.md` 已警告過「Windows CI is unverified」這個坑）。

---

# Part 2 — 補覆蓋：Windows 真正的風險區域

**前提認知**：Part 1 全綠只代表「既有測試在 Windows 上跑得動」，不代表
「2.1.0 在 Windows 上正確」。下列缺口目前**零覆蓋**。

## 缺口 1 — 檔案 handle 是否確實釋放（優先度最高）

**風險**：Windows 不允許刪除或改名仍被開啟的檔案；Linux 允許。因此
「掃描結束後忘了關檔」這個 bug 在 Linux 上**永遠測不出來**，到 Windows 才爆。
2.1.0 新增的 append-reopen 路徑（`output_files.go:69`）正是最容易漏關的地方。

**測法**（跨平台同一份程式碼，Linux 上恆過、Windows 上有真正鑑別力）：

```
執行一次完整 Run（正常結束 / 被 cancel 各一個 case）
→ 結束後嘗試 os.Rename(outFile, outFile+".moved")
→ 必須成功
（resume 狀態檔同樣測一次）
```

`os.Rename` 成功即證明沒有殘留 handle。這是**用最低成本換取 Windows 專屬保護**
的典型手法，強烈建議納入。

## 缺口 2 — 同一程序內先寫後續接（append-reopen）

**風險**：Windows 的檔案共用模式（sharing mode）比 Linux 嚴格。若前一次的 handle
未釋放，`os.OpenFile` 可能直接失敗（sharing violation），而 Linux 完全不會。

**測法**：在同一個測試內連續跑兩次——第一次正常寫入並結束，第二次以
`-resume` append 模式開啟同一個輸出檔，斷言：開啟成功、header 未重複、
資料列數 = 第一次 + 第二次。

**注意**：CRLF header 比對已由 `output_files.go:111` 處理（已驗證），
**不需要**再為 CRLF 寫測試。

## 缺口 3 — Windows 專屬的路徑形狀

**風險**：現有路徑測試只用 `/tmp/...` 這種 Unix 形狀。Windows 真實輸入包含
磁碟機代號（`C:\out\scan.csv`）、含空白的路徑（`C:\Program Files\...`）、
UNC 路徑（`\\server\share\out`）。`filepath.Dir("C:\\out\\scan.csv")` 應得 `C:\out`。

**測法**：對 `resumePath` 與 `OutputPath` 各加一個 `//go:build windows` 的表格測試，
涵蓋上述三種形狀。

**已驗證的好消息**：正式程式碼沒有任何手工拼接路徑的地方，所以這組**預期會直接通過**——
它的價值是「防止未來有人寫出 `dir + "/" + name`」的迴歸護欄，而非現在就會抓到 bug。

## 缺口 4 — 中斷訊號語意（誠實的限制）

**風險**：Windows 沒有真正的 SIGINT。Go 在 Windows 上
`os.Process.Signal(os.Interrupt)` 是**不支援的**，真實 Ctrl+C 需要
`GenerateConsoleCtrlEvent` 這個 Win32 API。因此「從測試對子程序送 Ctrl+C」
在 Linux 可行、在 Windows 不可行。

**建議做法（不要硬造假象）**：
1. 耐久性契約用 **context 取消**驗證（平台中立，目前已是如此）——這覆蓋了
   「收到中斷後資料是否完整」這個真正重要的問題。
2. 訊號**傳遞**本身在 Windows 上的正確性，**明確記錄為未驗證**，寫進
   release notes 與 `50-lessons.md`，不要用一個假裝成功的測試蓋過去。
3. 若日後真的需要，再評估用 `GenerateConsoleCtrlEvent` 寫一個 Windows 專屬
   e2e——成本高，建議等有實際使用者回報再做。

> 這條刻意選擇「誠實留白」而非「湊一個測試」，符合 `60-development-guidelines.md`
> G5：不能驗證的事就明講，不要假裝。

## 缺口 5 — e2e 完全不在 Windows 上跑

**現況**：`e2e/run_e2e.sh` 是 Docker Compose，只在 Linux job 執行。Windows job 只有
`go build` + `go test`。也就是說 Windows 上**從來沒有跑過完整的掃描流程**。

**建議**：不強求把 Docker e2e 搬上 Windows（成本高、隔離性難保證）。
改為在 Windows job 加一個**輕量整合測試**：用本地 listener 起 mock 服務，
跑完整的 prep → scan → resume 三步驟，斷言輸出檔內容正確。這在 `go test` 內
即可完成，不需要 Docker，且是目前投資報酬率最高的 Windows 覆蓋補強。

## Part 2 優先順序建議

1. **缺口 1**（handle 釋放）— 成本最低、Windows 專屬鑑別力最高。
2. **缺口 5**（Windows 輕量整合測試）— 補上「完整流程從沒在 Windows 跑過」這個大洞。
3. **缺口 2**（append-reopen）— 直接保護 2.1.0 新功能。
4. **缺口 3**（路徑形狀）— 迴歸護欄，預期即刻通過。
5. **缺口 4**（訊號）— 記錄限制，暫不實作。

---

## 驗證策略（重要限制）

**我們沒有本機 Windows 環境；GitHub Actions 的 Windows job 是唯一的 Windows 神諭。**

因此：

- 每個 PR 都必須小到「CI 一次紅綠就能明確判定成敗」。
- 對於 D 組的計時問題與 127.0.0.2 假設，**不要在 Linux 上臆測結論**——
  推 PR、讀 Windows job 的輸出，用證據說話。
- Linux 端仍須維持 `make verify` exit 0（含 85% 覆蓋率門檻）。

## 治理合規檢查

- **TDD（憲法 III / G1）**：Part 1 屬純測試修正，「red first」已由現有 CI 紅燈提供
  證據；若 Part 2 的新測試抓到真 bug，修正式程式時必須重走完整 red → green 流程。
- **獨立審查（G2）**：兩部分皆需不同 provider／model 的跨模型審查（Codex 優先），
  且審查者須自行確認 Windows job 結果，不採信實作者的貼文。
- **驗證金字塔（G3）**：unit 由 `make verify` 覆蓋；e2e 觸發條件成立時
  （Part 2 缺口 2、5 動到 pipeline/writer）須跑 `make verify-e2e`；
  本次不涉及熱路徑效能改動，**perf 觸發條件不成立**，於報告中明確標示。
- **不得弱化 gate（R5）**：見上方「明確禁止的捷徑」。
