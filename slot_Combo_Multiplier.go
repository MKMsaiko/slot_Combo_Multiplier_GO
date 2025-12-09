/*
遊戲規則：
Cascading Way Game : 1024 ways  window : 4*5
遊戲盤面由左到右，同符號連續出現3、4、5次，即有贏分，符號消除後會補上新符；直到無贏分
贏分計算為：押注 x 符號賠率 x 符號在各軸個數
主遊戲依每轉消除次數，當次消除贏分倍率依序x1 x2 x3 x4，後續皆x4
免費遊戲依消除次數跨轉累計，當次消除贏分倍率依序x1 x2 x4 x6…至x50封頂
主遊戲中出現3個任意位置的Scatter觸發免費遊戲x8，每多1個再+2
免費遊戲中出現3個任意位置的Scatter再觸發免費遊戲x8，每多1個再+2
免費遊戲上限為50轉，Scatter出現在主遊戲1-5轉輪，免費遊戲2-4轉輪
Wild皆出現在2-5轉輪，可替代除Scatter外之任意符號
賠率表以1024 ways總押注1作計算

程式流程：
依處理器thread數 → 分數個worker → 各自做以下1-7 → 彙整輸出

1. 主遊戲初轉（MG）

	呼叫 w.spinInit(rng, reelsMGSpin)
	對 5 軸各抽一個停點，填滿 4×5 視窗，清空 Hybrid 補帶狀態。

2. 主遊戲 ways 計算 + 連消（含 AftX_MG）

	進入 MG 迴圈：
		a. 呼叫 evalWays(w) 算本步 ways 派彩，無贏分就跳出 MG。
		b. 第一次有贏分的盤面：bumpLenCats 記到 mgInitLenCount。
		c. 每一步（初轉＋每次消除後）都用 bumpLenCats 記到 mgLenCount。
		d. 將本步贏分乘上目前 MG 倍率（1→2→3→4 封頂）加到 mgWin，combo++。
		e. 若尚未切 AftX，且 combo ≥ AftComboX_MG，則以機率 pAftX_MG 把補帶換成 reelsMGRefillAftX，並 resetHybridRefillState。
		f. 依目前選到的補帶（普通 / AftX）呼叫 applyCascadesHybrid 做「消除+掉落+補符」，MG 倍率 +1（最多 4）。
	MG 迴圈結束後，記錄這把 MG combo 次數進 mgComboHist。

3. 判斷是否觸發 FG

	呼叫 countScatterAll(w) 數整個 4×5 視窗的 Scatter（MG 結束後盤面）。
		s < 3：沒有 FG。若 MG 也沒贏分，deadSpins++。
		s ≥ 3：觸發 FG：
			a. 由 startSpinsByScatter(s) 算出初始場次 fgStart（8 起跳，上限 50）。
			b. triggerCount++，fgStartDist[fgStart]++。

4. 一整段 FG（含 AftX_FG + END 機制）

	呼叫 playFG(...)，傳入：FG 轉輪、補帶、起始場次 fgStart，以及各種 FG 統計陣列。
	playFG 內部流程（「這一整段 FG」）：
		a. queue = initSpins（目前排隊要跑的 FG 轉數），mult = 1（跨轉累計倍率 1,2,4,6…50），segPeak = 1。
		b. 當 queue > 0 時，依序跑每一轉： (queue--，spins++，turnIndex = 這段的第幾轉)
			初始輪帶：
				若 mult ≥ aftMultX_FG_EndSpin，則以 pEndSpin 機率改用 reelsFG_END_Spin，並計數 usedEndSpin。
				否則用普通 FG spin 帶 reelsFGSpin。
				→ 呼叫 w.spinInit(選到的 spin 帶)。
				這一轉內再進入「連消」迴圈：
					呼叫 evalWays(w) 算贏分，沒贏分就結束這一轉。
					第一次有贏分：記到 fgInitLenCount；每一步都記到 fgLenCount。
					把本步贏分乘上目前跨轉倍率 mult，加到 res.total；turnCombo++。
					判斷該步補帶」：
						若 mult ≥ aftMultX_FG_EndRefill：
							優先以 pEndRefill 切入 END_Refill（reelsFG_END_Refill，整轉補符鎖在 END 模式）並記錄第一次切入的 turnIndex；
						若沒有切 END 且目前仍是 Normal：
							以 pAftX_FG_AftMultX 切到 AftX 補帶 reelsFGRefillAftX。
						若 mult 還沒到門檻：
							沿用原先的 combo 版 AftX邏輯（turnCombo ≥ AftComboX_FG 時，以 pAftX_FG 切到 AftX）。
						每次模式切換都 resetHybridRefillState。
						用目前選到的補帶（Normal / AftX / END）呼叫 applyCascadesHybrid，做消除+補符。
						FG 跨轉倍率更新：
							呼叫 nextFGMult(mult)，依序 1→2→4→6→…→50 封頂，同時更新這段的 segPeak。

				這轉連消結束後，數盤面 Scatter：
					s ≥ 3 則算要再加幾轉 add = retriggerByScatter(s)，
					再依「本段 FG 已跑+排隊中」總數，配合 maxFGTotalSpins=50 做截斷；add > 0 則 queue += add，retri++，
					並把 add 依 8,10,12.. 併入 retriggerDist。
					把這轉 turnCombo 併到 fgComboHist（>20 併入 20），更新整段最長 FG combo。

5. 整段 FG 派彩加權、累計：

	以 segPeak 更新 peakMultHist、peakMultAvg、peakMultMax。
	把該段實際總場次 spins 併到 fgSegLenHist（1..50）。
	END 使用次數與第一次 END_Refill 切入轉數回傳給 worker。
	worker 把本段 FG 的贏分（res.total）、spins、retri、END 統計等，累加到自己的 local Stats。

6. 單把結果累計與分層

	該把總贏分 spinTotal = mgWin + fgWin。
	依 spinTotal/bet ，更新 Big/Mega/Super/Holy/Jumbo/Jojo 分層與 maxSingleSpin。

7. 進度心跳

	每轉 bumpCnt++，4096 轉做一次 atomic.AddInt64(&spinsDone, 4096)。
*/
package main

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

/**************
 * 參數
 **************/
var (
	numSpins int64   = 100000000
	bet      float64 = 1.0
	workers          = runtime.NumCPU()

	// AftX
	AftComboX_MG = 3
	pAftX_MG     = 0.35

	AftComboX_FG = 2
	pAftX_FG     = 0.4

	// FG END 機制
	// END_Refill 門檻與機率
	aftMultX_FG_EndRefill = 8   // 當段內累計倍率 >= 此值後，開始對補符做 END_Refill / AftX_FG_AftMultX 判定
	pEndRefill            = 0.5 // 在高倍率階段，每次補符先以此機率切入 reelsFG_END_Refill（該轉剩餘步驟沿用）
	pAftX_FG_AftMultX     = 0.5 // 高倍率階段下，若未切入 END_Refill，改用此機率嘗試切入 AftX 補帶

	// END_Spin 門檻與機率
	aftMultX_FG_EndSpin = 10 // 當段內累計倍率 >= 此值後，每轉開頭以 pEndSpin 機率用 reelsFG_END_Spin 做初轉輪帶
	pEndSpin            = 0.45
)

/**************
 * 常數
 **************/
const (
	reelsCount = 5
	rows       = 4
	numWays    = 1024
	minPayLen  = 3
	maxLen     = 5

	maxFGTotalSpins = 50
)

/**************
 * 符號
 **************/
const (
	S9 uint8 = iota
	S10
	SJ
	SQ
	SK
	SB
	SF
	SR
	SW // Wild
	SS // Scatter
	NumSymbols
)

/**************
 * 賠率表（3/4/5 連）
 **************/
var pay = func() (p [NumSymbols][3]float64) {
	p[S9] = [3]float64{0.04, 0.10, 0.20}
	p[S10] = [3]float64{0.04, 0.15, 0.30}
	p[SJ] = [3]float64{0.06, 0.20, 0.40}
	p[SQ] = [3]float64{0.06, 0.25, 0.50}
	p[SK] = [3]float64{0.10, 0.40, 1.00}
	p[SB] = [3]float64{0.15, 0.60, 1.20}
	p[SF] = [3]float64{0.20, 1.00, 1.80}
	p[SR] = [3]float64{0.30, 1.20, 2.50}
	return
}()

/**************
 * 輪帶
 **************/
