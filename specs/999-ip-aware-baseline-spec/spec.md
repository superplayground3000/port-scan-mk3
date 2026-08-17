# Feature Specification: IP-Aware Baseline Specification

**Feature Branch**: `[001-ip-aware-baseline-spec]`  
**Created**: 2026-03-02  
**Status**: Draft  
**Input**: User description: "establish baseline specification with design.md and 2026-03-02-ip-aware-full-spec-design.md and 2026-03-02-ip-aware-full-implementation.md"

## Clarifications

### Session 2026-03-02

> 本節僅保留決策脈絡；可執行規範以 Requirements 為準。

- Q: CIDR CSV 欄位名稱解析是否區分大小寫，且遇到重複欄位時如何處理？ → A: 區分大小寫；僅接受完全一致欄位名；同名欄位重複時啟動失敗。
- Q: Resume 狀態檔的讀寫路徑規則為何？ → A: 提供 `-resume` 時同一路徑讀寫；未提供時預設寫入 `<output-dir>/resume_state.json`。
- Q: 若輸出檔已存在，掃描啟動時應如何處理？ → A: 保留既有檔案，並為本次執行自動建立帶時間戳的新輸出檔。
- Q: 時間戳輸出檔名格式要用哪一種？ → A: 採 UTC `YYYYMMDDTHHMMSSZ` 格式（例如 `scan_results-20260302T013045Z.csv`）。
- Q: 若同一秒內重複啟動導致檔名衝突，應如何處理？ → A: 在同秒時間戳後附加遞增序號（例如 `-1`, `-2`）。

## Constitution Alignment Check

- **I. Library-First Design**: PASS（核心行為需求對應 `pkg/*` 可重用邏輯，CLI 僅為命令入口。）
- **II. CLI Contract-First**: PASS（需求保留 `validate|scan` 使用情境與輸入/輸出契約穩定性。）
- **III. Test-First Delivery (NON-NEGOTIABLE)**: PASS（每個 User Story 都定義獨立可驗證測試情境。）
- **IV. Integration Coverage for Contract Boundaries**: PASS（輸入解析、任務展開、writer/resume 契約均有可測 acceptance。）
- **V. Isolated End-to-End Verification**: PASS（規格要求正常與異常壓力情境的端到端驗證。）
- **VI. Observability by Default**: PASS（FR-016/FR-017 + SC-007/SC-008 明確規範事件欄位與進度摘要。）
- **VII. Versioning and Release Evidence**: PASS（本功能屬 CLI 契約變更，需附 release notes 與遷移資訊。）
- **Technology Stack Requirements**: PASS（限定 Go 1.24 與既有 TCP 掃描路徑，不引入衝突技術棧。）
- **Quality Gates**: PASS（成功標準與後續任務要求 `go test`、coverage、e2e 證據。）

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 依欄位名稱定義掃描目標 (Priority: P1)

作為掃描操作者，我可以指定 CIDR CSV 中代表目標 IP 與邊界 CIDR 的欄位名稱，即使 CSV 含有其他額外欄位，系統仍能正確抽取目標並執行前置驗證。

**Why this priority**: 若目標定義錯誤會直接造成掃描範圍偏差，屬於風險最高且會影響所有後續流程的核心能力。

**Independent Test**: 提供含自訂欄位名與多餘欄位的 CIDR CSV，指定對應欄位名稱後執行驗證；可獨立確認欄位映射、目標展開與範圍約束是否正確。

**Acceptance Scenarios**:

1. **Given** CIDR CSV 含 `source_ip` 與 `source_cidr` 欄位且使用者已指定對應欄位名, **When** 系統載入輸入資料, **Then** 系統僅以該兩欄建立掃描目標與分組。
2. **Given** 某列 `ip` 展開後有任一位址不在同列 `ip_cidr` 範圍內, **When** 系統執行啟動驗證, **Then** 系統必須在派發任何掃描任務前中止並回報違規列資訊。
3. **Given** 指定欄位名稱與 CSV 表頭僅大小寫不同或必要欄位不存在, **When** 系統執行啟動驗證, **Then** 系統必須立即中止並給出可定位問題的錯誤訊息。

---

### User Story 2 - 取得雙輸出結果檔 (Priority: P2)

作為掃描操作者，我希望一次掃描可同時得到完整結果檔與僅開放連接結果檔，以便後續報表與告警流程直接使用。

**Why this priority**: 輸出契約會直接影響下游分析與營運流程；若格式不一致或內容錯誤，會讓結果不可用。

**Independent Test**: 執行一次包含開放與關閉連接目標的掃描，檢查兩個輸出檔的存在性、欄位一致性與資料過濾正確性。

**Acceptance Scenarios**:

1. **Given** 掃描完成且同時有 open/close 結果, **When** 系統寫出結果檔, **Then** 主要輸出檔需包含所有結果，且 open-only 輸出檔只包含 `open` 狀態資料。
2. **Given** 輸出目錄內已存在舊結果檔, **When** 新一輪掃描啟動, **Then** 系統必須保留舊檔並建立帶時間戳的新主要輸出檔與對應 open-only 輸出檔。

---

