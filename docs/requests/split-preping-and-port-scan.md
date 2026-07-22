# Split Preping And Port Scan

在大量資料下，preping 以及後續轉換 ping 結果與 input 成 Per CIDR bucket 這兩個步驟會花費大量時間，並且中間漫長的silence會讓人無法知道目前的狀態，這個需求的目標是把preping，轉換結果成可以被resume使用的bucket CIDR 存檔，port scan拆成三個獨立的步驟。

Requirements:
- 既有的輸入/輸出格式不變
- preping的uncreachable.csv輸出可以直接搭配rich csv交給Per CIDR bucket generator
- Per CIDR bucket generator 可以根據輸入(rich.csv 以及 unreachable.csv)產出能直接給port scan --resume使用的 Per CIDR bucket file
- Per CIDR bucket generator 轉換過程中必須能展示出目前進度(百分比)
- Per CIDR bucket generator 如果可以，必須能用平行處理加速
- preping 必須能展示出目前進度(百分比)
- 拆開後，原本屬於該流程的flags都必須保留，並且新增必要的flags
- 任何可調整參數都不能hard coded，尤其是timeout，worker數量等關鍵參數