var reelsMGSpinStr = [][]string{
	// Reel 1
	{"9", "9", "Q", "Q", "K", "K", "K", "R", "R", "S", "9", "9", "J", "J", "K", "K", "F", "F", "J", "J", "9", "9", "9", "S", "F", "F", "J", "J", "9", "9", "Q", "Q", "J", "J", "R", "R", "10", "10", "F", "F", "J", "J", "K", "K", "9", "9", "R", "R", "Q", "Q", "B", "B", "K", "K", "B", "B", "9", "9", "K", "K", "9", "9", "J", "J", "J", "F", "F", "K", "K", "Q", "Q", "F", "F", "10", "10", "K", "K", "J", "J", "F", "F", "Q", "Q", "F", "F", "J", "J", "J", "9", "9", "K", "K", "R", "S", "Q", "Q", "F", "F", "10", "10", "F", "F", "9", "9", "K", "K", "S", "9", "9", "10", "10", "K", "K", "K", "10", "10", "9", "9", "Q", "Q", "9", "9", "K", "K", "Q", "Q", "J", "J", "9", "9", "9", "K", "K", "S", "10", "10", "J", "J", "10", "10", "S", "J", "J", "B", "B", "F", "F", "J", "J", "S", "K", "K", "10", "10", "J", "J", "9", "9", "B", "B", "F", "F"},
	// Reel 2
	{"R", "R", "Q", "Q", "B", "B", "S", "Q", "Q", "R", "R", "J", "J", "B", "B", "K", "K", "W", "10", "10", "J", "J", "B", "B", "9", "9", "R", "R", "F", "F", "J", "J", "10", "10", "S", "J", "J", "Q", "Q", "Q", "K", "K", "10", "10", "R", "R", "K", "K", "Q", "Q", "10", "10", "Q", "Q", "10", "10", "J", "J", "B", "B", "K", "K", "K", "R", "R", "J", "J", "9", "9", "F", "F", "10", "10", "B", "B", "10", "10", "K", "K", "S", "R", "R", "B", "B", "W", "9", "9", "F", "F", "S", "Q", "Q", "B", "B", "10", "10", "Q", "Q", "10", "10", "R", "R", "B", "B", "10", "10", "S", "9", "9", "R", "R", "B", "B", "9", "9", "S", "R", "R", "Q", "Q", "J", "J", "9", "9", "S", "R", "R", "B", "B", "J", "J", "Q", "Q", "10", "10", "Q", "Q", "B", "B", "Q", "Q", "10", "10", "B", "B", "10", "10", "Q", "Q", "Q", "B", "B", "Q", "Q", "10", "10", "B", "B", "R", "R", "9", "9"},
	// Reel 3
	{"Q", "Q", "J", "J", "S", "9", "9", "K", "K", "10", "10", "B", "B", "S", "9", "9", "R", "R", "10", "10", "9", "9", "K", "K", "S", "9", "9", "Q", "Q", "J", "J", "10", "10", "F", "F", "9", "9", "J", "J", "10", "10", "B", "B", "B", "9", "9", "10", "10", "J", "J", "K", "K", "F", "F", "R", "R", "10", "10", "J", "J", "F", "F", "Q", "Q", "10", "10", "B", "B", "9", "9", "Q", "Q", "K", "K", "10", "10", "B", "B", "F", "F", "B", "B", "R", "R", "Q", "Q", "R", "R", "K", "K", "Q", "Q", "9", "9", "10", "10", "K", "K", "9", "9", "K", "K", "Q", "Q", "9", "9", "10", "10", "F", "F", "J", "J", "W", "10", "10", "Q", "Q", "10", "10", "J", "J", "B", "B", "K", "K", "10", "10", "Q", "Q", "Q", "B", "B", "9", "9", "10", "10", "J", "J", "F", "F", "9", "9", "K", "K", "W", "9", "9", "Q", "Q", "J", "J", "J", "S", "9", "9", "B", "B", "J", "J", "S", "9", "9"},
	// Reel 4
	{"9", "10", "Q", "K", "S", "J", "B", "R", "F", "9", "J", "W", "10", "B", "Q", "F", "J", "R", "W", "9", "10", "Q", "R", "9", "J", "B", "K", "10", "F", "R", "9", "K", "J", "F", "9", "Q", "B", "F", "10", "J", "9", "B", "J", "10", "Q", "9", "J", "B", "F", "K", "9", "B", "10", "Q", "K", "R", "F", "J", "Q", "9", "B", "10", "Q", "B", "J", "10", "9", "K", "B", "Q", "J", "K", "F", "9", "J", "F", "10", "9", "J", "R", "Q", "B", "F", "10", "9", "J", "B", "K", "Q", "R", "F", "J", "10", "9", "J", "F", "10", "R", "Q", "B", "10", "R", "F", "B", "10", "F", "B", "9", "K", "F", "9", "B", "10", "9", "R", "K", "10", "J", "R", "9", "Q", "9", "B", "R", "K", "9", "J", "B", "Q", "10", "9", "K", "J", "R", "10", "K", "S", "Q", "R", "B", "K", "J", "Q", "9", "10", "J", "B", "R", "K", "Q", "F", "10", "B", "9", "Q", "J", "10", "K", "Q", "R", "J", "B"},
	// Reel 5
	{"Q", "F", "S", "K", "B", "R", "10", "J", "B", "R", "W", "9", "Q", "R", "J", "B", "Q", "K", "10", "F", "J", "B", "10", "F", "Q", "9", "10", "J", "R", "9", "B", "F", "10", "B", "Q", "J", "R", "10", "B", "J", "Q", "10", "9", "R", "Q", "10", "9", "J", "F", "B", "R", "Q", "9", "J", "S", "10", "J", "Q", "B", "9", "10", "F", "J", "9", "10", "Q", "J", "9", "B", "Q", "10", "J", "R", "Q", "10", "9", "J", "F", "B", "Q", "K", "10", "J", "Q", "9", "10", "B", "J", "F", "9", "B", "Q", "10", "R", "9", "B", "J", "Q", "10", "R", "F", "J", "9", "B", "K", "10", "Q", "9", "J", "10", "F", "9", "Q", "K", "J", "F", "Q", "W", "10", "9", "K", "F", "K", "10", "K", "9", "Q", "R", "K", "J", "10", "R", "Q", "9", "K", "F", "B", "Q", "K", "9", "J", "F", "K", "J", "9", "R", "K", "10", "J", "9", "K", "Q", "J", "9", "10", "B", "Q", "9", "10", "J", "K", "9"},
}

var reelsFGSpinStr = [][]string{
	// Reel 1
	{"10", "9", "Q", "F", "K", "B", "9", "J", "R", "F", "K", "Q", "R", "9", "10", "J", "9", "B", "J", "Q", "10", "K", "J", "9", "Q", "F", "J", "10", "R", "B", "9", "10", "Q", "F", "J", "10", "9", "F", "R", "B", "9", "J", "Q", "9", "10", "J", "K", "Q", "R", "J", "9", "10", "R", "F", "J", "B", "10", "F", "R", "B", "9", "10", "K", "R", "Q", "B", "10", "9", "J", "K", "10", "Q", "9", "J", "B", "10", "9", "J", "B", "K", "Q", "F", "J", "K", "9", "Q", "J", "K", "10", "Q", "J", "R", "K", "9", "Q", "10", "F", "J", "B", "Q", "9", "J", "10", "R", "F", "10", "9", "R", "B", "Q", "F", "9", "J", "10", "K", "B", "R", "9", "10", "Q", "K", "J", "Q", "10", "9", "F", "B", "K", "Q", "9", "B", "K", "Q", "F", "10", "K"},
	// Reel 2
	{"10", "Q", "9", "K", "J", "10", "F", "R", "Q", "B", "F", "9", "Q", "K", "R", "B", "W", "10", "9", "F", "10", "Q", "9", "J", "R", "10", "F", "9", "Q", "10", "B", "S", "K", "9", "10", "B", "Q", "9", "10", "J", "R", "F", "K", "9", "J", "R", "10", "B", "Q", "K", "9", "B", "J", "K", "10", "B", "R", "Q", "9", "10", "K", "Q", "J", "10", "9", "Q", "J", "K", "B", "9", "J", "K", "Q", "10", "S", "R", "J", "9", "10", "Q", "K", "J", "R", "9", "Q", "10", "K", "J", "F", "Q", "10", "J", "K", "9", "F", "S", "Q", "J", "F", "9", "Q", "10", "F", "B", "B", "9", "9", "B", "B", "Q", "Q", "10", "10", "J", "J", "S", "F", "F", "9", "9", "R", "R", "S", "J", "J", "10", "10", "K", "K", "Q", "Q", "F", "F", "J", "J", "K", "K", "R", "R", "B", "B", "S"},
	// Reel 3
	{"Q", "Q", "9", "9", "10", "10", "K", "K", "J", "J", "B", "B", "F", "F", "R", "R", "W", "Q", "Q", "9", "9", "J", "J", "Q", "Q", "F", "F", "9", "9", "Q", "Q", "S", "J", "J", "9", "9", "R", "R", "Q", "Q", "B", "B", "10", "10", "9", "9", "9", "J", "J", "J", "10", "10", "Q", "Q", "Q", "R", "R", "J", "J", "K", "K", "B", "B", "9", "9", "9", "K", "K", "K", "Q", "Q", "10", "10", "10", "S", "J", "J", "J", "B", "B", "K", "K", "9", "9", "Q", "Q", "J", "J", "10", "10", "10", "F", "F", "9", "9", "S", "K", "K", "K", "F", "F", "10", "10", "B", "B", "9", "9", "R", "R", "K", "K", "Q", "Q", "10", "10", "S", "F", "F", "9", "9", "B", "B", "S", "10", "10", "Q", "Q", "J", "J", "K", "K", "F", "F", "10", "10", "J", "J", "B", "B", "R", "R", "S"},
	// Reel 4
	{"9", "K", "Q", "R", "K", "B", "9", "J", "R", "F", "K", "Q", "R", "9", "10", "J", "W", "K", "J", "9", "F", "K", "J", "9", "Q", "R", "J", "9", "R", "K", "J", "S", "R", "K", "F", "J", "10", "9", "F", "R", "B", "F", "J", "K", "9", "R", "B", "K", "Q", "R", "J", "9", "Q", "R", "F", "J", "B", "K", "F", "R", "B", "9", "10", "K", "R", "J", "B", "10", "9", "R", "K", "10", "J", "9", "S", "J", "K", "10", "9", "J", "B", "K", "Q", "R", "J", "K", "R", "9", "B", "K", "9", "Q", "J", "R", "K", "S", "J", "Q", "10", "R", "J", "B", "Q", "9", "J", "9", "R", "F", "10", "9", "R", "K", "J", "F", "9", "S", "J", "9", "K", "J", "R", "9", "S", "10", "J", "K", "R", "Q", "10", "9", "R", "B", "K", "J", "9", "B", "K", "Q", "F", "9", "K", "S"},
	// Reel 5
	{"10", "Q", "B", "Q", "R", "B", "10", "Q", "F", "R", "10", "9", "K", "F", "10", "Q", "W", "B", "F", "R", "10", "F", "K", "Q", "B", "10", "R", "F", "Q", "B", "F", "Q", "10", "B", "F", "Q", "B", "10", "9", "B", "F", "B", "R", "F", "Q", "B", "K", "R", "10", "B", "F", "K", "B", "Q", "J", "F", "B", "R", "Q", "9", "B", "10", "R", "J", "10", "F", "B", "10", "Q", "10", "R", "Q", "10", "F", "B", "10", "F", "Q", "K", "10", "B", "9", "F", "Q", "B", "J", "K", "B", "10", "Q", "9", "K", "Q", "F", "9", "Q", "K", "9", "J", "10", "Q", "K", "9", "10", "J", "F", "10", "9", "K", "Q", "J", "9", "10", "F", "B", "F", "J", "K", "R", "J", "9", "10", "Q", "F", "J", "B", "10", "Q", "F", "J", "10", "F", "10", "Q", "B", "J"},
}