### User Story 3 - 在中斷與壓力異常下可恢復 (Priority: P3)

作為掃描操作者，我需要系統在手動暫停、壓力過載或連續壓力服務失敗時維持可控行為，並可從中斷點恢復，避免重掃或漏掃。

**Why this priority**: 這是長時間批量掃描可營運化的關鍵保障，雖晚於輸入/輸出契約，但對穩定性與可追溯性非常重要。

**Independent Test**: 模擬手動暫停、壓力過載及連續壓力服務失敗情境，確認派發停止規則、終止規則與恢復結果一致性。

**Acceptance Scenarios**:

1. **Given** 系統處於掃描中且任一暫停來源觸發, **When** 系統接收暫停訊號, **Then** 新任務派發必須停止，而已在執行中的任務可自然完成。
2. **Given** 壓力服務連續發生失敗, **When** 失敗達到第 3 次連續事件, **Then** 系統必須終止本次掃描並產生可恢復的斷點狀態。
3. **Given** 使用者以斷點狀態重新啟動掃描, **When** 系統續跑, **Then** 結果必須與不中斷一次跑完的資料集合一致（無重複、無遺漏）。
4. **Given** 使用者以 `-resume <state-path>` 啟動或恢復掃描, **When** 系統需要保存新斷點, **Then** 系統必須回寫到同一 `<state-path>`；若未提供 `-resume`，則保存於 `<output-dir>/resume_state.json`。

---

### Edge Cases

- CIDR CSV 欄位名稱比對採區分大小寫；若出現完全同名重複欄位，系統必須直接中止且不得自行選擇欄位。
- 同一 `ip_cidr` 下多個 `ip` 選擇器展開後互相重疊時，系統需在啟動階段中止而非執行期才發現。
- 不同 `ip_cidr` 彼此重疊但列資料各自合法時，系統仍需視為全域衝突並拒絕啟動。
- 掃描結果全為非 open 狀態時，open-only 輸出檔仍需保留表頭且無資料列。
- 輸出目錄已有既有結果檔時，命名行為依 FR-012/FR-013（不得覆蓋既有檔）。
- 同秒檔名衝突時，序號分配行為依 FR-012/FR-013（主檔與 open-only 使用同序號）。
- 當輸入資料缺少業務欄位值（如 `fab_name` 或 `cidr_name`）時，輸出欄位仍需存在，值為空字串。
- 在暫停期間接收到中止事件時，系統需先完成在途任務，再保存可恢復狀態。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系統 MUST 允許使用者指定「目標 IP 欄位名稱」與「邊界 CIDR 欄位名稱」，且比對規則為區分大小寫的精確匹配；預設可對應 `ip` 與 `ip_cidr`。
- **FR-002**: 系統 MUST 能讀取含任意附加欄位的 CIDR CSV，並僅依使用者指定的兩個欄位建立掃描輸入模型。
- **FR-003**: 系統 MUST 支援 `ip` 欄位中的單一 IPv4 與 IPv4 CIDR 表示法，並展開為可掃描目標集合。
- **FR-004**: 系統 MUST 在啟動階段驗證每個展開目標位址皆落在同列 `ip_cidr` 範圍內；任何違反都必須立即中止流程。
- **FR-005**: 系統 MUST 在啟動階段驗證並拒絕以下情況：必要欄位不存在、必要欄位僅大小寫不符、必要欄位名稱重複、欄位值格式錯誤、檔案不可讀。
- **FR-006**: 系統 MUST 在啟動階段拒絕以下衝突：重複 `(ip, ip_cidr)` 組合、不同 `ip_cidr` 互相重疊、同一 `ip_cidr` 內不同 `ip` 展開集合重疊。
- **FR-007**: 系統 MUST 只掃描由 `ip` 欄位展開出的唯一位址集合，不可將整個 `ip_cidr` 視為掃描目標全集。
- **FR-008**: 系統 MUST 以 `ip_cidr` 作為進度分組邊界，為每個分組維護可恢復的線性進度位置。
- **FR-009**: 系統 MUST 支援自動壓力控制與手動暫停雙來源；任一來源要求暫停時停止新任務派發，兩者皆解除時才恢復派發。
- **FR-010**: 系統 MUST 在壓力服務連續失敗第 1 與第 2 次時保留上次已知狀態並記錄失敗；連續第 3 次失敗時終止本次流程。
- **FR-011**: 系統 MUST 產出主要結果檔，且欄位順序必須固定為：`ip`,`ip_cidr`,`port`,`status`,`response_time_ms`,`fab_name`,`cidr_name`。其中 `fab_name`、`cidr_name` 若來源缺值，必須輸出為空字串，不得省略欄位。
- **FR-012**: 系統 MUST 在每次執行時建立唯一時間戳命名的主要結果檔，格式為 `scan_results-YYYYMMDDTHHMMSSZ.csv`（UTC）；若同秒衝突，必須改為 `scan_results-YYYYMMDDTHHMMSSZ-<n>.csv`（`n` 為遞增正整數），且不得覆蓋既有結果檔。
- **FR-013**: 系統 MUST 為每次執行建立與主要結果檔同一時間戳批次的 open-only 輸出檔，格式為 `opened_results-YYYYMMDDTHHMMSSZ.csv`（UTC）；若同秒衝突，必須改為 `opened_results-YYYYMMDDTHHMMSSZ-<n>.csv` 並與主要結果檔使用相同序號；該檔欄位結構與主要結果檔一致，且僅包含 `open` 記錄。
- **FR-014**: 系統 MUST 在中止或異常終止時保存可恢復狀態；若提供 `-resume` 則使用該路徑做讀取與後續寫入，若未提供則使用 `<output-dir>/resume_state.json`。
- **FR-015**: 系統 MUST 在續跑時保證任務結果不重複且不遺漏。
- **FR-016**: 系統 MUST 提供結構化執行事件，且每筆事件至少包含：`target`、`port`、`state_transition`、`error_cause`（若無錯誤則為空值或明確 `none`）。
- **FR-017**: 系統 MUST 在長時間掃描期間定期輸出 progress 事件（至少含已掃描數、總數、完成率），並在結束時輸出 completion summary（至少含總任務數、open/close 統計、耗時）。
- **FR-018**: 基本輸入列的 `port` MUST 覆寫該列的 port file。空白 `port` MUST 使用 port file 的全部 ports。
- **FR-019**: 系統 MUST 對基本輸入的唯一 `ip:port/tcp` 工作去重。Bucket 產生與 resume rebuild MUST 使用同一結果與計數。
- **FR-020**: 新 snapshot MUST 記錄 target-semantics version。若基本輸入含 row port，未記錄 version 的 snapshot MUST 在網路活動前被拒絕。

