package friday

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ──────────────────────────────────────────────────────────────────────
// Manual Trade – human order-ticket UI + chat/voice proposal skill.
//
// This is NOT a bot. Friday analyzes, sizes and proposes a trade; the human
// reviews the ticket and clicks Buy/Sell to confirm. The order then goes to
// the configured execution gateway (defaults to the engine /mt5/order), which
// re-applies the prop-firm daily cap + TP clamp — so Friday can never fire a
// cap-breaching order, and the prop firm sees a normal manual deal.
//
// Decoupled from the always-on laptop MT5 bot loops: set MANUAL_GATEWAY_URL
// to point this skill at any order gateway accepting the same /mt5/order
// contract; set MANUAL_ANALYSIS_URL to a different analysis engine if needed.
// ──────────────────────────────────────────────────────────────────────

func webUIPath() string {
	candidates := []string{
		filepath.Join(ProjectRoot, "..", "webui"),
		filepath.Join(ProjectRoot, "webui"),
		filepath.Join(".", "webui"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			return c
		}
	}
	return candidates[0]
}

func floatOf(m map[string]any, k string) float64 {
	v, ok := m[k]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case float32:
		return float64(x)
	}
	return 0
}

// ManualTicket is the JSON the browser sends: one order, sized by a human.
type ManualTicket struct {
	Symbol string  `json:"symbol" binding:"required"`
	Type   string  `json:"type" binding:"required,oneof=buy sell"` // human click
	Volume float64 `json:"volume" binding:"required,gt=0"`         // lots, human-chosen
	SL     float64 `json:"sl"`                                     // stop loss price
	TP     float64 `json:"tp"`                                     // take profit price
}

func gatewayBase() string {
	if v := os.Getenv("MANUAL_GATEWAY_URL"); v != "" {
		return v
	}
	return engineBase
}

// ManualTradePage serves the deepchart + order ticket.
func (s *Server) ManualTradePage() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.File(filepath.Join(webUIPath(), "trade.html"))
	}
}

// ManualAccount proxies /mt5/account for the ticket's risk summary.
func (s *Server) ManualAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := engineGet("/mt5/account")
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution gateway unreachable", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// ManualTick proxies /mt5/tick/:symbol for the price ladder.
func (s *Server) ManualTick() gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := engineGet("/mt5/tick/" + c.Param("symbol"))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution gateway unreachable", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// ManualPositions proxies /mt5/positions so the human sees open trades.
func (s *Server) ManualPositions() gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := engineGet("/mt5/positions")
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution gateway unreachable", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// ManualOrder relays a human-placed order to the gateway, preserving the
// gateway's exact verdict (200 ok / 403 cap-blocked / 503 down). The order
// target is configurable via MANUAL_GATEWAY_URL so this manual skill is
// decoupled from the always-on MT5 bot loops.
func (s *Server) ManualOrder() gin.HandlerFunc {
	return func(c *gin.Context) {
		var t ManualTicket
		if err := c.ShouldBindJSON(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		body, _ := json.Marshal(t)

		url := gatewayBase() + "/mt5/order"
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution gateway unreachable", "detail": err.Error()})
			return
		}
		defer resp.Body.Close()
		payload, _ := io.ReadAll(resp.Body)

		if ct := resp.Header.Get("Content-Type"); ct != "" {
			c.Header("Content-Type", ct)
		}
		c.Status(resp.StatusCode)
		_, _ = c.Writer.Write(payload)
	}
}

