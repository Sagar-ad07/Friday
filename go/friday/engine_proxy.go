package friday

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// Engine Proxy Helpers (used by trading/crypto/bot tools)
// ──────────────────────────────────────────────────────────────────────

var engineBase = getEngineBase()

func getEngineBase() string {
	if v := os.Getenv("ENGINE_URL"); v != "" {
		return v
	}
	port := os.Getenv("TRADING_ENGINE_PORT")
	if port == "" {
		port = "8001"
	}
	return "http://localhost:" + port
}

func engineGet(path string) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(engineBase + path)
	if err != nil {
		return nil, fmt.Errorf("engine unreachable: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("engine decode: %w", err)
	}
	return result, nil
}

func enginePost(path string, body any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(engineBase+path, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("engine unreachable: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("engine decode: %w", err)
	}
	return result, nil
}

// ──────────────────────────────────────────────────────────────────────
// Real Math Expression Evaluator (safe, no external deps)
// ──────────────────────────────────────────────────────────────────────

func evalMath(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)
	p := &mathParser{tokens: tokenizeMath(expr)}
	result := p.parseExpr()
	if p.err != nil {
		return 0, p.err
	}
	return result, nil
}

type mathToken struct {
	kind  byte   // 'n' number, '+' '-' '*' '/' '(' ')' '^'
	value float64
}

func tokenizeMath(s string) []mathToken {
	var tokens []mathToken
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		if c >= '0' && c <= '9' || c == '.' {
			start := i
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			val, _ := strconv.ParseFloat(s[start:i], 64)
			tokens = append(tokens, mathToken{'n', val})
			continue
		}
		if c == '*' && i+1 < len(s) && s[i+1] == '*' {
			tokens = append(tokens, mathToken{'^', 0})
			i += 2
			continue
		}
		if strings.ContainsRune("+-*/()^", rune(c)) {
			tokens = append(tokens, mathToken{c, 0})
			i++
			continue
		}
		return nil // invalid char
	}
	return tokens
}

type mathParser struct {
	tokens []mathToken
	pos    int
	err    error
}

func (p *mathParser) peek() mathToken {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return mathToken{0, 0}
}

func (p *mathParser) consume() mathToken {
	t := p.peek()
	p.pos++
	return t
}

func (p *mathParser) parseExpr() float64 {
	left := p.parseTerm()
	for p.peek().kind == '+' || p.peek().kind == '-' {
		op := p.consume().kind
		right := p.parseTerm()
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left
}

func (p *mathParser) parseTerm() float64 {
	left := p.parsePower()
	for p.peek().kind == '*' || p.peek().kind == '/' {
		op := p.consume().kind
		right := p.parsePower()
		if op == '*' {
			left *= right
		} else if right != 0 {
			left /= right
		} else {
			p.err = fmt.Errorf("division by zero")
			return 0
		}
	}
	return left
}

func (p *mathParser) parsePower() float64 {
	left := p.parseUnary()
	if p.peek().kind == '^' {
		p.consume()
		right := p.parseUnary()
		left = math.Pow(left, right)
	}
	return left
}

func (p *mathParser) parseUnary() float64 {
	if p.peek().kind == '+' {
		p.consume()
		return p.parseAtom()
	}
	if p.peek().kind == '-' {
		p.consume()
		return -p.parseAtom()
	}
	return p.parseAtom()
}

func (p *mathParser) parseAtom() float64 {
	if p.peek().kind == '(' {
		p.consume()
		val := p.parseExpr()
		if p.peek().kind == ')' {
			p.consume()
		}
		return val
	}
	if p.peek().kind == 'n' {
		return p.consume().value
	}
	p.err = fmt.Errorf("unexpected token")
	return 0
}