// MG/FG 補符
var reelsMGRefillStr = [][]string{
	// Reel 1
	{"Q", "10", "J", "9", "9", "K", "Q", "B", "F", "R", "S", "9", "B", "J", "R", "K", "K", "9", "10", "F", "Q", "9", "10", "10", "F", "Q", "9", "B", "B", "J", "J", "R", "K", "J", "9", "B", "B", "R", "10", "K", "10", "F", "10", "Q", "J", "R", "F", "9", "Q", "K", "10", "F", "9", "B", "R", "F", "J", "10", "R", "Q", "K", "9", "B", "10", "9", "Q", "B", "10", "K", "J", "R", "9", "J", "K", "10", "Q", "B", "9", "Q", "J", "J", "K", "10", "F", "9", "Q", "10", "K", "J", "B", "9", "Q", "9", "J", "K", "J", "10", "K", "9", "10", "Q", "Q", "J", "S", "9", "10", "J", "F", "Q", "9", "J", "J", "Q", "10", "9", "10", "Q"},
	// Reel 2
	{"R", "B", "10", "9", "9", "10", "W", "F", "9", "Q", "S", "10", "F", "Q", "9", "B", "10", "W", "J", "R", "K", "W", "J", "J", "R", "K", "10", "W", "J", "Q", "Q", "9", "B", "Q", "10", "F", "B", "9", "W", "B", "J", "Q", "B", "K", "Q", "9", "R", "W", "K", "B", "J", "F", "10", "F", "9", "R", "Q", "J", "R", "K", "B", "10", "J", "W", "10", "K", "10", "J", "9", "Q", "R", "10", "Q", "9", "J", "K", "B", "10", "K", "Q", "W", "9", "J", "F", "10", "K", "J", "9", "Q", "B", "10", "K", "W", "Q", "9", "F", "J", "9", "10", "J", "J", "K", "Q", "W", "10", "J", "Q", "F", "9", "10", "9", "K", "9", "J", "10", "9", "Q"},
	// Reel 3
	{"J", "F", "10", "R", "R", "K", "9", "Q", "9", "B", "S", "J", "Q", "K", "10", "F", "F", "10", "Q", "9", "B", "W", "Q", "Q", "9", "B", "J", "B", "R", "K", "K", "10", "F", "K", "J", "Q", "B", "10", "Q", "J", "Q", "9", "B", "B", "K", "10", "9", "F", "B", "J", "Q", "F", "J", "R", "10", "9", "K", "Q", "R", "10", "F", "J", "R", "W", "J", "9", "B", "Q", "10", "K", "R", "9", "Q", "10", "K", "J", "10", "J", "9", "K", "9", "10", "Q", "F", "J", "9", "Q", "10", "K", "B", "J", "9", "10", "9", "10", "F", "Q", "10", "9", "K", "J", "J", "Q", "W", "J", "Q", "9", "F", "10", "J", "10", "9", "Q", "9", "10", "9", "R"},
	// Reel 4
	{"9", "J", "K", "R", "9", "K", "J", "K", "9", "R", "S", "F", "J", "K", "9", "J", "R", "9", "F", "K", "J", "W", "K", "9", "B", "R", "F", "K", "B", "Q", "K", "J", "R", "B", "J", "9", "B", "J", "R", "9", "K", "10", "B", "F", "Q", "9", "K", "R", "Q", "R", "K", "F", "9", "J", "K", "F", "J", "9", "W", "9", "B", "K", "R", "J", "B", "K", "9", "F", "R", "K", "K", "J", "J", "R", "K", "9", "J", "9", "J", "10", "K", "R", "J", "10", "R", "J", "K", "9", "J", "K", "9", "R", "J", "Q", "R", "K", "Q", "10", "R", "J", "R", "10", "Q", "Q", "9", "K", "9", "W", "J", "Q", "9", "R", "10", "K", "J", "R", "10"},
	// Reel 5
	{"10", "Q", "B", "F", "J", "Q", "B", "F", "10", "R", "S", "B", "F", "Q", "F", "K", "10", "9", "Q", "B", "R", "W", "B", "10", "K", "F", "Q", "B", "R", "F", "10", "Q", "9", "F", "B", "10", "B", "Q", "10", "9", "B", "J", "R", "Q", "B", "F", "J", "F", "R", "10", "B", "F", "K", "10", "Q", "Q", "F", "B", "10", "9", "R", "Q", "B", "W", "10", "K", "F", "10", "Q", "F", "10", "B", "Q", "10", "R", "K", "B", "Q", "10", "B", "J", "K", "Q", "F", "10", "Q", "10", "J", "K", "B", "10", "Q", "9", "B", "Q", "F", "10", "B", "Q", "J", "10", "9", "F", "W", "9", "10", "J", "F", "Q", "10", "9", "B", "Q", "Q", "9", "10", "F"},
}

var reelsFGRefillStr = [][]string{
	// Reel 1
	{"Q", "10", "J", "9", "9", "K", "Q", "B", "F", "R", "9", "B", "J", "R", "K", "K", "9", "10", "F", "Q", "9", "10", "10", "F", "Q", "9", "B", "B", "J", "J", "R", "K", "J", "9", "B", "B", "R", "10", "K", "10", "F", "B", "Q", "J", "R", "F", "K", "Q", "K", "10", "F", "9", "B", "R", "F", "J", "10", "R", "Q", "K", "9", "B", "10", "9", "Q", "B", "10", "K", "J", "R", "9", "J", "K", "10", "Q", "B", "9", "Q", "J", "J", "K", "10", "F", "9", "Q", "10", "K", "J", "B", "9", "Q", "9", "J", "K", "F", "10", "K", "9", "10", "Q", "Q", "J", "9", "10", "J", "F", "Q", "9", "J", "J", "Q", "10", "9", "10", "R"},
	// Reel 2
	{"R", "B", "10", "9", "9", "J", "W", "F", "K", "Q", "S", "10", "F", "Q", "9", "B", "B", "W", "J", "R", "K", "W", "J", "J", "R", "K", "10", "W", "F", "Q", "Q", "9", "B", "Q", "10", "F", "B", "9", "W", "B", "J", "R", "B", "K", "Q", "9", "R", "W", "K", "B", "J", "F", "10", "F", "9", "R", "Q", "J", "R", "K", "B", "10", "F", "W", "10", "K", "B", "J", "9", "Q", "R", "10", "Q", "9", "J", "K", "B", "10", "K", "Q", "W", "9", "J", "F", "10", "K", "J", "9", "Q", "B", "10", "K", "W", "Q", "9", "F", "J", "9", "10", "J", "J", "K", "Q", "W", "10", "J", "Q", "F", "9", "10", "9", "K", "K", "J", "10", "9", "R"},
	// Reel 3
	{"J", "F", "10", "R", "R", "K", "9", "Q", "9", "B", "S", "J", "R", "K", "10", "F", "F", "10", "Q", "9", "B", "W", "Q", "Q", "9", "B", "J", "B", "R", "K", "K", "10", "F", "K", "J", "R", "B", "10", "Q", "F", "Q", "9", "B", "B", "K", "10", "9", "F", "B", "F", "Q", "F", "J", "R", "10", "9", "K", "Q", "R", "B", "F", "J", "R", "W", "J", "9", "B", "Q", "10", "K", "R", "9", "Q", "10", "K", "J", "B", "J", "9", "K", "9", "10", "Q", "F", "J", "9", "Q", "10", "K", "B", "J", "9", "10", "K", "10", "F", "Q", "10", "9", "K", "J", "J", "Q", "W", "J", "Q", "9", "F", "10", "J", "10", "K", "Q", "9", "10", "9", "R"},
	// Reel 4
	{"Q", "10", "R", "K", "K", "B", "J", "J", "F", "9", "S", "Q", "9", "B", "J", "R", "R", "J", "K", "10", "F", "W", "K", "K", "10", "F", "Q", "9", "9", "B", "B", "J", "R", "B", "Q", "9", "B", "J", "9", "R", "K", "10", "B", "F", "B", "J", "10", "F", "F", "R", "K", "F", "Q", "9", "J", "10", "B", "K", "R", "F", "R", "Q", "9", "W", "9", "J", "B", "K", "10", "Q", "R", "10", "K", "J", "9", "Q", "B", "9", "J", "Q", "10", "10", "K", "F", "9", "J", "K", "10", "Q", "B", "9", "J", "10", "Q", "10", "F", "K", "J", "10", "9", "Q", "Q", "K", "W", "Q", "9", "10", "F", "J", "9", "J", "K", "Q", "10", "9", "10", "R"},
	// Reel 5
	{"9", "K", "J", "Q", "Q", "R", "R", "B", "F", "10", "K", "10", "F", "Q", "9", "9", "J", "B", "J", "R", "W", "B", "B", "J", "R", "K", "10", "10", "F", "F", "Q", "9", "F", "K", "10", "B", "Q", "10", "9", "B", "J", "B", "R", "F", "Q", "J", "K", "R", "9", "B", "F", "K", "10", "Q", "J", "F", "B", "R", "R", "9", "K", "10", "W", "10", "Q", "B", "9", "J", "K", "R", "J", "9", "Q", "10", "K", "B", "10", "Q", "K", "J", "J", "9", "F", "10", "Q", "9", "J", "K", "B", "10", "Q", "9", "K", "J", "F", "9", "Q", "J", "10", "K", "K", "9", "W", "9", "10", "J", "F", "Q", "10", "9", "K", "Q", "J", "9", "10", "R"},
}

