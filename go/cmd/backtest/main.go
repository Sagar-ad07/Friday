package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

type Candle struct {
	Time  time.Time
	Open  float64
	High  float64
	Low   float64
	Close float64
}

type yahooResp struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error interface{} `json:"error"`
	} `json:"chart"`
}

func main() {
	fmt.Println("=== STRATEGY BACKTEST — 2026 DATA ===")
	candles := fetchH1Data()
	fmt.Printf("Fetched %d H1 candles from Yahoo\n", len(candles))
	fmt.Printf("Period: %s to %s\n\n", candles[0].Time.Format("Jan 02 2006"), candles[len(candles)-1].Time.Format("Jan 02 2006"))

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	// Test TPCS with 1:2 R:R
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  TPCS v4 (1:2 R:R) — RSI>55 ADX>25 pull=tight SL=12 TP=24")
	fmt.Println(strings.Repeat("=", 70))
	testTPCS(candles, closes)

	// Test MeanReversionH1Strategy (1:2 built-in)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  MeanRevH1 (1:2 R:R) — BB(20,2) RSI<35/>65")
	fmt.Println(strings.Repeat("=", 70))
	testMeanRev(candles, closes)

	// Test combined: TPCS in trending, MeanRev in ranging
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  Combined: TPCS (trending ADX>20) + MeanRev (ranging ADX<=20)")
	fmt.Println(strings.Repeat("=", 70))
	testCombined(candles, closes)

	// Test BBB-RSI strategy with wider bands
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  BB-RSI Wide (1:2) — BB(20,2.5) RSI<30/>70")
	fmt.Println(strings.Repeat("=", 70))
	testBBRSIWide(candles, closes)

	// TPCS with ADX>30 (stricter)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  TPCS ADX>30 (1:2) — RSI>55 pull=tight")
	fmt.Println(strings.Repeat("=", 70))
	testTPCSADX30(candles, closes)

	// TPCS with RSI>60 (stricter)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  TPCS RSI>60 (1:2) — ADX>25 pull=tight")
	fmt.Println(strings.Repeat("=", 70))
	testTPCSRSI60(candles, closes)

	// TPCS with session 12-17 only (peak liquidity)
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  TPCS 12-17UTC (1:2) — ADX>25 RSI>55 pull=tight")
	fmt.Println(strings.Repeat("=", 70))
	testTPCSPeak(candles, closes)

	// === MACD (EXNESS 24/7) — NO SESSION FILTER ===
	fmt.Println("\n" + strings.Repeat("#", 70))
	fmt.Println("  MACD(12,26,9) STRATEGY — no session filter (Exness 24/7)")
	fmt.Println(strings.Repeat("#", 70))

	for _, sl := range []float64{8, 10, 12, 15, 20} {
		tp := sl * 2
		fmt.Println(strings.Repeat("-", 70))
		fmt.Printf("  MACD crossover SL=%.0f TP=%.0f (1:2) — Exness 24/7\n", sl, tp)
		fmt.Println(strings.Repeat("-", 70))
		testMACD_Cross(candles, closes, sl, tp)
	}

	// MACD + ADX filter
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  MACD + ADX>25 — SL=12 TP=24 (1:2) Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testMACD_ADX(candles, closes)

	// MACD + RSI filter
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  MACD + RSI>50 for buy / RSI<50 for sell — SL=12 TP=24 Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testMACD_RSI(candles, closes)

	// MACD + histogram slope (momentum increasing)
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  MACD hist increasing + ADX>25 — SL=12 TP=24 Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testMACD_Momentum(candles, closes)

	// === HIGH WR TARGETED SEARCH (Exness 24/7) ===
	fmt.Println("")
	fmt.Println(strings.Repeat("#", 70))
	fmt.Println("  HIGH WR TARGET — Exness 24/7, SL=12 TP=24 (1:2)")
	fmt.Println(strings.Repeat("#", 70))

	// TPCS RSI>65 (very strong momentum required)
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS RSI>65 — SL=12 TP=24 (1:2) 08-20 UTC")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_RSI65(candles, closes)

	// MeanRev BB+RSI+ADX with 1:2 R:R
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  MeanRev BB+RSI+ADX — SL=1.5SD TP=3.0SD (1:2) Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testMeanRev1to2(candles, closes)

	// BB only (no indicators) with Exness 24/7 and 1:2 R:R
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  BB raw touch — SL=14 TP=28 (1:2) Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testBBRaw1to2(candles, closes)

	// TPCS ADX>30 (stronger trend with 1:2 R:R)
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS ADX>30 — SL=12 TP=24 (1:2) Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_ADX30_1to2(candles, closes)

	// TPCS no RSI filter, just EMA pullback (very simple)
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS EMA pullback only — SL=12 TP=24 (1:2) Exness 24/7")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_NoRSI(candles, closes)

	// === CREATIVE HIGH-WR SEARCH ===
	fmt.Println("")
	fmt.Println(strings.Repeat("#", 70))
	fmt.Println("  CREATIVE HIGH-WR STRATEGIES — SL=12 TP=24 (1:2)")
	fmt.Println(strings.Repeat("#", 70))

	// Asian session only (00-08 UTC)
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS Asian session (00-08 UTC) — SL=12 TP=24")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_Asian(candles, closes)

	// Counter-trend fade on extreme BB touch
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  Counter-trend fade BB extreme — SL=12 TP=24")
	fmt.Println(strings.Repeat("-", 70))
	testCounterTrendBB(candles, closes)

	// Consolidation breakout
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  Consolidation breakout — SL=12 TP=24")
	fmt.Println(strings.Repeat("-", 70))
	testConsolBreakout(candles, closes)

	// Daily trend filter
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS + D1 trend filter — SL=12 TP=24")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_Daily(candles, closes)

	// Partial TP close
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("  TPCS partial TP (12 then 24) — SL=12 TP1=12 TP2=24 08-20")
	fmt.Println(strings.Repeat("-", 70))
	testTPCS_PartialTP(candles, closes)
}

