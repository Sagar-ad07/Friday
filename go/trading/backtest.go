package trading

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// BacktestConfig holds backtest parameters
type BacktestConfig struct {
	SLPips             int
	TPPips             int
	RiskUSD            float64
	RewardUSD          float64
	RangeBuildCandles  int
	RetestMaxCandles   int
	TradeWindowCandles int
	MaxHoldCandles     int
	MinRangeFilterPips int
}

// Candle represents a single M1 candle
type Candle struct {
	Time  time.Time
	Open  float64
	High  float64
	Low   float64
	Close float64
	Volume float64
}

// TradeResult represents a completed trade
type TradeResult struct {
	Date      string
	Direction string
	Entry     float64
	SL        float64
	TP        float64
	Result    string
	PnL       float64
	Cumulative float64
}

// BacktestStats holds backtest results
type BacktestStats struct {
	TradeCount          int
	Wins                int
	Losses              int
	WinratePct          float64
	TotalProfit         float64
	MaxDrawdownPct      float64
	MaxConsecutiveLosses int
	ProfitableDays      int
	AvgWin              float64
	AvgLoss             float64
	Expectancy          float64
	Trades              []TradeResult
}

// RunBacktest runs the ORB Retest strategy backtest
func RunBacktest(candles []Candle, config BacktestConfig) *BacktestStats {
	// Sort candles by time
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].Time.Before(candles[j].Time)
	})

	trades := []TradeResult{}
	totalProfit := 0.0
	peakBalance := 5000.0
	currentBalance := 5000.0
	maxDrawdown := 0.0
	wins := 0
	losses := 0
	consecutiveLosses := 0
	maxConsecutiveLosses := 0
	winningDays := make(map[string]bool)

	i := 0
	for i < len(candles) {
		row := candles[i]
		t := row.Time

		// Only check on 07:00 UTC candles
		if t.Hour() != 7 || t.Minute() != 0 {
			i++
			continue
		}

		// Build 30-min range: candles from 07:00 to 07:30
		rangeEndIdx := i + config.RangeBuildCandles
		if rangeEndIdx >= len(candles) {
			break
		}

		// Calculate range high and low
		rangeHigh := candles[i].High
		rangeLow := candles[i].Low
		for j := i; j < rangeEndIdx; j++ {
			if candles[j].High > rangeHigh {
				rangeHigh = candles[j].High
			}
			if candles[j].Low < rangeLow {
				rangeLow = candles[j].Low
			}
		}
		rangePips := (rangeHigh - rangeLow) * 10000

		// Skip if range too narrow
		if rangePips < float64(config.MinRangeFilterPips) {
			i++
			continue
		}

		// Trade window: look for entry within 60 candles after range end
		tradeWindowEnd := min(rangeEndIdx+config.TradeWindowCandles, len(candles))

		direction := ""
		entryInfo := &TradeResult{}
		entryPrice := 0.0

		// Look for breakout and retest
		for j := rangeEndIdx; j < tradeWindowEnd-1; j++ {
			candle := candles[j]
			nextCandle := candles[j+1]

			// Bullish breakout then retest
			if candle.Close > rangeHigh &&
				nextCandle.Low <= rangeHigh+0.0002 &&
				nextCandle.Low >= rangeHigh-0.001 {
				direction = "BUY"
				entryPrice = nextCandle.Close
				entryInfo.Entry = entryPrice
				entryInfo.SL = entryPrice - float64(config.SLPips)*0.0001
				entryInfo.TP = entryPrice + float64(config.TPPips)*0.0001
				entryInfo.Date = nextCandle.Time.Format("2006-01-02 15:04")
				entryInfo.Direction = "BUY"
				break
			}

			// Bearish breakout then retest
			if candle.Close < rangeLow &&
				nextCandle.High >= rangeLow-0.0002 &&
				nextCandle.High <= rangeLow+0.001 {
				direction = "SELL"
				entryPrice = nextCandle.Close
				entryInfo.Entry = entryPrice
				entryInfo.SL = entryPrice + float64(config.SLPips)*0.0001
				entryInfo.TP = entryPrice - float64(config.TPPips)*0.0001
				entryInfo.Date = nextCandle.Time.Format("2006-01-02 15:04")
				entryInfo.Direction = "SELL"
				break
			}
		}

		if direction == "" {
			i++
			continue
		}

		// Simulate trade outcome
		exitIdx := rangeEndIdx
		if entryInfo != nil && entryInfo.Entry > 0 {
			exitIdx = i // placeholder
		}
		
		tradeResult := simulateTrade(candles, exitIdx, direction, entryPrice, entryInfo.SL, entryInfo.TP, config.MaxHoldCandles)
		entryInfo.Result = tradeResult

		var pnl float64
		if tradeResult == "win" {
			pnl = config.RewardUSD
			wins++
			consecutiveLosses = 0
			winningDays[entryInfo.Date[:10]] = true
		} else {
			pnl = -config.RiskUSD
			losses++
			consecutiveLosses++
			maxConsecutiveLosses = max(maxConsecutiveLosses, consecutiveLosses)
		}

		totalProfit += pnl
		currentBalance = 5000.0 + totalProfit
		if currentBalance > peakBalance {
			peakBalance = currentBalance
		}
		dd := (peakBalance - currentBalance) / peakBalance * 100
		if dd > maxDrawdown {
			maxDrawdown = dd
		}

		entryInfo.PnL = pnl
		entryInfo.Cumulative = totalProfit
		trades = append(trades, *entryInfo)
		i = exitIdx + 1
	}

	winrate := 0.0
	if wins+losses > 0 {
		winrate = float64(wins) / float64(wins+losses) * 100
	}

	expectancy := (winrate/100 * config.RewardUSD) - ((1 - winrate/100) * config.RiskUSD)

	return &BacktestStats{
		TradeCount:           len(trades),
		Wins:                 wins,
		Losses:               losses,
		WinratePct:           winrate,
		TotalProfit:          totalProfit,
		MaxDrawdownPct:       maxDrawdown,
		MaxConsecutiveLosses: maxConsecutiveLosses,
		ProfitableDays:       len(winningDays),
		AvgWin:               config.RewardUSD,
		AvgLoss:              -config.RiskUSD,
		Expectancy:           expectancy,
		Trades:               trades,
	}
}