// AftX：MG/FG
var reelsMGRefillStrAftX = [][]string{
	// Reel 1
	{"9", "9", "S", "10", "10", "J", "J", "Q", "Q", "K", "K", "B", "B", "F", "F", "R", "R", "9", "9", "J", "J", "J", "9", "9", "9", "Q", "Q", "10", "10", "J", "J", "K", "K", "10", "10", "B", "B", "9", "9", "9", "Q", "Q", "F", "F", "J", "J", "10", "10", "10", "R", "R", "F", "F", "K", "K", "B", "B", "R", "R", "9", "9", "Q", "Q", "10", "10", "10", "J", "J", "J", "10", "10", "9", "9", "9", "Q", "Q", "F", "F", "J", "J", "B", "B", "J", "J", "10", "10", "K", "K", "9", "9", "B", "B", "Q", "Q", "9", "9", "9", "K", "K", "10", "10", "K", "K", "Q", "Q"},
	// Reel 2
	{"Q", "Q", "J", "J", "S", "9", "9", "10", "10", "B", "B", "K", "K", "R", "R", "F", "F", "Q", "Q", "9", "9", "J", "J", "J", "Q", "Q", "B", "B", "B", "J", "J", "K", "K", "9", "9", "10", "10", "J", "J", "B", "B", "Q", "Q", "K", "K", "10", "10", "10", "Q", "Q", "R", "R", "J", "J", "9", "9", "F", "F", "10", "10", "Q", "Q", "J", "J", "K", "K", "9", "9", "10", "10", "10", "J", "J", "J", "R", "R", "10", "10", "Q", "Q", "Q", "F", "F", "9", "9", "Q", "Q", "B", "B", "9", "9", "R", "R", "Q", "Q", "Q", "K", "K", "J", "J", "B", "B", "B", "9", "9"},
	// Reel 3
	{"10", "10", "J", "J", "Q", "Q", "S", "9", "9", "K", "K", "F", "F", "B", "B", "R", "R", "10", "10", "Q", "Q", "10", "10", "9", "9", "9", "J", "J", "10", "10", "10", "K", "K", "J", "J", "9", "9", "K", "K", "J", "J", "J", "Q", "Q", "10", "10", "F", "F", "Q", "Q", "J", "J", "J", "9", "9", "9", "10", "10", "10", "K", "K", "B", "B", "J", "J", "Q", "Q", "K", "K", "B", "B", "10", "10", "F", "F", "Q", "Q", "R", "R", "B", "B", "J", "J", "F", "F", "Q", "Q", "10", "10", "R", "R", "9", "9", "F", "F", "J", "J", "K", "K", "Q", "Q", "10", "10", "9", "9"},
	// Reel 4
	{"9", "J", "10", "K", "R", "J", "B", "10", "9", "S", "R", "Q", "F", "K", "B", "Q", "F", "9", "10", "J", "Q", "B", "10", "K", "J", "9", "Q", "10", "F", "K", "9", "J", "10", "F", "9", "J", "B", "Q", "9", "R", "J", "Q", "F", "10", "9", "J", "R", "F", "9", "10", "J", "F", "9", "10", "R", "J", "B", "Q", "F", "J", "10", "Q", "9", "J", "10", "R", "9", "K", "J", "B", "10", "K", "9", "Q", "10", "J", "9", "K", "Q", "J", "10", "R", "9", "Q", "J", "R", "9", "K", "J", "Q", "9", "K", "R", "Q", "9", "10", "R", "Q", "9", "K", "R", "10", "9", "J", "R"},
	// Reel 5
	{"10", "9", "K", "F", "10", "Q", "J", "B", "10", "R", "S", "K", "10", "R", "F", "9", "J", "Q", "B", "10", "F", "J", "9", "K", "Q", "10", "J", "9", "F", "10", "Q", "K", "F", "J", "10", "Q", "9", "F", "10", "J", "9", "K", "R", "Q", "J", "10", "9", "K", "Q", "R", "J", "9", "10", "Q", "B", "J", "F", "9", "10", "J", "K", "Q", "10", "B", "Q", "9", "F", "10", "K", "Q", "B", "J", "10", "9", "F", "B", "10", "J", "K", "B", "9", "10", "K", "Q", "9", "R", "10", "J", "9", "R", "Q", "J", "9", "B", "F", "J", "9", "10", "Q", "F", "9", "J", "F", "10", "9"},
}

var reelsFGRefillStrAftX = [][]string{
	// Reel 1
	{"9", "9", "9", "10", "10", "J", "J", "Q", "Q", "K", "K", "B", "B", "F", "F", "R", "R", "9", "9", "J", "J", "J", "9", "9", "9", "Q", "Q", "10", "10", "J", "J", "K", "K", "10", "10", "B", "B", "9", "9", "9", "Q", "Q", "F", "F", "J", "J", "10", "10", "10", "R", "R", "F", "F", "K", "K", "B", "B", "R", "R", "9", "9", "Q", "Q", "10", "10", "10", "J", "J", "J", "10", "10", "9", "9", "9", "Q", "Q", "F", "F", "J", "J", "B", "B", "J", "J", "10", "10", "K", "K", "9", "9", "B", "B", "Q", "Q", "9", "9", "9", "K", "K", "10", "10", "K", "K", "Q", "Q"},
	// Reel 2
	{"Q", "Q", "J", "J", "S", "9", "9", "10", "10", "B", "B", "K", "K", "R", "R", "F", "F", "Q", "Q", "9", "9", "J", "J", "J", "Q", "Q", "B", "B", "B", "J", "J", "K", "K", "9", "9", "10", "10", "J", "J", "B", "B", "Q", "Q", "K", "K", "10", "10", "10", "Q", "Q", "R", "R", "J", "J", "9", "9", "F", "F", "10", "10", "Q", "Q", "J", "J", "K", "K", "9", "9", "10", "10", "10", "J", "J", "J", "R", "R", "10", "10", "Q", "Q", "Q", "F", "F", "9", "9", "Q", "Q", "B", "B", "9", "9", "R", "R", "Q", "Q", "Q", "K", "K", "J", "J", "B", "B", "B", "9", "9"},
	// Reel 3
	{"10", "10", "J", "J", "Q", "Q", "S", "9", "9", "K", "K", "F", "F", "B", "B", "R", "R", "10", "10", "Q", "Q", "10", "10", "9", "9", "9", "J", "J", "10", "10", "10", "K", "K", "J", "J", "9", "9", "K", "K", "J", "J", "J", "Q", "Q", "10", "10", "F", "F", "Q", "Q", "J", "J", "J", "9", "9", "9", "10", "10", "10", "K", "K", "B", "B", "J", "J", "Q", "Q", "K", "K", "B", "B", "10", "10", "F", "F", "Q", "Q", "R", "R", "B", "B", "J", "J", "F", "F", "Q", "Q", "10", "10", "R", "R", "9", "9", "F", "F", "J", "J", "K", "K", "Q", "Q", "10", "10", "9", "9"},
	// Reel 4
	{"9", "J", "10", "Q", "9", "10", "J", "Q", "S", "9", "R", "Q", "10", "9", "J", "K", "B", "9", "Q", "10", "J", "F", "B", "10", "K", "R", "J", "10", "F", "9", "K", "J", "B", "9", "Q", "J", "F", "9", "R", "Q", "J", "10", "F", "9", "J", "10", "B", "R", "F", "J", "10", "9", "R", "F", "J", "9", "K", "10", "F", "J", "9", "10", "B", "J", "9", "R", "F", "10", "9", "J", "B", "Q", "9", "K", "10", "J", "Q", "R", "10", "J", "K", "9", "Q", "R", "J", "K", "Q", "9", "R", "K", "Q", "J", "9", "K", "Q", "R", "10", "9", "Q", "R", "K", "9", "10", "R", "Q"},
	// Reel 5
	{"10", "9", "J", "Q", "10", "F", "9", "J", "K", "Q", "10", "J", "F", "R", "10", "K", "B", "J", "9", "10", "F", "Q", "9", "10", "B", "Q", "9", "10", "F", "Q", "9", "J", "F", "Q", "10", "K", "J", "F", "Q", "10", "9", "J", "Q", "K", "10", "9", "J", "Q", "R", "10", "J", "K", "9", "R", "Q", "K", "J", "9", "10", "Q", "B", "9", "K", "10", "B", "9", "Q", "K", "F", "9", "B", "10", "F", "J", "B", "10", "9", "J", "K", "10", "F", "R", "9", "K", "B", "10", "Q", "J", "9", "B", "R", "F", "9", "Q", "10", "J", "9", "F", "10", "J", "R", "K", "10", "J", "F"},
}