type trade struct {
	time   time.Time
	dir    string
	entry  float64
	sl     float64
	tp     float64
	result string
	pips   float64
}

func testTPCS(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]

		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }

		eLong := calcEMA(hist, 50)
		eShort := calcEMA(hist, 20)
		ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		adx := calcADX(hist, 14)
		if adx < 25 { continue }

		prevClose := hist[len(hist)-2]
		pip := 0.0001
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull
		backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55

		if !buyCond && !sellCond { continue }

		sl := 12 * pip
		tp := 24 * pip
		dir := "BUY"
		var slPrice, tpPrice float64
		if buyCond {
			dir = "BUY"; slPrice = price - sl; tpPrice = price + tp
		} else {
			dir = "SELL"; slPrice = price + sl; tpPrice = price - tp
		}

		result := "loss"
		var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" {
				if cc.Low <= slPrice { result = "loss"; pips = -12; break }
				if cc.High >= tpPrice { result = "win"; pips = 24; break }
			} else {
				if cc.High >= slPrice { result = "loss"; pips = -12; break }
				if cc.Low <= tpPrice { result = "win"; pips = 24; break }
			}
		}
		log = append(log, trade{c.Time, dir, price, slPrice, tpPrice, result, pips})
	}
	printStats("TPCS 1:2", log)
}

func testMeanRev(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]

		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }

		mean, std := meanStd(hist, 20)
		rsi := calcRSI(hist, 14)
		upper := mean + 2.0*std
		lower := mean - 2.0*std

		var buy, sell bool
		if rsi < 35 && price <= lower { buy = true }
		if rsi > 65 && price >= upper { sell = true }
		if !buy && !sell { continue }

		slDist := std * 1.5
		tpDist := std * 3.0
		dir := "BUY"
		var slPrice, tpPrice float64
		if buy {
			dir = "BUY"; slPrice = price - slDist; tpPrice = price + tpDist
		} else {
			dir = "SELL"; slPrice = price + slDist; tpPrice = price - tpDist
		}

		result := "loss"
		var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" {
				if cc.Low <= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }
				if cc.High >= tpPrice { result = "win"; pips = tpDist / 0.0001; break }
			} else {
				if cc.High >= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }
				if cc.Low <= tpPrice { result = "win"; pips = tpDist / 0.0001; break }
			}
		}
		log = append(log, trade{c.Time, dir, price, slPrice, tpPrice, result, pips})
	}
	printStats("MeanRevH1 1:2", log)
}

func testCombined(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]

		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }

		adx := calcADX(hist, 14)
		var sig *struct{ dir string; sl, tp float64 }

		if adx > 20 {
			// TPCS
			eLong := calcEMA(hist, 50)
			eShort := calcEMA(hist, 20)
			ePull := calcEMA(hist, 10)
			rsi := calcRSI(hist, 14)
			prevClose := hist[len(hist)-2]
			pip := 0.0001
			bullish := eShort > eLong
			bearish := eShort < eLong
			pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
			pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
			backAbove := price > ePull
			backBelow := price < ePull
			buyCond := bullish && pullZone && backAbove && rsi > 55
			sellCond := bearish && pullZoneDn && backBelow && rsi < 55
			if buyCond { sig = &struct{ dir string; sl, tp float64 }{"BUY", price - 12*pip, price + 24*pip} }
			if sellCond { sig = &struct{ dir string; sl, tp float64 }{"SELL", price + 12*pip, price - 24*pip} }
		} else {
			// MeanRev
			mean, std := meanStd(hist, 20)
			rsi := calcRSI(hist, 14)
			upper := mean + 2.0*std
			lower := mean - 2.0*std
			if rsi < 35 && price <= lower { sig = &struct{ dir string; sl, tp float64 }{"BUY", price - 1.5*std, price + 3.0*std} }
			if rsi > 65 && price >= upper { sig = &struct{ dir string; sl, tp float64 }{"SELL", price + 1.5*std, price - 3.0*std} }
		}

		if sig == nil { continue }

		result := "loss"
		var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if sig.dir == "BUY" {
				if cc.Low <= sig.sl { result = "loss"; pips = -(math.Abs(sig.sl-price) / 0.0001); break }
				if cc.High >= sig.tp { result = "win"; pips = math.Abs(sig.tp-price) / 0.0001; break }
			} else {
				if cc.High >= sig.sl { result = "loss"; pips = -(math.Abs(sig.sl-price) / 0.0001); break }
				if cc.Low <= sig.tp { result = "win"; pips = math.Abs(sig.tp-price) / 0.0001; break }
			}
		}
		log = append(log, trade{c.Time, sig.dir, price, sig.sl, sig.tp, result, pips})
	}
	printStats("Combined (regime)", log)
}

