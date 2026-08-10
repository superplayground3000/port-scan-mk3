# Scanner DialContext + Local Ephemeral Port Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 背景

目前 `pkg/scanner/ScanTCP` 依賴 `net.DialTimeout` 風格的注入函式（`dial(network, address, timeout)`），而 `pkg/scanapp` 預設也是使用 `net.DialTimeout`。新需求是改為使用 `net.Dialer` + `DialContext`，並且：

- timeout 由 dial context 控制
- local address 的 port 固定設定為 `0`，讓 OS 自動挑選 ephemeral source port
- `context deadline exceeded` 必須歸類為 timeout（`close(timeout)`）
- 保留可注入 dial 能力（便於測試與客製）

## 目標

- 將 TCP 掃描連線路徑從 `DialTimeout` 遷移到 `DialContext`
- 明確使用 `LocalAddr: &net.TCPAddr{Port: 0}`
- 不改變既有輸出格式與核心狀態分類（`open` / `close` / `close(timeout)`）
- 維持測試可注入能力，避免回歸與測試脆弱化

## 非目標

- 不變更 CSV 欄位、輸出批次檔命名、掃描排程/限速邏輯
- 不新增 CLI 旗標
- 不重構 `scanapp` 的 worker 模型

## 設計總覽

### 1. 介面調整

- `pkg/scanner/scanner.go`
  - `ScanTCP` dial 參數改為 `DialContext` 風格：
    - 舊：`func(string, string, time.Duration) (net.Conn, error)`
    - 新：`func(context.Context, string, string) (net.Conn, error)`
  - `ScanTCP` 內部建立 `context.WithTimeout` 後呼叫 dial。

- `pkg/scanapp/scan.go`
  - `RunOptions.Dial`（與其 type alias）同步改為 `DialContext` 風格簽名。
  - 若未注入 dial，預設使用：
    - `net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}`
    - 實際呼叫 `dialer.DialContext`。

### 2. 資料流

1. worker 收到 `scanTask`。
2. 呼叫 `scanner.ScanTCP(dial, ip, port, cfg.Timeout)`。
3. `ScanTCP` 內部建立 timeout context。
4. 透過注入的 dial（預設為 dialer 的 `DialContext`）連線。
5. 成功即 `open`；失敗進入錯誤分類。

### 3. 錯誤分類規則

- `open`：連線成功。
- `close(timeout)`：
  - `err` 為 `net.Error` 且 `Timeout() == true`，或
  - `errors.Is(err, context.DeadlineExceeded)`（含 `context deadline exceeded`）。
- `close`：其他錯誤（例如 connection refused）。

## 相容性與風險

### 相容性

- 掃描結果 `Status` 欄位字串不變。
- `RunOptions.Dial` 與 scanner 測試 mock 的函式簽名會更新，但行為語意相同。

### 風險

- 風險：遷移簽名時遺漏呼叫點，導致編譯錯誤。
  - 緩解：以 `rg "ScanTCP\(|RunOptions\.Dial|DialFunc"` 全域檢查。
- 風險：timeout 判定只靠 `net.Error.Timeout()` 可能漏掉 `context.DeadlineExceeded`。
  - 緩解：加入 `errors.Is(err, context.DeadlineExceeded)` 分支與測試。

## 測試策略

- `pkg/scanner/scanner_test.go`
  - open path（真實 loopback listener）改用 `net.Dialer{}.DialContext`。
- `pkg/scanner/scanner_extra_test.go`
  - mock dial 改新簽名。
  - 保留 timeout / refused 測試。
  - 新增（或調整）`context.DeadlineExceeded` -> `close(timeout)` 測試。

- `pkg/scanapp` 相關測試
  - 若有注入 `RunOptions.Dial` 的測試，同步簽名；目前影響可控。

- 驗證命令
  - `go test ./pkg/scanner ./pkg/scanapp`
  - `go test ./...`

## 驗收條件

- 程式預設 dial 路徑改為 `net.Dialer` + `DialContext`。
- dial context timeout 生效。
- local source port 設為 `0`（由 OS 選 ephemeral port）。
- `context deadline exceeded` 被歸類為 `close(timeout)`。
- 既有測試與新增測試通過。