// FG：END
var reelsFG_END_SpinStr = [][]string{
	// Reel 1
	{"9", "9", "K", "K", "J", "J", "F", "F", "R", "R", "B", "B", "Q", "Q", "10", "10", "K", "K", "9", "9", "J", "J", "Q", "K", "K", "J", "J", "9", "9", "9", "B", "K", "K", "K", "R", "J", "J", "9", "9", "R", "K", "K", "9", "9", "F", "F", "K", "K", "B", "J", "J", "F", "F", "9", "9", "J", "J", "J", "Q", "9", "9", "9", "F", "F", "9", "9", "K", "K", "B", "J", "J", "9", "9", "J", "J", "9", "9", "K", "K", "F", "F", "9", "9", "K", "K", "J", "J", "F", "F", "K", "K", "J", "J", "F", "F", "K", "K", "J", "J", "10"},
	// Reel 2
	{"10", "10", "Q", "Q", "B", "B", "R", "R", "F", "F", "J", "J", "K", "K", "9", "9", "Q", "Q", "10", "10", "B", "B", "K", "Q", "Q", "B", "B", "10", "10", "10", "J", "Q", "Q", "Q", "F", "B", "B", "10", "10", "F", "Q", "Q", "10", "10", "R", "R", "Q", "Q", "J", "B", "B", "R", "R", "10", "10", "B", "B", "B", "K", "10", "10", "10", "R", "R", "10", "10", "Q", "Q", "J", "B", "B", "10", "10", "B", "B", "10", "10", "Q", "Q", "R", "R", "10", "10", "Q", "Q", "B", "B", "R", "R", "Q", "Q", "B", "B", "R", "R", "Q", "Q", "B", "B", "9"},
	// Reel 3
	{"9", "9", "F", "F", "9", "9", "Q", "Q", "9", "9", "J", "J", "F", "F", "10", "10", "B", "B", "R", "R", "K", "K", "B", "B", "9", "9", "F", "F", "10", "10", "R", "R", "J", "J", "Q", "Q", "9", "9", "10", "10", "K", "K", "Q", "Q", "F", "F", "J", "J", "B", "B", "R", "R", "10", "10", "9", "9", "R", "R", "K", "K", "J", "J", "10", "10", "F", "F", "Q", "Q", "J", "J", "K", "K", "B", "B", "10", "10", "F", "F", "Q", "Q", "9", "9", "R", "R", "K", "K", "J", "J", "10", "10", "9", "9", "F", "F", "10", "10", "Q", "Q", "R", "R"},
	// Reel 4
	{"9", "10", "J", "K", "9", "Q", "J", "10", "Q", "K", "R", "F", "9", "J", "R", "B", "9", "F", "10", "J", "B", "R", "9", "J", "B", "10", "9", "R", "J", "10", "K", "R", "J", "10", "Q", "9", "R", "10", "K", "9", "J", "10", "Q", "9", "J", "10", "Q", "B", "J", "10", "9", "Q", "J", "10", "9", "Q", "J", "R", "9", "10", "J", "Q", "9", "F", "J", "10", "Q", "9", "J", "10", "Q", "9", "J", "10", "9", "10", "9", "J", "Q", "9", "J", "9", "10", "9", "10", "Q", "9", "9", "J", "10", "J", "9", "Q", "9", "10", "J", "10", "J", "Q", "J"},
	// Reel 5
	{"K", "F", "J", "Q", "10", "B", "K", "9", "F", "J", "B", "Q", "K", "9", "10", "R", "K", "B", "10", "F", "9", "B", "R", "F", "10", "K", "9", "F", "J", "K", "R", "J", "Q", "10", "F", "R", "K", "9", "Q", "F", "10", "K", "Q", "9", "J", "F", "B", "Q", "R", "10", "J", "B", "R", "J", "9", "10", "K", "B", "9", "F", "Q", "K", "J", "R", "B", "K", "J", "Q", "R", "F", "J", "K", "B", "10", "F", "Q", "B", "9", "F", "Q", "10", "9", "R", "K", "J", "9", "B", "R", "K", "Q", "10", "J", "R", "F", "B", "9", "Q", "10", "F", "R"},
}

var reelsFG_END_RefillStr = reelsFG_END_SpinStr

func symCode(s string) uint8 {
	switch s {
	case "9":
		return S9
	case "10":
		return S10
	case "J":
		return SJ
	case "Q":
		return SQ
	case "K":
		return SK
	case "B":
		return SB
	case "F":
		return SF
	case "R":
		return SR
	case "W":
		return SW
	case "S":
		return SS
	}
	panic("unknown symbol: " + s)
}
func packReels(src [][]string) [][]uint8 {
	dst := make([][]uint8, len(src))
	for i := range src {
		dst[i] = make([]uint8, len(src[i]))
		for j := range src[i] {
			dst[i][j] = symCode(src[i][j])
		}
	}
	return dst
}

var (
	reelsMGSpin, reelsMGRefill           [][]uint8
	reelsFGSpin, reelsFGRefill           [][]uint8
	reelsMGRefillAftX, reelsFGRefillAftX [][]uint8
	reelsFG_END_Spin, reelsFG_END_Refill [][]uint8
)

/**************
 * 視窗
 **************/
type window4x5 struct {
	c [reelsCount][rows]uint8

	spinTopIdx   [reelsCount]int
	refillTopIdx [reelsCount]int
	refillSeeded [reelsCount]bool
}

func (w *window4x5) spinInit(rng *rand.Rand, spinReels [][]uint8) {
	for r := 0; r < reelsCount; r++ {
		L := len(spinReels[r])
		stop := rng.Intn(L)
		w.spinTopIdx[r] = stop
		for row := 0; row < rows; row++ {
			w.c[r][row] = spinReels[r][(stop+row)%L]
		}
		w.refillSeeded[r] = false
	}
}
func resetHybridRefillState(w *window4x5) {
	for r := 0; r < reelsCount; r++ {
		w.refillSeeded[r] = false
		w.refillTopIdx[r] = 0
	}
}

/**************
 * Ways 計算
 **************/
func reelMatchCounts(w *window4x5, t uint8) (cnt [reelsCount]int) {
	for r := 0; r < reelsCount; r++ {
		k := 0
		for row := 0; row < rows; row++ {
			s := w.c[r][row]
			if s == SS {
				continue
			}
			if s == t || s == SW {
				k++
			}
		}
		cnt[r] = k
	}
	return
}
func maxLenForSymbol(cnt [reelsCount]int) int {
	L := 0
	for r := 0; r < reelsCount; r++ {
		if cnt[r] == 0 {
			break
		}
		L++
	}
	if L > maxLen {
		L = maxLen
	}
	return L
}
func waysForLength(cnt [reelsCount]int, L int) int {
	if L < minPayLen {
		return 0
	}
	w := 1
	for r := 0; r < L; r++ {
		w *= cnt[r]
	}
	return w
}

type winner struct {
	sym uint8
	len int
}

func evalWays(w *window4x5) (win float64, winners []winner) {
	for t := uint8(0); t < NumSymbols; t++ {
		if t == SW || t == SS {
			continue
		}
		cnt := reelMatchCounts(w, t)
		L := maxLenForSymbol(cnt)
		if L < minPayLen {
			continue
		}
		if L > 5 {
			L = 5
		}
		w5 := waysForLength(cnt, 5)
		w4 := waysForLength(cnt, 4)
		w3 := waysForLength(cnt, 3)
		if w5 > 0 {
			win += float64(w5) * pay[t][2] * bet
			winners = append(winners, winner{t, 5})
		} else if w4 > 0 {
			win += float64(w4) * pay[t][1] * bet
			winners = append(winners, winner{t, 4})
		} else if w3 > 0 {
			win += float64(w3) * pay[t][0] * bet
			winners = append(winners, winner{t, 3})
		}
	}
	return
}

/**************
 * 符號最長等級記錄
 **************/
func lenToIdx(L int) int {
	switch L {
	case 3:
		return 0
	case 4:
		return 1
	case 5:
		return 2
	default:
		return -1
	}
}
func bumpLenCats(w *window4x5, dst *[NumSymbols][3]int64) {
	if dst == nil {
		return
	}
	for t := uint8(0); t < NumSymbols; t++ {
		if t == SW || t == SS {
			continue
		}
		cnt := reelMatchCounts(w, t)
		L := maxLenForSymbol(cnt)
		if L > 5 {
			L = 5
		}
		if idx := lenToIdx(L); idx >= 0 {
			dst[t][idx]++
		}
	}
}

/**************
 * Cascades（Hybrid 補符）
 **************/
func markWinningCells(w *window4x5, winners []winner) (mark [reelsCount][rows]bool) {
	for _, win := range winners {
		for r := 0; r < win.len; r++ {
			for row := 0; row < rows; row++ {
				s := w.c[r][row]
				if s == win.sym || s == SW {
					mark[r][row] = true
				}
			}
		}
	}
	return
}
func applyCascadesHybrid(rng *rand.Rand, refillReels [][]uint8, w *window4x5, winners []winner) (removedAny bool, removedByReel [reelsCount]int) {
	if len(winners) == 0 {
		return
	}
	mark := markWinningCells(w, winners)
	for r := 0; r < reelsCount; r++ {
		keep := make([]uint8, 0, rows)
		for row := 0; row < rows; row++ {
			if !mark[r][row] {
				keep = append(keep, w.c[r][row])
			}
		}
		removed := rows - len(keep)
		removedByReel[r] = removed
		if removed > 0 {
			removedAny = true
		}
		// 掉落
		for i := 0; i < len(keep); i++ {
			w.c[r][rows-len(keep)+i] = keep[i]
		}
		// 補符
		if removed > 0 {
			L := len(refillReels[r])
			if !w.refillSeeded[r] {
				w.refillTopIdx[r] = rng.Intn(L)
				w.refillSeeded[r] = true
			}
			newTop := (w.refillTopIdx[r] - removed) % L
			if newTop < 0 {
				newTop += L
			}
			for row := 0; row < removed; row++ {
				w.c[r][row] = refillReels[r][(newTop+row)%L]
			}
			w.refillTopIdx[r] = newTop
		}
	}
	return
}

/**************
 * Scatter 計數
 **************/