func testBBRSIWide(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]

		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }

		mean, std := meanStd(hist, 20)
		rsi := calcRSI(hist, 14)
		upper := mean + 2.5*std
		lower := mean - 2.5*std

		var buy, sell bool
		if rsi < 30 && price <= lower { buy = true }
		if rsi > 70 && price >= upper { sell = true }
		if !buy && !sell { continue }

		slDist := std * 1.5
		tpDist := std * 3.0
		dir := "BUY"
		var slPrice, tpPrice float64
		if buy {
			dir = "BUY"; slPrice = price - slDist; tpPrice = price + tpDist
		} else {
			dir = "SELL"; slPrice = price + slDist; tpPrice = price - tpDist
		}

		result := "loss"
		var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" {
				if cc.Low <= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }
				if cc.High >= tpPrice { result = "win"; pips = tpDist / 0.0001; break }
			} else {
				if cc.High >= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }
				if cc.Low <= tpPrice { result = "win"; pips = tpDist / 0.0001; break }
			}
		}
		log = append(log, trade{c.Time, dir, price, slPrice, tpPrice, result, pips})
	}
	printStats("BB-RSI Wide 1:2", log)
}

func testTPCSADX30(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		adx := calcADX(hist, 14)
		if adx < 30 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		prevClose := hist[len(hist)-2]; pip := 0.0001
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS ADX>30 1:2", log)
}

func testTPCSRSI60(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		adx := calcADX(hist, 14); if adx < 25 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		prevClose := hist[len(hist)-2]; pip := 0.0001
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 60
		sellCond := bearish && pullZoneDn && backBelow && rsi < 50
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS RSI>60 1:2", log)
}

func testTPCSPeak(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]; c := candles[i]
		if c.Time.Hour() < 12 || c.Time.Hour() >= 17 { continue }
		if len(hist) < 75 { continue }
		adx := calcADX(hist, 14); if adx < 25 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		prevClose := hist[len(hist)-2]; pip := 0.0001
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS 12-17UTC 1:2", log)
}

func testBBRaw(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }
		mean, std := meanStd(hist, 20)
		upper := mean + 2.0*std; lower := mean - 2.0*std
		if price < lower || price > upper {
			dir := "BUY"; sl := price - 14*0.0001; tp := price + 41*0.0001
			if price > upper { dir = "SELL"; sl = price + 14*0.0001; tp = price - 41*0.0001 }
			result := "loss"; var pips float64
			for j := i + 1; j < min(i+48, len(candles)); j++ {
				cc := candles[j]
				if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -14; break }; if cc.High >= tp { result = "win"; pips = 41; break } } else { if cc.High >= sl { result = "loss"; pips = -14; break }; if cc.Low <= tp { result = "win"; pips = 41; break } }
			}
			log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
		}
	}
	printStats("BB raw 1:2.93", log)
}

func testBBRSI(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }
		mean, std := meanStd(hist, 20); rsi := calcRSI(hist, 14)
		upper := mean + 2.0*std; lower := mean - 2.0*std
		buy := price < lower && rsi < 30
		sell := price > upper && rsi > 70
		if !buy && !sell { continue }
		dir := "BUY"; sl := price - 14*0.0001; tp := price + 41*0.0001
		if sell { dir = "SELL"; sl = price + 14*0.0001; tp = price - 41*0.0001 }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -14; break }; if cc.High >= tp { result = "win"; pips = 41; break } } else { if cc.High >= sl { result = "loss"; pips = -14; break }; if cc.Low <= tp { result = "win"; pips = 41; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("BB+RSI 1:2.93", log)
}

func testBBADX(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 30 { continue }
		adx := calcADX(hist, 14); if adx >= 25 { continue }
		mean, std := meanStd(hist, 20)
		upper := mean + 2.0*std; lower := mean - 2.0*std
		if price < lower || price > upper {
			dir := "BUY"; sl := price - 14*0.0001; tp := price + 41*0.0001
			if price > upper { dir = "SELL"; sl = price + 14*0.0001; tp = price - 41*0.0001 }
			result := "loss"; var pips float64
			for j := i + 1; j < min(i+48, len(candles)); j++ {
				cc := candles[j]
				if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -14; break }; if cc.High >= tp { result = "win"; pips = 41; break } } else { if cc.High >= sl { result = "loss"; pips = -14; break }; if cc.Low <= tp { result = "win"; pips = 41; break } }
			}
			log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
		}
	}
	printStats("BB+ADX<25 1:2.93", log)
}

func testBBBest(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 30 { continue }
		adx := calcADX(hist, 14); if adx >= 25 { continue }
		mean, std := meanStd(hist, 20); rsi := calcRSI(hist, 14)
		upper := mean + 2.0*std; lower := mean - 2.0*std
		buy := price < lower && rsi < 30
		sell := price > upper && rsi > 70
		if !buy && !sell { continue }
		dir := "BUY"; sl := price - 14*0.0001; tp := price + 41*0.0001
		if sell { dir = "SELL"; sl = price + 14*0.0001; tp = price - 41*0.0001 }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -14; break }; if cc.High >= tp { result = "win"; pips = 41; break } } else { if cc.High >= sl { result = "loss"; pips = -14; break }; if cc.Low <= tp { result = "win"; pips = 41; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("BB+RSI+ADX 1:2.93", log)
}

func testTPCS_RSI65(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50)
		eShort := calcEMA(hist, 20)
		ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		adx := calcADX(hist, 14)
		if adx < 25 { continue }
		if rsi < 65 { continue }
		prevClose := hist[len(hist)-2]
		pip := 0.0001
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull
		backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 65
		sellCond := bearish && pullZoneDn && backBelow && rsi < 35
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS RSI>65 1:2", log)
}

