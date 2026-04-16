# Pre-process Workflow Spec

## Background

pre-processing has follow input files:

- filtered targets CSV: lists with lots of columns, contains dst ip, dst port, dst ip cidr
- previous scanned result CSV: with opened targets, columns are host,port
- cleaned CIDRs CSV: a list of CIDRs with status column indicates the CIDR is closed or still open, status: open/close


掃描分成兩種模式:

- 從頭開始: 拿 filtered targets CSV + cleaned CIDRs CSV 產出 port-scan 用的 input CSV
  - 每個data center會有一個自己的 filtered targets CSV
  - cleaned CIDRs CSV包含全部 datacenter資訊
  - 產出的 input必須要有固定的存放邏輯，以方便 port-scan tool 根據規則存取
- 覆掃: 拿上次掃出來的 opened targets重新做一次掃描
  - 每個data center會有自己上次掃描的 opened targets
  - opened targets 也要用 cleaned CIDRs CSV filter 過後產出 給 port-scan 用的 input CSV