// ManualPropose builds a human-sized, cap-aware trade proposal from the live
// price, account equity and the gateway's technical analysis. It is meant to
// be invoked by chat ("Friday propose a manual trade on EURUSDm") or voice;
// the result pre-fills /trade so the human only clicks to confirm. It never
// submits an order.
func (s *Server) ManualPropose() gin.HandlerFunc {
	return func(c *gin.Context) {
		var q struct {
			Symbol   string  `json:"symbol"`
			Side     string  `json:"side"`     // "" = auto from momentum
			RiskPct  float64 `json:"risk_pct"` // % of free equity risked, default 1
			SLPips   float64 `json:"sl_pips"`  // optional, default 50
		}
		if err := c.ShouldBindJSON(&q); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if q.Symbol == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
			return
		}
		if q.RiskPct <= 0 {
			q.RiskPct = 1.0
		}
		if q.SLPips <= 0 {
			q.SLPips = 50.0
		}

		// --- live price + account risk ---
		tick, _ := engineGet("/mt5/tick/" + q.Symbol)
		acct, _ := engineGet("/mt5/account")
		equity := floatOf(acct, "equity")
		margin := floatOf(acct, "margin")
		free := math.Max(0, equity-margin) // engine returns equity + margin

		// --- signal: momentum score from the analysis engine ---
		mom, _ := engineGet("/analysis/momentum/" + q.Symbol + "/H1")
		score := floatOf(mom, "score")
		conf := floatOf(mom, "confidence")
		side := q.Side
		if side == "" {
			if score > 0 {
				side = "buy"
			} else if score < 0 {
				side = "sell"
			}
		}

		// --- cap guard: mirror the bot's daily-profit-cap read ---
		st, _ := engineGet("/trading/status")
		cap := floatOf(st, "daily_profit_cap")
		if cap == 0 {
			cap = 37.5
		}
		realized := floatOf(st, "daily_pnl")
		capRemaining := cap - realized
		capBlocked := false
		if e, ok := st["last_error"]; ok {
			if s2, ok := e.(string); ok && strings.Contains(strings.ToLower(s2), "cap") {
				capBlocked = true
			}
		}

		// --- sizing: risk N% of free equity, 1:2 R:R, clamp to max-lot cap ---
		risk := (free * q.RiskPct) / 100.0
		pipValue := 0.10 // ~$ per pip per micro lot for majors; heuristic, review before confirming
		lot := risk / (q.SLPips * pipValue * 2)
		lot = math.Max(0.01, math.Round(math.Min(0.05, lot)*100)/100) // 0.01–0.05 lots, 2 dp

		// --- price + SL/TP from current tick ---
		point := 0.0001
		if int(floatOf(tick, "digits")) >= 3 {
			point = 0.01
		}
		var price float64
		if side == "buy" {
			price = floatOf(tick, "ask")
		} else if side == "sell" {
			price = floatOf(tick, "bid")
		}
		rr := 2.0
		var sl, tp float64
		if price > 0 && side != "" {
			if side == "buy" {
				sl = price - q.SLPips*point
				tp = price + q.SLPips*rr*point
			} else {
				sl = price + q.SLPips*point
				tp = price - q.SLPips*rr*point
			}
		}

		rationale := "no clear signal"
		switch {
		case side == "":
			rationale = "momentum neutral — no proposal"
		case capBlocked:
			rationale = "daily cap reached — trading blocked until tomorrow"
		case capRemaining <= 0:
			rationale = "near daily cap — consider closing first"
		default:
			rationale = "Friday view: momentum score " + num(score) + ", signal confidence " + num(conf*100) + "%, free " + num(free) + ", risk " + num(q.RiskPct) + "% → " + side
		}

		c.JSON(http.StatusOK, gin.H{
			"symbol":          q.Symbol,
			"side":            side,
			"lot":             lot,
			"price":           price,
			"sl":              sl,
			"tp":              tp,
			"sl_pips":         q.SLPips,
			"rrr":             "1:2",
			"risk_pct":        q.RiskPct,
			"cap_remaining":   capRemaining,
			"cap_blocked":     capBlocked,
			"realized_today":  realized,
			"free_equity":     free,
			"confidence":      conf,
			"rationale":       rationale,
			"analysis":        mom,
		})
	}
}

// num formats a float compactly (no trailing zeros).
func num(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', -1, 64), "0"), ".")
}

// ExecuteRustDeskTrade handles the confirmation and execution of RustDesk clicks for manual trades
func (s *Server) ExecuteRustDeskTrade() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Confirm string `json:"confirm" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "confirm field required: 'EXECUTE TRADE'"})
			return
		}

		// Execute the trade via RustDesk automation
		rustDeskTool := &RustDeskTool{}
		result, err := rustDeskTool.ExecuteConfirmed(req.Confirm)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to execute trade", "detail": err.Error()})
			return
		}

		c.JSON(http.StatusOK, result)
	}
}
 
 / /   E x e c u t e R u s t D e s k C o n t r a c t T r a d e   h a n d l e s   t h e   c o n f i r m a t i o n   a n d   e x e c u t i o n   o f   R u s t D e s k   c l i c k s   f o r   c o n t r a c t   t r a d e s  
 f u n c   ( s   * S e r v e r )   E x e c u t e R u s t D e s k C o n t r a c t T r a d e ( )   g i n . H a n d l e r F u n c   {  
 	 r e t u r n   f u n c ( c   * g i n . C o n t e x t )   {  
 	 	 v a r   r e q   s t r u c t   {  
 	 	 	 C o n f i r m   s t r i n g   ` j s o n : " c o n f i r m "   b i n d i n g : " r e q u i r e d " `  
 	 	 }  
 	 	 i f   e r r   : =   c . S h o u l d B i n d J S O N ( & r e q ) ;   e r r   ! =   n i l   {  
 	 	 	 c . J S O N ( h t t p . S t a t u s B a d R e q u e s t ,   g i n . H { " e r r o r " :   " c o n f i r m   f i e l d   r e q u i r e d :   ' E X E C U T E   T R A D E ' " } )  
 	 	 	 r e t u r n  
 	 	 }  
  
 	 	 r u s t D e s k C o n t r a c t T o o l   : =   & R u s t D e s k C o n t r a c t T o o l { }  
 	 	 r e s u l t ,   e r r   : =   r u s t D e s k C o n t r a c t T o o l . E x e c u t e C o n f i r m e d ( r e q . C o n f i r m )  
 	 	 i f   e r r   ! =   n i l   {  
 	 	 	 c . J S O N ( h t t p . S t a t u s I n t e r n a l S e r v e r E r r o r ,   g i n . H { " e r r o r " :   " f a i l e d   t o   e x e c u t e   t r a d e " ,   " d e t a i l " :   e r r . E r r o r ( ) } )  
 	 	 	 r e t u r n  
 	 	 }  
  
 	 	 c . J S O N ( h t t p . S t a t u s O K ,   r e s u l t )  
 	 }  
 }  
 