func testMeanRev1to2(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }
		mean, std := meanStd(hist, 20)
		rsi := calcRSI(hist, 14)
		adx := calcADX(hist, 14)
		if adx >= 25 { continue }
		upper := mean + 2.0*std
		lower := mean - 2.0*std
		buy := rsi < 35 && price <= lower
		sell := rsi > 65 && price >= upper
		if !buy && !sell { continue }
		slDist := std * 1.5
		tpDist := std * 3.0
		dir := "BUY"
		var slPrice, tpPrice float64
		if buy {
			dir = "BUY"; slPrice = price - slDist; tpPrice = price + tpDist
		} else {
			dir = "SELL"; slPrice = price + slDist; tpPrice = price - tpDist
		}
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }; if cc.High >= tpPrice { result = "win"; pips = tpDist / 0.0001; break } } else { if cc.High >= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }; if cc.Low <= tpPrice { result = "win"; pips = tpDist / 0.0001; break } }
		}
		log = append(log, trade{c.Time, dir, price, slPrice, tpPrice, result, pips})
	}
	printStats("MeanRev BB+RSI+ADX<25 1:2", log)
}

func testBBRaw1to2(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 25 { continue }
		mean, std := meanStd(hist, 20)
		upper := mean + 2.0*std
		lower := mean - 2.0*std
		if price < lower || price > upper {
			dir := "BUY"; sl := price - 14*0.0001; tp := price + 28*0.0001
			if price > upper { dir = "SELL"; sl = price + 14*0.0001; tp = price - 28*0.0001 }
			result := "loss"; var pips float64
			for j := i + 1; j < min(i+48, len(candles)); j++ {
				cc := candles[j]
				if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -14; break }; if cc.High >= tp { result = "win"; pips = 28; break } } else { if cc.High >= sl { result = "loss"; pips = -14; break }; if cc.Low <= tp { result = "win"; pips = 28; break } }
			}
			log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
		}
	}
	printStats("BB raw touch 1:2", log)
}

func testTPCS_ADX30_1to2(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		adx := calcADX(hist, 14)
		if adx < 30 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14)
		prevClose := hist[len(hist)-2]; pip := 0.0001
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS ADX>30 1:2", log)
}

func testTPCS_NoRSI(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]
		price := closes[i]
		c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50)
		eShort := calcEMA(hist, 20)
		ePull := calcEMA(hist, 10)
		adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		pip := 0.0001
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove
		sellCond := bearish && pullZoneDn && backBelow
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS NoRSI 1:2", log)
}

func testTPCSVar(candles []Candle, closes []float64, tpPips float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + tpPips*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - tpPips*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = tpPips; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = tpPips; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats(fmt.Sprintf("TPCS TP=%.0f", tpPips), log)
}

func testPullbackOnly(candles []Candle, closes []float64, tpPips float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong; bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove
		sellCond := bearish && pullZoneDn && backBelow
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + tpPips*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - tpPips*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = tpPips; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = tpPips; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats(fmt.Sprintf("Pullback TP=%.0f", tpPips), log)
}