func countScatterAll(w *window4x5) (c int) {
	for r := 0; r < reelsCount; r++ {
		for row := 0; row < rows; row++ {
			if w.c[r][row] == SS {
				c++
			}
		}
	}
	return
}

/**************
 * 初始/再觸發場次
 **************/
func startSpinsByScatter(s int) int {
	if s < 3 {
		return 0
	}
	k := 8 + (s-3)*2
	if k > maxFGTotalSpins {
		k = maxFGTotalSpins
	}
	return k
}
func retriggerByScatter(s int) int {
	if s < 3 {
		return 0
	}
	return 8 + (s-3)*2
}

/**************
 * MG（含 AftX_MG）
 **************/
func playMGSpin(
	rng *rand.Rand,
	spinReels, refillReels [][]uint8,
	w *window4x5,
	mgComboHist *[21]int64,
	mgInitLenCount *[NumSymbols][3]int64, // 初轉 3/4/5連
	mgLenCount *[NumSymbols][3]int64, // 所有步 3/4/5連（初轉+消除）
) (totalWin float64, anyWin bool, trigFG bool, fgStart int, comboCnt int) {

	w.spinInit(rng, spinReels)

	usedRefill := refillReels
	switched := false

	mult := 1.0
	firstStep := true

	for {
		spinWin, winners := evalWays(w)
		if spinWin <= 0 {
			break
		}

		// 初轉：第一次有贏分的盤面
		if firstStep {
			bumpLenCats(w, mgInitLenCount)
			firstStep = false
		}
		// 所有步（初轉+消除）
		bumpLenCats(w, mgLenCount)

		anyWin = true
		totalWin += spinWin * mult
		comboCnt++

		// AftComboX_MG 判定：達門檻的這一步就開始用 AftX 補帶，所以「最早會被 AftX 影響到的盤面」是第 (AftComboX_MG+1) 消。
		if !switched && comboCnt >= AftComboX_MG {
			if rng.Float64() < pAftX_MG {
				usedRefill = reelsMGRefillAftX
				resetHybridRefillState(w)
				switched = true
			}
		}

		// 用當前的 usedRefill 做補貨（可能已經是 AftX）
		_, _ = applyCascadesHybrid(rng, usedRefill, w, winners)

		// 更新 MG 倍率（1 → 2 → 3 → 4 ）
		mult += 1
		if mult > 4 {
			mult = 4
		}
	}

	// Scatter 判定觸發 FG
	sAll := countScatterAll(w)
	if sAll >= 3 {
		trigFG = true
		fgStart = startSpinsByScatter(sAll)
	}

	// 連消次數（>20 併入 20）
	b := comboCnt
	if b > 20 {
		b = 20
	}
	mgComboHist[b]++
	return
}

/**************
 * FG（含 AftX_FG、END 機制）
 **************/
type FGRunResult struct {
	spins            int
	total            float64
	retri            int
	maxCombo         int
	segPeak          int
	usedEndSpin      int64 // 本段使用 END_Spin 的次數
	endRefillCutTurn int   // 切入 END_Refill 的「段內第幾轉」：若發生一次，回傳對應 turnIndex (>0)；未發生則 0
}

func nextFGMult(current int) int {
	if current >= 50 {
		return 50
	}
	if current == 1 {
		return 2
	}
	n := current + 2
	if n > 50 {
		n = 50
	}
	return n
}

func playFG(
	rng *rand.Rand,
	spinReels, refillReels [][]uint8,
	w *window4x5,
	initSpins int,
	fgComboHistPerSpin *[21]int64,
	peakMultHist *[51]int64, // 1..50
	peakMultAvg *float64,
	peakMultMax *int,
	fgRetriggerDist *[21]int64,
	fgInitLenCount *[NumSymbols][3]int64, // 初轉純 ways
	fgLenCount *[NumSymbols][3]int64, // 所有步（初轉+消除）
) (res FGRunResult) {

	queue := initSpins
	mult := 1 // 整段跨轉累計倍率：1,2,4,6..50
	segPeak := 1

	// END_Spin / END_Refill 統計
	usedEndSpinThisSeg := int64(0)
	endRefillCutTurn := 0
	hadEndRefill := false

	const (
		refillModeNormal = 0
		refillModeAftX   = 1
		refillModeEnd    = 2
	)

	for queue > 0 {
		queue--
		res.spins++
		turnIndex := res.spins // 1-based

		// 初轉輪帶：若 mult 達 END_Spin 門檻，以 pEndSpin 判斷是否使用 END_Spin
		spinBand := reelsFGSpin
		if mult >= aftMultX_FG_EndSpin && rng.Float64() < pEndSpin {
			spinBand = reelsFG_END_Spin
			usedEndSpinThisSeg++
		}
		w.spinInit(rng, spinBand)

		// 當前轉的補符模式（Normal/AftX/End），只吃本轉
		refillMode := refillModeNormal

		turnCombo := 0
		firstStep := true

		for {
			spinWin, winners := evalWays(w)
			if spinWin <= 0 {
				break
			}

			// 初轉：本轉第一次有贏分的盤面
			if firstStep {
				bumpLenCats(w, fgInitLenCount)
				firstStep = false
			}
			// 所有步（初轉+消除）
			bumpLenCats(w, fgLenCount)

			// 用目前 mult 計入
			res.total += float64(mult) * spinWin
			turnCombo++

			// 決定這一步補符要用哪一條帶（在實際補貨前判定）
			if mult >= aftMultX_FG_EndRefill {
				// 高倍率階段：優先嘗試 END_Refill，再嘗試高倍用 AftX
				if refillMode != refillModeEnd {
					if rng.Float64() < pEndRefill {
						// 切入 END_Refill，該轉剩餘補符都用 END_Refill
						refillMode = refillModeEnd
						if !hadEndRefill {
							hadEndRefill = true
							endRefillCutTurn = turnIndex
						}
						resetHybridRefillState(w)
					} else if refillMode == refillModeNormal && rng.Float64() < pAftX_FG_AftMultX {
						// END 失敗，嘗試高倍率階段的 AftX 降級（Normal -> AftX）
						refillMode = refillModeAftX
						resetHybridRefillState(w)
					}
				}
				// 若已在 END，保持 END_Refill 到該轉結束
			} else {
				// 尚未達倍率門檻：使用原本 combo-based AftX 邏輯
				if refillMode == refillModeNormal && turnCombo >= AftComboX_FG {
					if rng.Float64() < pAftX_FG {
						refillMode = refillModeAftX
						resetHybridRefillState(w)
					}
				}
			}

			var usedRefill [][]uint8
			switch refillMode {
			case refillModeNormal:
				usedRefill = refillReels
			case refillModeAftX:
				usedRefill = reelsFGRefillAftX
			case refillModeEnd:
				usedRefill = reelsFG_END_Refill
			}

			_, _ = applyCascadesHybrid(rng, usedRefill, w, winners)

			// 更新倍率到下一階（1→2→4→6...50 封頂）
			mult = nextFGMult(mult)
			if mult > segPeak {
				segPeak = mult
			}
		}

		// 再觸發判定（無贏分後）
		s := countScatterAll(w)
		if s >= 3 {
			add := retriggerByScatter(s)

			// 目前「這一段 FG」已經排定的總場次 = 已經打過的 + 還在 queue 裡的
			// 為了讓整段不超過 maxFGTotalSpins，剩餘空間：
			space := maxFGTotalSpins - (res.spins + queue)
			if space < 0 {
				space = 0
			}

			if add > space {
				add = space
			}
			if add > 0 {
				queue += add
				res.retri++
				// 統計用的 bucket 還是用「單次加了幾場」，維持原本 8,10,12.. 併入 20 的寫法
				b := add
				if b > 20 {
					b = 20
				}
				fgRetriggerDist[b]++
			}
		}

		// 記逐轉 combo
		c := turnCombo
		if c > 20 {
			c = 20
		}
		fgComboHistPerSpin[c]++

		if turnCombo > res.maxCombo {
			res.maxCombo = turnCombo
		}
	}

	// 段落峰值倍率統計
	if segPeak < 1 {
		segPeak = 1
	}
	if segPeak > 50 {
		segPeak = 50
	}
	peakMultHist[segPeak]++
	*peakMultAvg += float64(segPeak)
	if segPeak > *peakMultMax {
		*peakMultMax = segPeak
	}

	res.segPeak = segPeak
	res.usedEndSpin = usedEndSpinThisSeg
	res.endRefillCutTurn = endRefillCutTurn
	return
}

/**************
 * 統計
 **************/
type Stats struct {
	mainWinSum     float64
	freeWinSum     float64
	triggerCount   int64
	retriggerCount int64
	totalFGSpins   int64
	maxSingleSpin  float64
	deadSpins      int64
	mgHasWinCount  int64

	mgAvgCascades float64
	mgComboMax    int
	mgComboHist   [21]int64 // C=0..20

	fgComboMax  int
	fgComboHist [21]int64 // 逐轉 C=0..20

	// FG 起始場次分佈（8..50）
	fgStartDist [51]int64

	// FG 段落峰值倍率（1..50）
	peakMultAvg  float64
	peakMultMax  int
	peakMultHist [51]int64 // 1..50

	// 每段 FG 的實際總場次分佈（1..50）
	fgSegLenHist [51]int64

	// 大獎分層
	bigWins   int64 // ≥20×bet
	megaWins  int64 // ≥60×bet
	superWins int64 // ≥100×bet
	holyWins  int64 // ≥300×bet
	jumboWins int64 // ≥500×bet
	jojoWins  int64 // ≥1000×bet

	// 再觸發 +8..20 的分佈（彙總桶）
	retriggerDist [21]int64

	// END 統計
	endSpinUseCount      int64     // reelsFG_END_Spin 使用次數（全段累積）
	endRefillCutTurnHist [51]int64 // 切入 END_Refill 的「段內第幾轉」

	// MG/FG 各符號 3/4/5 連次數（忽略 ways 倍數，只記該步最長等級；初轉+消除）
	mgLenCount [NumSymbols][3]int64
	fgLenCount [NumSymbols][3]int64

	// MG/FG 初轉（純 ways）3/4/5 連次數
	mgInitLenCount [NumSymbols][3]int64
	fgInitLenCount [NumSymbols][3]int64
}