### Non-Functional Requirements

- **NFR-001**: 所有輸入驗證失敗必須在任何掃描任務派發前被攔截並終止流程。
- **NFR-002**: 在相同輸入與參數下，主要輸出與 open-only 輸出的欄位順序與命名結果必須完全符合 FR-011/FR-012/FR-013；對應契約測試偏差率必須為 0%。
- **NFR-003**: 系統在中斷後續跑時，最終結果相對不中斷基準結果的重複率與遺漏率都必須為 0%。
- **NFR-004**: 執行期間事件記錄必須保持結構化，且長時間掃描必須持續提供 progress 與 completion summary。
- **NFR-005**: 交付前必須提供單元、整合、e2e 與覆蓋率 gate 的可追溯驗證證據。

### Assumptions

- 本功能針對 IPv4 目標定義與掃描流程，不擴展到 IPv6。
- Port file 維持 `port/tcp` 格式。基本列的 `port` 值可覆寫該列的 port file。
- 既有輸出中已被下游依賴的業務欄位會持續保留，若來源缺值則以既有行為處理。
- 斷點狀態檔在同一批次輸入前提下可直接用於續跑，不要求跨不同輸入資料混用。

### Dependencies

- 需要可存取的壓力服務端點以支援自動壓力控制情境。
- 需要可寫入的輸出目錄以建立主要結果檔、open-only 輸出檔與恢復狀態檔。
- 需要可重現的測試環境以驗證正常、服務錯誤與連線失敗三類壓力情境。

### Key Entities *(include if feature involves data)*

- **CIDR Input Row**: 使用者提供的一列輸入，核心屬性為 `ip` 與 `ip_cidr`，可含其他業務欄位。
- **Target Group**: 以 `ip_cidr` 聚合的掃描分組，包含唯一目標位址集合、port 集合與可恢復進度。
- **Scan Task**: 單一目標位址與單一 port 的掃描單位，為執行與結果產出的最小粒度。
- **Scan Result Record**: 每個掃描任務對應的一筆結果，至少含目標位址、邊界分組、port、狀態與回應時間。
- **Resume State Entry**: 每個 Target Group 的續跑狀態，記錄下一個待派發位置與群組完成狀態。
- **Pressure Status**: 自動壓力控制輸入的當前狀態與連續失敗計數，用於決定暫停或終止。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 在涵蓋自訂欄位名與附加欄位的驗證資料集中，100% 測試案例可正確解析出目標欄位並進入掃描準備階段。
- **SC-002**: 所有不合法輸入規則（欄位缺失、格式錯誤、範圍衝突、重複/重疊）皆在任務派發前被攔截，攔截率達 100%。
- **SC-003**: 任一測試批次中，實際掃描任務數必須等於唯一 `ip:port/tcp` 工作數，誤差為 0。
- **SC-004**: 每次成功掃描後，open-only 輸出檔的資料筆數需與同批次主要結果檔中 `open` 狀態筆數完全一致（100% 一致）。
- **SC-005**: 在中斷後續跑測試中，續跑最終結果與不中斷基準結果相比，重複率為 0%、遺漏率為 0%。
- **SC-006**: 在壓力服務連續失敗情境測試中，系統於第 3 次連續失敗終止的符合率為 100%，且每次均可產出可用續跑狀態。
- **SC-007**: 在觀測性驗證測試中，100% 抽樣事件都包含 `target`、`port`、`state_transition`、`error_cause` 四個欄位。
- **SC-008**: 在長時間掃描情境中，必須至少出現 1 筆 progress 事件與 1 筆 completion summary 事件。