func testEMACross(candles []Candle, closes []float64, tpPips float64) {
	var log []trade
	pip := 0.0001
	slPips := 10.0
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20)
		prevE := calcEMA(closes[:i], 20)
		prevL := calcEMA(closes[:i], 50)
		buyCross := eShort > eLong && prevE <= prevL
		sellCross := eShort < eLong && prevE >= prevL
		if !buyCross && !sellCross { continue }
		dir := "BUY"; sl := price - slPips*pip; tp := price + tpPips*pip
		if sellCross { dir = "SELL"; sl = price + slPips*pip; tp = price - tpPips*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -slPips; break }; if cc.High >= tp { result = "win"; pips = tpPips; break } } else { if cc.High >= sl { result = "loss"; pips = -slPips; break }; if cc.Low <= tp { result = "win"; pips = tpPips; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats(fmt.Sprintf("EMACross TP=%.0f", tpPips), log)
}

func testTPCS_Slope(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 80 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		eLongPrev := calcEMA(closes[:i], 50)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		slopeUp := eLong > eLongPrev*1.00001
		slopeDn := eLong < eLongPrev*0.99999
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong && slopeUp
		bearish := eShort < eLong && slopeDn
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS+slope 1:2", log)
}

func testTPCS_Sustained(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 95; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 95 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		above := 0; below := 0
		for k := i - 20; k <= i; k++ { if closes[k] > calcEMA(closes[:k+1], 50) { above++ } else { below++ } }
		bullish := eShort > eLong && above > below
		bearish := eShort < eLong && below > above
		prevClose := hist[len(hist)-2]
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS+sustained 1:2", log)
}

func testTPCS_H4Proxy(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		aligned := 0
		for k := i - 3; k <= i; k++ {
			e50 := calcEMA(closes[:k+1], 50)
			e20 := calcEMA(closes[:k+1], 20)
			if e20 > e50 { aligned++ } else { aligned-- }
		}
		bullish := eShort > eLong && aligned >= 2
		bearish := eShort < eLong && aligned <= -2
		prevClose := hist[len(hist)-2]
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS+H4proxy 1:2", log)
}

func testTPCS_Stronger(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-3*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+3*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 60
		sellCond := bearish && pullZoneDn && backBelow && rsi < 50
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS+strong 1:2", log)
}

func testTPCS_SlopeADX30(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 80 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		eLongPrev := calcEMA(closes[:i], 50)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 30 { continue }
		slopeUp := eLong > eLongPrev*1.00001
		slopeDn := eLong < eLongPrev*0.99999
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong && slopeUp
		bearish := eShort < eLong && slopeDn
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS+slope+ADX30 1:2", log)
}

func testEMA10Touch(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		eLongPrev := calcEMA(closes[:i], 50)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		slopeUp := eLong > eLongPrev*1.00001
		slopeDn := eLong < eLongPrev*0.99999
		prevClose := hist[len(hist)-2]
		retestUp := prevClose <= ePull+2*pip && prevClose >= ePull-2*pip
		retestDn := prevClose >= ePull-2*pip && prevClose <= ePull+2*pip
		buyCond := eShort > eLong && slopeUp && retestUp && price > ePull && rsi > 60
		sellCond := eShort < eLong && slopeDn && retestDn && price < ePull && rsi < 50
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("EMA10touch 1:2", log)
}

func testRSIContinuation(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 30 { continue }
		buyCond := price > eShort && eShort > eLong && rsi > 70
		sellCond := price < eShort && eShort < eLong && rsi < 30
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("RSI>70cont 1:2", log)
}

func testCrossRetest(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20)
		eLongPrev := calcEMA(closes[:i], 50); eShortPrev := calcEMA(closes[:i], 20)
		buyCross := eShort > eLong && eShortPrev <= eLongPrev
		sellCross := eShort < eLong && eShortPrev >= eLongPrev
		if buyCross {
			// wait for price to retest EMA20
			for k := i + 1; k < min(i+6, len(candles)); k++ {
				if closes[k] <= calcEMA(closes[:k+1], 20) || candles[k].Low <= calcEMA(closes[:k+1], 20) {
					entryPrice := closes[k]; ec := candles[k]
					if ec.Time.Hour() < 8 || ec.Time.Hour() >= 20 { break }
					sl := entryPrice - 12*pip; tp := entryPrice + 24*pip
					result := "loss"; var pips float64
					for j := k + 1; j < min(k+48, len(candles)); j++ {
						cc := candles[j]
						if cc.Low <= sl { result = "loss"; pips = -12; break }
						if cc.High >= tp { result = "win"; pips = 24; break }
					}
					log = append(log, trade{ec.Time, "BUY", entryPrice, sl, tp, result, pips})
					break
				}
			}
		}
		if sellCross {
			for k := i + 1; k < min(i+6, len(candles)); k++ {
				if closes[k] >= calcEMA(closes[:k+1], 20) || candles[k].High >= calcEMA(closes[:k+1], 20) {
					entryPrice := closes[k]; ec := candles[k]
					if ec.Time.Hour() < 8 || ec.Time.Hour() >= 20 { break }
					sl := entryPrice + 12*pip; tp := entryPrice - 24*pip
					result := "loss"; var pips float64
					for j := k + 1; j < min(k+48, len(candles)); j++ {
						cc := candles[j]
						if cc.High >= sl { result = "loss"; pips = -12; break }
						if cc.Low <= tp { result = "win"; pips = 24; break }
					}
					log = append(log, trade{ec.Time, "SELL", entryPrice, sl, tp, result, pips})
					break
				}
			}
		}
	}
	printStats("Cross+retest 1:2", log)
}