/**************
 * 心跳
 **************/
var spinsDone int64

func startProgress(total int64) func() {
	start := time.Now()
	tk := time.NewTicker(1 * time.Second)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-tk.C:
				done := atomic.LoadInt64(&spinsDone)
				elapsed := time.Since(start).Seconds()
				speed := float64(done) / (elapsed + 1e-9)
				eta := float64(total-done) / (speed + 1e-9)
				log.Printf("[PROGRESS] %d/%d (%.2f%%) | %.0f spins/s | ETA %.0fs",
					done, total, 100*float64(done)/float64(total), speed, eta)
			case <-stop:
				tk.Stop()
				return
			}
		}
	}()
	return func() { close(stop) }
}

/**************
 * Worker
 **************/
func everyStr(totalSpins, count int64) string {
	if count <= 0 {
		return "（—）"
	}
	return fmt.Sprintf("（約每 %d 轉一次）", int64(math.Round(float64(totalSpins)/float64(count))))
}
func worker(_ int, spins int64, out *Stats, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	var w window4x5
	local := Stats{}
	perSpinBet := bet

	const bump = 4096
	var bumpCnt int64

	for i := int64(0); i < spins; i++ {
		// MG
		mgWin, mgAnyWin, trig, fgStart, mgCombo := playMGSpin(
			rng, reelsMGSpin, reelsMGRefill, &w, &local.mgComboHist, &local.mgInitLenCount, &local.mgLenCount,
		)
		local.mainWinSum += mgWin
		if mgAnyWin {
			local.mgHasWinCount++
		}
		local.mgAvgCascades += float64(mgCombo)
		if mgCombo > local.mgComboMax {
			local.mgComboMax = mgCombo
		}

		spinTotal := mgWin

		if trig {
			// 起始場次
			if fgStart >= 8 && fgStart <= maxFGTotalSpins {
				local.fgStartDist[fgStart]++
			}

			res := playFG(
				rng,
				reelsFGSpin, reelsFGRefill,
				&w, fgStart,
				&local.fgComboHist,
				&local.peakMultHist, &local.peakMultAvg, &local.peakMultMax,
				&local.retriggerDist,
				&local.fgInitLenCount, &local.fgLenCount,
			)
			local.freeWinSum += res.total
			local.totalFGSpins += int64(res.spins)
			local.retriggerCount += int64(res.retri)
			local.triggerCount++
			spinTotal += res.total
			if res.maxCombo > local.fgComboMax {
				local.fgComboMax = res.maxCombo
			}

			// 段長分佈（1..50）
			segLen := res.spins
			if segLen < 1 {
				segLen = 1
			}
			if segLen > maxFGTotalSpins {
				segLen = maxFGTotalSpins
			}
			local.fgSegLenHist[segLen]++

			// END 統計
			local.endSpinUseCount += res.usedEndSpin
			if res.endRefillCutTurn > 0 {
				t := res.endRefillCutTurn
				if t > maxFGTotalSpins {
					t = maxFGTotalSpins
				}
				local.endRefillCutTurnHist[t]++
			}
		} else if !mgAnyWin {
			local.deadSpins++
		}

		// 大獎分層
		ratio := spinTotal / perSpinBet
		switch {
		case ratio >= 1000:
			local.jojoWins++
		case ratio >= 500:
			local.jumboWins++
		case ratio >= 300:
			local.holyWins++
		case ratio >= 100:
			local.superWins++
		case ratio >= 60:
			local.megaWins++
		case ratio >= 20:
			local.bigWins++
		}

		if spinTotal > local.maxSingleSpin {
			local.maxSingleSpin = spinTotal
		}

		bumpCnt++
		if bumpCnt == bump {
			atomic.AddInt64(&spinsDone, bump)
			bumpCnt = 0
		}
	}
	if bumpCnt > 0 {
		atomic.AddInt64(&spinsDone, bumpCnt)
	}
	*out = local
}

/**************
 * 主程式
 **************/