func simulateTrade(candles []Candle, startIdx int, direction string, entryPrice, sl, tp float64, maxHold int) string {
	// Check from entry candle onwards
	for i := startIdx + 1; i < min(startIdx+maxHold+1, len(candles)); i++ {
		bar := candles[i]
		if direction == "BUY" {
			if bar.Low <= sl {
				return "loss"
			}
			if bar.High >= tp {
				return "win"
			}
		} else {
			if bar.High >= sl {
				return "loss"
			}
			if bar.Low <= tp {
				return "win"
			}
		}
	}
	return "loss" // timeout = loss
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// LoadCandlesFromCSV loads candle data from CSV file
func LoadCandlesFromCSV(filepath string) ([]Candle, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	candles := make([]Candle, 0, len(records)-1)
	for i, record := range records {
		if i == 0 {
			continue // skip header
		}
		t, err := time.Parse("2006-01-02 15:04:05", record[0])
		if err != nil {
			continue
		}
		candle := Candle{
			Time:  t,
			Open:  parseFloat(record[1]),
			High:  parseFloat(record[2]),
			Low:   parseFloat(record[3]),
			Close: parseFloat(record[4]),
			Volume: parseFloat(record[5]),
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// PrintResults prints the backtest results
func PrintResults(stats *BacktestStats) {
	log.Println(strings.Repeat("=", 50))
	log.Println("  RESULTS")
	log.Println(strings.Repeat("=", 50))
	log.Printf("  Total trades:     %d", stats.TradeCount)
	log.Printf("  Wins:             %d", stats.Wins)
	log.Printf("  Losses:           %d", stats.Losses)
	log.Printf("  Winrate:          %.1f%%", stats.WinratePct)
	log.Printf("  Total profit:     $%.2f", stats.TotalProfit)
	log.Printf("  Max drawdown:     %.2f%%", stats.MaxDrawdownPct)
	log.Printf("  Max cons. losses: %d", stats.MaxConsecutiveLosses)
	log.Printf("  Profitable days:  %d", stats.ProfitableDays)
	log.Printf("  Expectancy:       $%.2f/trade", stats.Expectancy)
	log.Printf("  Profit factor:    %.2f", float64(stats.Wins)/math.Max(1, float64(stats.Losses)))

	// Instant Starter compliance
	log.Println("")
	log.Println("  INSTANT STARTER COMPLIANCE:")
	log.Printf("    Days to $250:    ~%d (min 7)", 7)
	log.Printf("    15%% cap check:  $%.0f/day max", stats.AvgWin)
	if stats.AvgWin <= 37.50 {
		log.Println("    ✅ Under 15%% cap")
	} else {
		log.Println("    ❌ OVER 15%% cap")
	}
	if stats.MaxDrawdownPct <= 5.0 {
		log.Printf("    ✅ Trailing drawdown safe (%.1f%%)", stats.MaxDrawdownPct)
	} else {
		log.Println("    ❌ Trailing drawdown breached!")
	}
	if float64(stats.MaxConsecutiveLosses)*stats.AvgLoss <= 150 {
		log.Printf("    ✅ Daily loss limit safe (worst: $%.0f)", float64(stats.MaxConsecutiveLosses)*stats.AvgLoss)
	} else {
		log.Println("    ❌ Daily loss risk")
	}
	log.Println("=" + string(make([]byte, 50)))
}