func testTrendCont(candles []Candle, closes []float64, tpPips float64) {
	var log []trade
	pip := 0.0001
	slPips := 10.0
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		bullTrend := eShort > eLong && rsi > 60
		bearTrend := eShort < eLong && rsi < 40
		if !bullTrend && !bearTrend { continue }
		dir := "BUY"; sl := price - slPips*pip; tp := price + tpPips*pip
		if bearTrend { dir = "SELL"; sl = price + slPips*pip; tp = price - tpPips*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -slPips; break }; if cc.High >= tp { result = "win"; pips = tpPips; break } } else { if cc.High >= sl { result = "loss"; pips = -slPips; break }; if cc.Low <= tp { result = "win"; pips = tpPips; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats(fmt.Sprintf("TrendCont RSI>60 TP=%.0f", tpPips), log)
}

func printStats(name string, log []trade) {
	if len(log) == 0 { fmt.Println("  NO SIGNALS"); return }
	wins, losses := 0, 0
	var netP, winP, lossP float64
	cl, mcl := 0, 0
	for _, t := range log {
		if t.result == "win" { wins++; winP += t.pips; cl = 0 } else { losses++; lossP -= t.pips; cl++; if cl > mcl { mcl = cl } }
		netP += t.pips
	}
	wr := float64(wins) / float64(len(log)) * 100
	avgW := winP / float64(wins)
	avgL := lossP / float64(losses)
	pf := winP / math.Max(lossP, 0.0001)
	ev := (wr/100)*avgW - (1-wr/100)*avgL
	fmt.Printf("  Trades: %d | Wins: %d (%.0f%%) | Losses: %d\n", len(log), wins, wr, losses)
	fmt.Printf("  Net Pips: %.1f | PF: %.2f | EV: %.1f pip | MaxCL: %d\n", netP, pf, ev, mcl)
	fmt.Printf("  Avg Win: %.1f | Avg Loss: %.1f\n\n", avgW, avgL)
}

func fetchH1Data() []Candle {
	now := time.Now().UTC()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/EURUSD=X?period1=%d&period2=%d&interval=1h", start.Unix(), now.Unix())
	client := &http.Client{Timeout: 60 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil { fmt.Fprintf(os.Stderr, "GET error: %v", err); os.Exit(1) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var chart yahooResp
	json.Unmarshal(body, &chart)
	r := chart.Chart.Result[0]
	times := r.Timestamp
	q := r.Indicators.Quote[0]
	var candles []Candle
	for i := 0; i < min(len(times), len(q.Open)); i++ {
		if math.IsNaN(q.Open[i]) || q.Open[i] == 0 { continue }
		candles = append(candles, Candle{time.Unix(times[i], 0).UTC(), q.Open[i], q.High[i], q.Low[i], q.Close[i]})
	}
	return candles
}

func meanStd(data []float64, period int) (float64, float64) {
	if len(data) < period { period = len(data) }
	start := len(data) - period
	var sum float64
	for i := start; i < len(data); i++ { sum += data[i] }
	mean := sum / float64(period)
	var sq float64
	for i := start; i < len(data); i++ { d := data[i] - mean; sq += d * d }
	return mean, math.Sqrt(sq / float64(period))
}

func calcEMA(data []float64, period int) float64 {
	if len(data) < period { period = len(data) }
	m := 2.0 / float64(period+1)
	start := len(data) - period
	var ema float64
	for i := start; i < len(data); i++ {
		if i == start { ema = data[i] } else { ema = (data[i]-ema)*m + ema }
	}
	return ema
}

func calcRSI(data []float64, period int) float64 {
	if len(data) < period+1 { return 50 }
	start := len(data) - period - 1
	var g, l float64
	for i := start + 1; i < len(data); i++ {
		d := data[i] - data[i-1]
		if d > 0 { g += d } else { l -= d }
	}
	ag := g / float64(period)
	al := l / float64(period)
	if al == 0 { return 100 }
	return 100 - (100 / (1 + ag/al))
}

func calcADX(data []float64, period int) float64 {
	if len(data) < period*2+1 { return 0 }
	n := period
	start := len(data) - n*2
	if start < 0 { start = 0 }
	p, ng, tr := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		idx := start + i + 1
		if idx >= len(data) { break }
		up := data[idx] - data[idx-1]
		dn := data[idx-1] - data[idx]
		if up > dn && up > 0 { p[i] = up } else if dn > up && dn > 0 { ng[i] = dn }
		tr[i] = math.Abs(data[idx] - data[idx-1])
	}
	ep, en, et := p[0], ng[0], tr[0]
	k := 2.0 / float64(period+1)
	for i := 1; i < n; i++ {
		ep = (p[i]-ep)*k + ep
		en = (ng[i]-en)*k + en
		et = (tr[i]-et)*k + et
	}
 if et == 0 { return 0 }
 return math.Abs(100*ep/et-100*en/et) / (100*ep/et + 100*en/et) * 100
}

func min(a, b int) int {
 if a < b { return a }; return b
}

func calcMACD(prices []float64, fast, slow, signal int) ([]float64, []float64, []float64) {
 n := len(prices)
 fastEMA := make([]float64, n)
 slowEMA := make([]float64, n)
 m := 2.0 / float64(fast+1)
 ema := prices[0]
 for i := 0; i < n; i++ {
  if i == 0 { ema = prices[i] } else { ema = (prices[i]-ema)*m + ema }
  fastEMA[i] = ema
 }
 m = 2.0 / float64(slow+1)
 ema = prices[0]
 for i := 0; i < n; i++ {
  if i == 0 { ema = prices[i] } else { ema = (prices[i]-ema)*m + ema }
  slowEMA[i] = ema
 }
 macdLine := make([]float64, n)
 for i := range prices {
  macdLine[i] = fastEMA[i] - slowEMA[i]
 }
 signalLine := make([]float64, n)
 m = 2.0 / float64(signal+1)
 ema = macdLine[0]
 for i := 0; i < n; i++ {
  if i == 0 { ema = macdLine[i] } else { ema = (macdLine[i]-ema)*m + ema }
  signalLine[i] = ema
 }
 hist := make([]float64, n)
 for i := range macdLine {
  hist[i] = macdLine[i] - signalLine[i]
 }
 return macdLine, signalLine, hist
}

func testMACD_Cross(candles []Candle, closes []float64, slPips, tpPips float64) {
 var log []trade
 pip := 0.0001
 for i := 80; i < len(candles); i++ {
  hist := closes[:i+1]
  price := closes[i]
  c := candles[i]
  if len(hist) < 80 { continue }
  macd, signal, histVals := calcMACD(hist, 12, 26, 9)
  n := len(macd)
  if n < 2 { continue }
  currMACD := macd[n-1]
  currSignal := signal[n-1]
  currHist := histVals[n-1]
  prevMACD := macd[n-2]
  bullish := currMACD > currSignal && currHist > 0 && currMACD > prevMACD
  bearish := currMACD < currSignal && currHist < 0 && currMACD < prevMACD
  if !bullish && !bearish { continue }
  dir := "BUY"
  sl := price - slPips*pip
  tp := price + tpPips*pip
  if bearish {
   dir = "SELL"
   sl = price + slPips*pip
   tp = price - tpPips*pip
  }
  result := "loss"
  var pips float64
  for j := i + 1; j < min(i+48, len(candles)); j++ {
   cc := candles[j]
   if dir == "BUY" {
    if cc.Low <= sl { result = "loss"; pips = -slPips; break }
    if cc.High >= tp { result = "win"; pips = tpPips; break }
   } else {
    if cc.High >= sl { result = "loss"; pips = -slPips; break }
    if cc.Low <= tp { result = "win"; pips = tpPips; break }
   }
  }
  log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
 }
 printStats(fmt.Sprintf("MACD cross SL=%.0f TP=%.0f", slPips, tpPips), log)
}

func testMACD_ADX(candles []Candle, closes []float64) {
 var log []trade
 pip := 0.0001
 for i := 80; i < len(candles); i++ {
  hist := closes[:i+1]
  price := closes[i]
  c := candles[i]
  if len(hist) < 80 { continue }
  macd, signal, histVals := calcMACD(hist, 12, 26, 9)
  n := len(macd)
  if n < 2 { continue }
  currMACD := macd[n-1]
  currSignal := signal[n-1]
  currHist := histVals[n-1]
  prevMACD := macd[n-2]
  bullish := currMACD > currSignal && currHist > 0 && currMACD > prevMACD
  bearish := currMACD < currSignal && currHist < 0 && currMACD < prevMACD
  if !bullish && !bearish { continue }
  adx := calcADX(hist, 14)
  if adx >= 25 { continue }
  dir := "BUY"
  sl := price - 12*pip
  tp := price + 24*pip
  if bearish {
   dir = "SELL"
   sl = price + 12*pip
   tp = price - 24*pip
  }
  result := "loss"
  var pips float64
  for j := i + 1; j < min(i+48, len(candles)); j++ {
   cc := candles[j]
   if dir == "BUY" {
    if cc.Low <= sl { result = "loss"; pips = -12; break }
    if cc.High >= tp { result = "win"; pips = 24; break }
   } else {
    if cc.High >= sl { result = "loss"; pips = -12; break }
    if cc.Low <= tp { result = "win"; pips = 24; break }
   }
  }
  log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
 }
 printStats("MACD + ADX<25 1:2", log)
}

func testMACD_RSI(candles []Candle, closes []float64) {
 var log []trade
 pip := 0.0001
 for i := 80; i < len(candles); i++ {
  hist := closes[:i+1]
  price := closes[i]
  c := candles[i]
  if len(hist) < 80 { continue }
  macd, signal, histVals := calcMACD(hist, 12, 26, 9)
  n := len(macd)
  if n < 2 { continue }
  currMACD := macd[n-1]
  currSignal := signal[n-1]
  currHist := histVals[n-1]
  prevMACD := macd[n-2]
  bullish := currMACD > currSignal && currHist > 0 && currMACD > prevMACD
  bearish := currMACD < currSignal && currHist < 0 && currMACD < prevMACD
  if !bullish && !bearish { continue }
  rsi := calcRSI(hist, 14)
  if bullish && rsi <= 50 { continue }
  if bearish && rsi >= 50 { continue }
  dir := "BUY"
  sl := price - 12*pip
  tp := price + 24*pip
  if bearish {
   dir = "SELL"
   sl = price + 12*pip
   tp = price - 24*pip
  }
  result := "loss"
  var pips float64
  for j := i + 1; j < min(i+48, len(candles)); j++ {
   cc := candles[j]
   if dir == "BUY" {
    if cc.Low <= sl { result = "loss"; pips = -12; break }
    if cc.High >= tp { result = "win"; pips = 24; break }
   } else {
    if cc.High >= sl { result = "loss"; pips = -12; break }
    if cc.Low <= tp { result = "win"; pips = 24; break }
   }
  }
  log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
 }
 printStats("MACD + RSI 1:2", log)
}

func testMACD_Momentum(candles []Candle, closes []float64) {
 var log []trade
 pip := 0.0001
 for i := 80; i < len(candles); i++ {
  hist := closes[:i+1]
  price := closes[i]
  c := candles[i]
  if len(hist) < 80 { continue }
  macd, signal, histVals := calcMACD(hist, 12, 26, 9)
  n := len(macd)
  if n < 2 { continue }
  currMACD := macd[n-1]
  currSignal := signal[n-1]
  currHist := histVals[n-1]
  prevMACD := macd[n-2]
  bullish := currMACD > currSignal && currHist > 0 && currMACD > prevMACD
  bearish := currMACD < currSignal && currHist < 0 && currMACD < prevMACD
  if !bullish && !bearish { continue }
  adx := calcADX(hist, 14)
  if adx >= 25 { continue }
  dir := "BUY"
  sl := price - 12*pip
  tp := price + 24*pip
  if bearish {
   dir = "SELL"
   sl = price + 12*pip
   tp = price - 24*pip
  }
  result := "loss"
  var pips float64
  for j := i + 1; j < min(i+48, len(candles)); j++ {
   cc := candles[j]
   if dir == "BUY" {
    if cc.Low <= sl { result = "loss"; pips = -12; break }
    if cc.High >= tp { result = "win"; pips = 24; break }
   } else {
    if cc.High >= sl { result = "loss"; pips = -12; break }
    if cc.Low <= tp { result = "win"; pips = 24; break }
   }
  }
  log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
 }
  printStats("MACD + hist slope + ADX<25 1:2", log)
}

func testTPCS_Asian(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 0 || c.Time.Hour() >= 8 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS Asian 1:2", log)
}

func testCounterTrendBB(candles []Candle, closes []float64) {
	var log []trade
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		mean, std := meanStd(hist, 20)
		rsi := calcRSI(hist, 14)
		upper := mean + 2.0*std
		lower := mean - 2.0*std
		buy := price <= lower && rsi < 40
		sell := price >= upper && rsi > 60
		if !buy && !sell { continue }
		slDist := std * 1.5
		tpDist := slDist * 2.0
		dir := "BUY"
		var slPrice, tpPrice float64
		if buy {
			dir = "BUY"; slPrice = price - slDist; tpPrice = price + tpDist
		} else {
			dir = "SELL"; slPrice = price + slDist; tpPrice = price - tpDist
		}
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }; if cc.High >= tpPrice { result = "win"; pips = tpDist / 0.0001; break } } else { if cc.High >= slPrice { result = "loss"; pips = -(slDist / 0.0001); break }; if cc.Low <= tpPrice { result = "win"; pips = tpDist / 0.0001; break } }
		}
		log = append(log, trade{c.Time, dir, price, slPrice, tpPrice, result, pips})
	}
	printStats("Counter-trend BB fade 1:2", log)
}

