# 2026-03-09 Draw.io CLI Export Skill Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. Objective

建立一個放在 `~/.codex/skills/` 的全域個人 skill，讓未來任何 session 在需要把 `.drawio` 檔匯出成圖片或 PDF 時，都能用一致、可驗證、可排查的流程完成工作，而不是每次重新摸索 CLI 參數。

## 2. Problem

目前已知本機 draw.io CLI 路徑為：

- `/Applications/draw.io.app/Contents/MacOS/draw.io`

雖然 `--help` 能列出旗標，但直接看 help 有幾個問題：

- 不知道哪些旗標在本機實際可用
- 不知道單頁匯出、全頁 PDF、透明背景、資料夾批次等情境該怎麼選參數
- 不知道成功時 CLI 的輸出長什麼樣子
- 發生空輸出、頁碼錯誤、輸出檔名不符時沒有固定排查步驟

## 3. Design Goals

- 全域可重用：skill 不綁定單一 repo
- doc-first：以流程與判斷規則為主，不依賴 wrapper script
- scenario-oriented：涵蓋高頻匯出情境模板
- evidence-based：只寫入已在本機驗證過的 CLI 行為
- compact：`SKILL.md` 保持精簡，把長參考資料拆到附屬檔案

## 4. Non-Goals

- 不建立 draw.io wrapper script
- 不處理 draw.io 圖面內容設計與排版
- 不嘗試支援未在本機驗證的自動化轉檔 pipeline
- 不修改任何專案內 `.drawio` 檔內容

## 5. Options Considered

### Option A: Doc-first skill + scenario templates

以 skill 文件提供檢查流程、決策表、常用命令模板、驗證與排查。

優點：
- 可攜性最高
- 容易維護
- 最符合 skill 的用途

缺點：
- 使用者仍需手動執行命令

### Option B: Skill + helper script

優點：
- 上手最快

缺點：
- 增加路徑與維護成本
- script 若壞掉，skill 反而失去泛用性

### Option C: Cheat sheet only

優點：
- 撰寫最快

缺點：
- 缺少判斷規則與排錯流程
- 遇到非模板情境時幫助有限

## 6. Chosen Approach

採用 Option A，並在 skill 內加上高頻 scenario templates。

原因：
- 能同時保留通用判斷流程與直接可用的命令範例
- 不把重複利用建立在額外腳本上
- 最容易隨 draw.io CLI 實測結果持續修正

## 7. Deliverables

### 7.1 Global skill directory

- `~/.codex/skills/drawio-cli-export/SKILL.md`
- `~/.codex/skills/drawio-cli-export/cli-reference.md`

### 7.2 Repo documentation

- 本設計文件
- 對應 implementation plan

## 8. Skill Structure

### 8.1 `SKILL.md`

內容只放：
- 何時該用這個 skill
- 4 步標準流程
- 常用情境模板
- 常見失敗與排查
- 參考檔連結

### 8.2 `cli-reference.md`

內容放：
- 已驗證的 CLI 路徑
- 已驗證可用旗標
- 實測命令與觀察結果
- 參數選擇備忘

## 9. Standard Workflow

skill 的核心工作流固定為：

1. Confirm binary path
2. Confirm input path and page intent
3. Choose export mode by outcome
4. Verify output file exists and matches expectation

必須明確提醒：

- `--page-index` 是 1-based
- 若 `-o` 給的是完整檔名，格式會由副檔名決定
- 若未提供 `-o`，CLI 會依輸入檔名推導輸出檔名

## 10. High-Frequency Scenarios

skill 至少覆蓋：

- 單頁匯出 PNG
- 單頁匯出 SVG
- 多頁匯出 PDF
- 透明背景 PNG
- 指定尺寸或比例縮放
- 以資料夾為輸入做批次匯出

## 11. Validation Strategy

依 `writing-skills` 的 TDD 精神，本次 skill 內容只納入已實測過的行為：

- `--help` 旗標列表
- `-x -f png -p 1`
- `-x -f svg -p 2`
- `-x -f pdf -a`
- 後續補測透明 PNG、預設輸出檔名、遞迴資料夾匯出

## 12. Success Criteria

- skill 目錄可被未來 session 直接搜尋與觸發
- `SKILL.md` 足夠短，能快速掃描
- 參考檔完整記錄已驗證命令
- 依 skill 指引可成功完成至少 3 種不同匯出情境

## 13. Risks and Mitigations

- 風險：CLI 行為隨 draw.io 版本變動
  - 對策：參考檔明記本機 binary path 與實測結果，必要時重驗
- 風險：skill 寫成純命令表，缺少決策能力
  - 對策：主文件以流程與判斷規則為中心
- 風險：範例過多導致 skill 不易掃描
  - 對策：詳細例子移到 `cli-reference.md`
