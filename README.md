# slot_Combo_Multiplier_GO

- Slot simulator demo

- 模擬遊戲數據，輸出各項統計資料

- 主要特色：
  - 主遊戲依每轉消除次數，當次消除贏分倍率依序x1 x2 x3 x4，後續皆x4
  - 免費遊戲依消除次數跨轉累計，當次消除贏分倍率依序x1 x2 x4 x6…至x50封頂
  - 含多個輪帶替換機制

- 遊戲規則與程式流程詳見程式檔頭註解

## 環境需求
- Windows 10/11
- Go 1.24

## 開發工具(建議)
- Visual Studio Code
  - 擴充：Go (golang.go)

## Build
- go build -o slot_Combo_Multiplier.exe .

## Run (example)
- .\slot_Combo_Multiplier.exe 或 go run .\slot_Combo_Multiplier.go

## 附註
- 本專案中之.go檔無外部依賴，迅速試跑可直接貼進VScode/VS或其他編譯軟體測試

## 模擬器輸出示意圖 完整輸出請見.txt附檔
<p align="center">
  <img src="image/slot_Combo_Multiplier_Sim.png" width="700" alt="模擬輸出截圖">
  <br><sub>RTP、獎項分佈等主要遊戲表現</sub>
</p>