func testConsolBreakout(candles []Candle, closes []float64) {
	var log []trade
	consolBars := 5
	consolRange := 0.0010
	for i := 80; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 80 { continue }
		eShort := calcEMA(hist, 20)
		rsi := calcRSI(hist, 14)
		rangeHigh := price; rangeLow := price
		for k := i - consolBars; k < i; k++ {
			if candles[k].High > rangeHigh { rangeHigh = candles[k].High }
			if candles[k].Low < rangeLow { rangeLow = candles[k].Low }
		}
		if (rangeHigh - rangeLow) > consolRange { continue }
		buyCond := price > rangeHigh && rsi > 55 && price > eShort
		sellCond := price < rangeLow && rsi < 45 && price < eShort
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*0.0001; tp := price + 24*0.0001
		if sellCond { dir = "SELL"; sl = price + 12*0.0001; tp = price - 24*0.0001 }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("Consol breakout 1:2", log)
}

func testTPCS_Daily(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"; sl := price - 12*pip; tp := price + 24*pip
		if sellCond { dir = "SELL"; sl = price + 12*pip; tp = price - 24*pip }
		result := "loss"; var pips float64
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" { if cc.Low <= sl { result = "loss"; pips = -12; break }; if cc.High >= tp { result = "win"; pips = 24; break } } else { if cc.High >= sl { result = "loss"; pips = -12; break }; if cc.Low <= tp { result = "win"; pips = 24; break } }
		}
		log = append(log, trade{c.Time, dir, price, sl, tp, result, pips})
	}
	printStats("TPCS D1 filter 1:2", log)
}

