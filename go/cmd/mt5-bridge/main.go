package main

import (
 "encoding/json"
 "fmt"
 "log"
 "math"
 "net/http"
 "sync"
)

const (
 FixedLotSize    = 0.01
 StopLossPips    = 14.0
 TakeProfitPips  = 41.0
 BBPeriod        = 20
 BBStdDev        = 2.0
 RSIPeriod       = 14
 RSIOverbought   = 70
 RSIOversold     = 30
 ADXPeriod       = 14
 ADXRangeMax     = 25
 MaxCandleLookback = 48
)

type MarketPayload struct {
 Symbol string    `json:"symbol"`
 Prices []float64 `json:"prices"`
}

type TradeResponse struct {
 Action       string  `json:"action"`
 Lot          float64 `json:"lot"`
 StopLossPips float64 `json:"sl_pips"`
 TakeProfitPips float64 `json:"tp_pips"`
}

type BotEngine struct {
 mu sync.Mutex
}

func meanStd(prices []float64, period int) (float64, float64) {
 if len(prices) < period {
  return 0, 0
 }
 start := len(prices) - period
 sum := 0.0
 for i := start; i < len(prices); i++ {
  sum += prices[i]
 }
 mean := sum / float64(period)
 var sq float64
 for i := start; i < len(prices); i++ {
  d := prices[i] - mean
  sq += d * d
 }
 return mean, math.Sqrt(sq / float64(period))
}

func calcRSI(prices []float64, period int) float64 {
 if len(prices) < period+1 {
  return 50
 }
 start := len(prices) - period - 1
 if start < 0 {
  start = 0
 }
 var gain, loss float64
 for i := start + 1; i < len(prices); i++ {
  d := prices[i] - prices[i-1]
  if d > 0 {
   gain += d
  } else {
   loss -= d
  }
 }
 avgGain := gain / float64(period)
 avgLoss := loss / float64(period)
 if avgLoss == 0 {
  return 100
 }
 rs := avgGain / avgLoss
 return 100 - (100 / (1 + rs))
}

func calcADX(prices []float64, period int) float64 {
 if len(prices) < period*2+1 {
  return 0
 }
 n := period
 start := len(prices) - n*2
 if start < 0 {
  start = 0
 }
 p := make([]float64, n)
 ng := make([]float64, n)
 tr := make([]float64, n)
 for i := 0; i < n; i++ {
  idx := start + i + 1
  if idx >= len(prices) {
   break
  }
  up := prices[idx] - prices[idx-1]
  dn := prices[idx-1] - prices[idx]
  if up > dn && up > 0 {
   p[i] = up
  } else if dn > up && dn > 0 {
   ng[i] = dn
  }
  tr[i] = math.Abs(prices[idx] - prices[idx-1])
 }
 ep, en, et := p[0], ng[0], tr[0]
 k := 2.0 / float64(period+1)
 for i := 1; i < n; i++ {
  ep = (p[i]-ep)*k + ep
  en = (ng[i]-en)*k + en
  et = (tr[i]-et)*k + et
 }
 if et == 0 {
  return 0
 }
 pdi := 100 * ep / et
 ndi := 100 * en / et
 if pdi+ndi == 0 {
  return 0
 }
 return math.Abs(pdi-ndi) / (pdi + ndi) * 100
}

func evaluateBollingerBands(prices []float64) (float64, float64, float64, error) {
 if len(prices) < BBPeriod {
  return 0, 0, 0, fmt.Errorf("insufficient price data: got %d, required %d", len(prices), BBPeriod)
 }
 sma, std := meanStd(prices, BBPeriod)
 upperBand := sma + (std * BBStdDev)
 lowerBand := sma - (std * BBStdDev)
 return upperBand, sma, lowerBand, nil
}

func (b *BotEngine) HandleTick(w http.ResponseWriter, r *http.Request) {
 b.mu.Lock()
 defer b.mu.Unlock()

 if r.Method != http.MethodPost {
  http.Error(w, "Invalid request method, use POST", http.StatusMethodNotAllowed)
  return
 }

 var payload MarketPayload
 if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
  http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
  return
 }

 prices := payload.Prices
 if len(prices) < 50 {
  http.Error(w, fmt.Sprintf("need at least 50 prices, got %d", len(prices)), http.StatusBadRequest)
  return
 }

 upper, _, lower, err := evaluateBollingerBands(prices)
 if err != nil {
  http.Error(w, err.Error(), http.StatusBadRequest)
  return
 }

 currentPrice := prices[len(prices)-1]
 response := TradeResponse{
  Action:         "HOLD",
  Lot:            FixedLotSize,
  StopLossPips:   StopLossPips,
  TakeProfitPips: TakeProfitPips,
 }

 adx := calcADX(prices, ADXPeriod)
 rsi := calcRSI(prices, RSIPeriod)

 if adx < ADXRangeMax && rsi < RSIOverbought && currentPrice <= lower {
  response.Action = "BUY"
  log.Printf("[SIGNAL] %s BUY @ %.5f (BB lower=%.5f, RSI=%.1f, ADX=%.1f) SL=%.1f TP=%.1f",
   payload.Symbol, currentPrice, lower, rsi, adx, StopLossPips, TakeProfitPips)
 } else if adx < ADXRangeMax && rsi > RSIOverbought && currentPrice >= upper {
  response.Action = "SELL"
  log.Printf("[SIGNAL] %s SELL @ %.5f (BB upper=%.5f, RSI=%.1f, ADX=%.1f) SL=%.1f TP=%.1f",
   payload.Symbol, currentPrice, upper, rsi, adx, StopLossPips, TakeProfitPips)
 }

 if response.Action == "HOLD" {
  log.Printf("[HOLD] %s @ %.5f (BB lower=%.5f upper=%.5f, RSI=%.1f, ADX=%.1f)",
   payload.Symbol, currentPrice, lower, upper, rsi, adx)
 }

 w.Header().Set("Content-Type", "application/json")
 json.NewEncoder(w).Encode(response)
}

func main() {
 bot := &BotEngine{}

 mux := http.NewServeMux()
 mux.HandleFunc("/process-tick", bot.HandleTick)

 port := ":8080"
 log.Printf("BB MeanRev+RSI+ADX MT5 Bridge started on port %s", port)
 log.Printf("Filters: BB(20,2) + RSI<30/>70 + ADX<25 | SL=14 TP=41 (1:2.93 R:R)")

 if err := http.ListenAndServe(port, mux); err != nil {
  log.Fatalf("Server crashed: %v", err)
 }
}