func main() {
	reelsMGSpin = packReels(reelsMGSpinStr)
	reelsMGRefill = packReels(reelsMGRefillStr)
	reelsFGSpin = packReels(reelsFGSpinStr)
	reelsFGRefill = packReels(reelsFGRefillStr)
	reelsMGRefillAftX = packReels(reelsMGRefillStrAftX)
	reelsFGRefillAftX = packReels(reelsFGRefillStrAftX)

	// 若 END strips 未提供，回退到 FG 對應版本，確保可執行
	if len(reelsFG_END_SpinStr) == 0 {
		reelsFG_END_SpinStr = reelsFGSpinStr
	}
	if len(reelsFG_END_RefillStr) == 0 {
		reelsFG_END_RefillStr = reelsFGRefillStr
	}
	reelsFG_END_Spin = packReels(reelsFG_END_SpinStr)
	reelsFG_END_Refill = packReels(reelsFG_END_RefillStr)

	runtime.GOMAXPROCS(workers)
	stopHB := startProgress(numSpins)
	defer stopHB()

	totalBet := float64(numSpins) * bet

	var wg sync.WaitGroup
	wg.Add(workers)
	stats := make([]Stats, workers)
	chunk := numSpins / int64(workers)
	rem := numSpins % int64(workers)
	baseSeed := time.Now().UnixNano()

	for w := 0; w < workers; w++ {
		spins := chunk
		if int64(w) < rem {
			spins++
		}
		go func(i int, n int64) {
			defer wg.Done()
			worker(i, n, &stats[i], baseSeed+int64(i)*1337)
		}(w, spins)
	}
	wg.Wait()

	// 匯總
	total := Stats{}
	var totalRetriggerDist [21]int64
	for i := 0; i < workers; i++ {
		s := stats[i]
		total.mainWinSum += s.mainWinSum
		total.freeWinSum += s.freeWinSum
		total.triggerCount += s.triggerCount
		total.retriggerCount += s.retriggerCount
		total.totalFGSpins += s.totalFGSpins
		if s.maxSingleSpin > total.maxSingleSpin {
			total.maxSingleSpin = s.maxSingleSpin
		}
		total.deadSpins += s.deadSpins
		total.mgHasWinCount += s.mgHasWinCount

		total.mgAvgCascades += s.mgAvgCascades
		if s.mgComboMax > total.mgComboMax {
			total.mgComboMax = s.mgComboMax
		}
		for k := 0; k <= 20; k++ {
			total.mgComboHist[k] += s.mgComboHist[k]
			total.fgComboHist[k] += s.fgComboHist[k]
		}
		if s.fgComboMax > total.fgComboMax {
			total.fgComboMax = s.fgComboMax
		}
		for k := 1; k <= maxFGTotalSpins; k++ {
			total.fgStartDist[k] += s.fgStartDist[k]
		}

		total.peakMultAvg += s.peakMultAvg
		if s.peakMultMax > total.peakMultMax {
			total.peakMultMax = s.peakMultMax
		}
		for k := 1; k <= 50; k++ {
			total.peakMultHist[k] += s.peakMultHist[k]
		}
		for k := 1; k <= maxFGTotalSpins; k++ {
			total.fgSegLenHist[k] += s.fgSegLenHist[k]
		}

		total.bigWins += s.bigWins
		total.megaWins += s.megaWins
		total.superWins += s.superWins
		total.holyWins += s.holyWins
		total.jumboWins += s.jumboWins
		total.jojoWins += s.jojoWins

		for k := 8; k <= 20; k += 2 {
			totalRetriggerDist[k] += s.retriggerDist[k]
		}

		// END 累計
		total.endSpinUseCount += s.endSpinUseCount
		for k := 1; k <= maxFGTotalSpins; k++ {
			total.endRefillCutTurnHist[k] += s.endRefillCutTurnHist[k]
		}

		// MG/FG 符號 3/4/5 連次數累加（總計與初轉）
		for t := 0; t < int(NumSymbols); t++ {
			for b := 0; b < 3; b++ {
				total.mgLenCount[t][b] += s.mgLenCount[t][b]
				total.fgLenCount[t][b] += s.fgLenCount[t][b]
				total.mgInitLenCount[t][b] += s.mgInitLenCount[t][b]
				total.fgInitLenCount[t][b] += s.fgInitLenCount[t][b]
			}
		}
	}

	totalWin := total.mainWinSum + total.freeWinSum
	rtpMG := total.mainWinSum / totalBet
	rtpFG := total.freeWinSum / totalBet
	rtpTotal := totalWin / totalBet

	// 輸出（原有段落維持不變）
	fmt.Printf("=== Monte Carlo | workers=%d | spins=%d | ways=%d | bet=%.2f ===\n",
		workers, numSpins, numWays, bet)
	fmt.Printf("AftX_MG   : AftComboX_MG=%d, pAftX_MG=%.3f\n",
		AftComboX_MG, pAftX_MG)
	fmt.Printf("AftX_FG   : AftComboX_FG=%d, pAftX_FG=%.3f, pAftX_FG_AftMultX=%.3f\n",
		AftComboX_FG, pAftX_FG, pAftX_FG_AftMultX)
	fmt.Printf("END_Refill: aftMultX_FG_EndRefill=%d, pEndRefill=%.3f\n",
		aftMultX_FG_EndRefill, pEndRefill)
	fmt.Printf("END_Spin  : aftMultX_FG_EndSpin=%d, pEndSpin=%.3f\n",
		aftMultX_FG_EndSpin, pEndSpin)
	fmt.Printf("總成本 (Total Bet)                    : %.2f\n", totalBet)
	fmt.Printf("總贏分 (Total Win)                    : %.2f\n", totalWin)
	fmt.Printf("最高單把贏分                           : %.2f (x%.2f)\n", total.maxSingleSpin, total.maxSingleSpin/bet)
	fmt.Printf("主遊戲 RTP                            : %.6f\n", rtpMG)
	fmt.Printf("免費遊戲 RTP                          : %.6f\n", rtpFG)
	fmt.Printf("總 RTP                                :%.6f\n", rtpTotal)

	// 觸發 觸發與再觸發的觀察值目前最高分別至16與12 保險統計至18
	fmt.Printf("免費遊戲觸發次數                      : %d (觸發率 %.6f) %s\n",
		total.triggerCount, float64(total.triggerCount)/float64(numSpins), everyStr(numSpins, total.triggerCount))
	for k := 8; k <= 18; k += 2 {
		cnt := total.fgStartDist[k]
		fmt.Printf("  └起始 %2d 轉                         : %d (機率 %.6f) %s\n",
			k, cnt, float64(cnt)/float64(numSpins), everyStr(numSpins, cnt))
	}
	fmt.Printf("免費遊戲再觸發次數                    : %d %s\n",
		total.retriggerCount, everyStr(numSpins, total.retriggerCount))
	for k := 8; k <= 18; k += 2 {
		cnt := totalRetriggerDist[k]
		fmt.Printf("  └再觸發 +%2d 轉                      : %d (機率 %.6f) %s\n",
			k, cnt, float64(cnt)/float64(numSpins), everyStr(numSpins, cnt))
	}

	if total.triggerCount > 0 {
		fmt.Printf("每次免費遊戲平均場次                   : %.3f\n",
			float64(total.totalFGSpins)/float64(total.triggerCount))
	}

	// MG Win Rate、Dead Spin
	fmt.Printf("MG 有贏分比例                         : %.6f %s\n",
		float64(total.mgHasWinCount)/float64(numSpins), everyStr(numSpins, total.mgHasWinCount))
	fmt.Printf("MG無贏分且無觸發FG                    : %d (空轉占比 %.6f)\n",
		total.deadSpins, float64(total.deadSpins)/float64(numSpins))

	// 最大連消
	fmt.Printf("MG 最高連消次數（combo）              : %d\n", total.mgComboMax)

	// FG
	var fgSpinCount int64
	for c := 0; c <= 20; c++ {
		fgSpinCount += total.fgComboHist[c]
	}

	fmt.Printf("FG 最高連消次數（combo）              : %d\n", total.fgComboMax)

	// FG 最高累計倍率（每段）— 1..50
	peakAvg := 0.0
	if total.triggerCount > 0 {
		peakAvg = total.peakMultAvg / float64(total.triggerCount)
	}
	fmt.Printf("FG 平均累計倍率（每段）               : %.3f\n", peakAvg)
	fmt.Printf("FG 最高累計倍率（每段）—（以觸發次數為分母）\n")
	for k := 1; k <= 50; k++ {
		cnt := total.peakMultHist[k]
		if cnt == 0 {
			continue
		}
		p := 0.0
		if total.triggerCount > 0 {
			p = float64(cnt) / float64(total.triggerCount)
		}
		fmt.Printf("  peak=%2d : %11d  (%.6f)\n", k, cnt, p)
	}

	// 新增：每段 FG 的實際總場次分佈（1..50）
	fmt.Printf("\n每段 FG 的實際總場次（以觸發次數為分母）上限 : %d\n", maxFGTotalSpins)
	for k := 1; k <= maxFGTotalSpins; k++ {
		cnt := total.fgSegLenHist[k]
		if cnt == 0 {
			continue
		}
		p := 0.0
		if total.triggerCount > 0 {
			p = float64(cnt) / float64(total.triggerCount)
		}
		fmt.Printf("  len=%2d : %11d  (%.6f)\n", k, cnt, p)
	}

	// MG 連消≥4
	var mgGE4 int64
	for c := 4; c <= 20; c++ {
		mgGE4 += total.mgComboHist[c]
	}
	fmt.Printf("\nMG 連消≥4 次數  : %d (占比 %.6f) %s\n", mgGE4, float64(mgGE4)/float64(numSpins), everyStr(numSpins, mgGE4))

	// 連消次數統計
	fmt.Printf("MG 連消次數分佈（每把） C=0..20，>20 併入 20：\n")
	for c := 0; c <= 20; c++ {
		cnt := total.mgComboHist[c]
		p := float64(cnt) / float64(numSpins)
		fmt.Printf("  C=%2d  : %11d  (%.6f) %s\n", c, cnt, p, everyStr(numSpins, cnt))
	}
	fmt.Printf("\nFG 連消次數分佈（逐轉） C=0..20，>20 併入 20：\n")
	if fgSpinCount == 0 {
		for c := 0; c <= 20; c++ {
			fmt.Printf("  C=%2d  : %11d  (%.6f) （—）\n", c, 0, 0.0)
		}
	} else {
		for c := 0; c <= 20; c++ {
			cnt := total.fgComboHist[c]
			p := float64(cnt) / float64(fgSpinCount)
			fmt.Printf("  C=%2d  : %11d  (%.6f) %s\n", c, cnt, p, everyStr(fgSpinCount, cnt))
		}
	}

	// END 統計輸出
	fmt.Printf("\nreelsFG_END_Spin 使用次數  : %d\n", total.endSpinUseCount)
	fmt.Printf("切入 reelsFG_END_Refill 的時機（FG 段的第幾轉）\n")
	for k := 1; k <= maxFGTotalSpins; k++ {
		cnt := total.endRefillCutTurnHist[k]
		if cnt == 0 {
			continue
		}
		fmt.Printf("  第 %2d 轉 : %11d\n", k, cnt)
	}

	// 獎項分層
	fmt.Printf("\n獎項分佈\n")
	fmt.Printf("Big  Win  (≥20×bet)                   : %d (占比 %.6f) %s\n", total.bigWins, float64(total.bigWins)/float64(numSpins), everyStr(numSpins, total.bigWins))
	fmt.Printf("Mega Win  (≥60×bet)                   : %d (占比 %.6f) %s\n", total.megaWins, float64(total.megaWins)/float64(numSpins), everyStr(numSpins, total.megaWins))
	fmt.Printf("Super Win (≥100×bet)                  : %d (占比 %.6f) %s\n", total.superWins, float64(total.superWins)/float64(numSpins), everyStr(numSpins, total.superWins))
	fmt.Printf("Holy Win (≥300×bet)                   : %d (占比 %.6f) %s\n", total.holyWins, float64(total.holyWins)/float64(numSpins), everyStr(numSpins, total.holyWins))
	fmt.Printf("Jumbo Win (≥500×bet)                  : %d (占比 %.6f) %s\n", total.jumboWins, float64(total.jumboWins)/float64(numSpins), everyStr(numSpins, total.jumboWins))
	fmt.Printf("Jojo Win  (≥1000×bet)                 : %d (占比 %.6f) %s\n", total.jojoWins, float64(total.jojoWins)/float64(numSpins), everyStr(numSpins, total.jojoWins))

	// －－－ MG/FG 3/4/5連：初轉、消除新增、總計 －－－
	// MG
	fmt.Printf("\nMG 初轉各符號 3/4/5 連次數（忽略 ways 倍數，只記每步最長等級）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		c3 := total.mgInitLenCount[t][0]
		c4 := total.mgInitLenCount[t][1]
		c5 := total.mgInitLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), c3, c4, c5)
	}

	fmt.Printf("\nMG 因消除新增各符號 3/4/5 連次數（總計 − 初轉）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		a3 := total.mgLenCount[t][0] - total.mgInitLenCount[t][0]
		a4 := total.mgLenCount[t][1] - total.mgInitLenCount[t][1]
		a5 := total.mgLenCount[t][2] - total.mgInitLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), a3, a4, a5)
	}

	fmt.Printf("\nMG 各符號 3/4/5 連次數（忽略 ways 倍數，只記每步最長等級）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		c3, c4, c5 := total.mgLenCount[t][0], total.mgLenCount[t][1], total.mgLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), c3, c4, c5)
	}

	// FG
	fmt.Printf("\nFG 初轉各符號 3/4/5 連次數（忽略 ways 倍數，只記每步最長等級）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		c3 := total.fgInitLenCount[t][0]
		c4 := total.fgInitLenCount[t][1]
		c5 := total.fgInitLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), c3, c4, c5)
	}

	fmt.Printf("\nFG 因消除新增各符號 3/4/5 連次數（總計 − 初轉）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		a3 := total.fgLenCount[t][0] - total.fgInitLenCount[t][0]
		a4 := total.fgLenCount[t][1] - total.fgInitLenCount[t][1]
		a5 := total.fgLenCount[t][2] - total.fgInitLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), a3, a4, a5)
	}

	fmt.Printf("\nFG 各符號 3/4/5 連次數（忽略 ways 倍數，只記每步最長等級）\n")
	for _, t := range []int{int(S9), int(S10), int(SJ), int(SQ), int(SK), int(SB), int(SF), int(SR)} {
		c3, c4, c5 := total.fgLenCount[t][0], total.fgLenCount[t][1], total.fgLenCount[t][2]
		fmt.Printf("  %-2s : 3連=%12d  4連=%12d  5連=%12d\n", symLabel(t), c3, c4, c5)
	}
}

func symLabel(t int) string {
	switch t {
	case int(S9):
		return "9"
	case int(S10):
		return "10"
	case int(SJ):
		return "J"
	case int(SQ):
		return "Q"
	case int(SK):
		return "K"
	case int(SB):
		return "B"
	case int(SF):
		return "F"
	case int(SR):
		return "R"
	default:
		return "-"
	}
}