func testTPCS_PartialTP(candles []Candle, closes []float64) {
	var log []trade
	pip := 0.0001
	tp1 := 12.0; tp2 := 24.0; slDist := 12.0
	for i := 75; i < len(candles); i++ {
		hist := closes[:i+1]; price := closes[i]; c := candles[i]
		if c.Time.Hour() < 8 || c.Time.Hour() >= 20 { continue }
		if len(hist) < 75 { continue }
		eLong := calcEMA(hist, 50); eShort := calcEMA(hist, 20); ePull := calcEMA(hist, 10)
		rsi := calcRSI(hist, 14); adx := calcADX(hist, 14)
		if adx < 25 { continue }
		prevClose := hist[len(hist)-2]
		bullish := eShort > eLong
		bearish := eShort < eLong
		pullZone := prevClose >= ePull-10*pip && prevClose <= ePull+3*pip
		pullZoneDn := prevClose <= ePull+10*pip && prevClose >= ePull-3*pip
		backAbove := price > ePull; backBelow := price < ePull
		buyCond := bullish && pullZone && backAbove && rsi > 55
		sellCond := bearish && pullZoneDn && backBelow && rsi < 55
		if !buyCond && !sellCond { continue }
		dir := "BUY"
		var slP, tp1P, tp2P float64
		if buyCond {
			dir = "BUY"; slP = price - slDist*pip; tp1P = price + tp1*pip; tp2P = price + tp2*pip
		} else {
			dir = "SELL"; slP = price + slDist*pip; tp1P = price - tp1*pip; tp2P = price - tp2*pip
		}
		result := "loss"; var pips float64
		hitTP1 := false
		for j := i + 1; j < min(i+48, len(candles)); j++ {
			cc := candles[j]
			if dir == "BUY" {
				if cc.Low <= slP { result = "loss"; pips = -slDist; break }
				if !hitTP1 && cc.High >= tp1P { hitTP1 = true; pips = tp1; continue }
				if hitTP1 && cc.High >= tp2P { result = "win"; pips = tp2; break }
			} else {
				if cc.High >= slP { result = "loss"; pips = -slDist; break }
				if !hitTP1 && cc.Low <= tp1P { hitTP1 = true; pips = tp1; continue }
				if hitTP1 && cc.Low <= tp2P { result = "win"; pips = tp2; break }
			}
		}
		log = append(log, trade{c.Time, dir, price, slP, tp2P, result, pips})
	}
	printStats("TPCS partial TP 1:2", log)
}
