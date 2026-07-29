# Speed Up Scan Preparation

當bucket list 變大時，scan會花大量時間做scan前置準備，這次遇到的問題是大約130個chunks, 每個chunks 是一個/24 or /22 CIDR，ports為多數為1個，而unreachable_ipv4_u32有大約42587個，這個大小的bucket JSON花了6個小時還沒有成功parse完開始掃描

## Requirements
- 找出parse 慢的bottleneck，根據bottleneck提出加速方案
- 用各種不同resume file結構驗證正確性與速度
- 掃描過程中要可以偵被TRL+C暫停，暫停必須
  - 把當前狀態寫成json存檔，並且可以在下一次scan執行時搭配--resume從斷點開始
  - 確保尚未寫入檔案的result全數寫入output 檔案
- resume時，要能夠接續上次的result output檔案繼續寫入
- 以log顯示進行到哪一步驟(例如開始CIDR bucket parse, parse到第幾個bucket, CIDR bucket parse完畢, 開始scan)
  - 原本的scan進度顯示必須保留

## Suggested Approach
- 只parse當前CIDR並且Generate Targets
- 計算進度以bucket為單位，不把filter與CIDR乘開