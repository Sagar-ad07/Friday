package db

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// resetForTest resets the package-level vars for testing
func resetForTest() {
	db = nil
	once = sync.Once{}
}

func TestInitAndGet(t *testing.T) {
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "friday_test.db")
	defer os.Remove(dbPath)
	defer Close()

	resetForTest()

	if err := Init(dbPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	d := Get()
	if d == nil {
		t.Fatal("expected non-nil DB after Init")
	}

	// Verify tables exist
	tables := []string{"memory_facts", "decisions", "accounts", "kv_store"}
	for _, table := range tables {
		var name string
		err := d.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("expected table %s to exist: %v", table, err)
		}
	}
}

func TestKVStore(t *testing.T) {
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "friday_test_kv.db")
	defer os.Remove(dbPath)
	defer Close()

	resetForTest()

	if err := Init(dbPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Set and get
	if err := KVSet("test_key", "test_value"); err != nil {
		t.Fatalf("KVSet failed: %v", err)
	}

	val, err := KVGet("test_key")
	if err != nil {
		t.Fatalf("KVGet failed: %v", err)
	}
	if val != "test_value" {
		t.Errorf("expected test_value, got %s", val)
	}

	// Update existing
	if err := KVSet("test_key", "updated_value"); err != nil {
		t.Fatalf("KVSet update failed: %v", err)
	}

	val, _ = KVGet("test_key")
	if val != "updated_value" {
		t.Errorf("expected updated_value, got %s", val)
	}

	// Get missing key
	val, _ = KVGet("nonexistent")
	if val != "" {
		t.Errorf("expected empty string for missing key, got %s", val)
	}
}

func TestMemoryFacts(t *testing.T) {
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "friday_test_mem.db")
	defer os.Remove(dbPath)
	defer Close()

	resetForTest()

	if err := Init(dbPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	d := Get()

	// Insert a fact
	result, err := d.Exec("INSERT INTO memory_facts (fact, type, verified, tags) VALUES (?, ?, ?, ?)",
		"Bitcoin is a cryptocurrency", "SEMANTIC", 1, "crypto,bitcoin")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	id, _ := result.LastInsertId()
	if id == 0 {
		t.Error("expected non-zero insert ID")
	}

	// Query via FTS
	rows, err := d.Query("SELECT f.fact, f.type, f.verified FROM memory_facts f JOIN memory_fts ON f.id = memory_fts.rowid WHERE memory_fts MATCH ?", "Bitcoin")
	if err != nil {
		t.Fatalf("FTS query failed: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var fact, memType string
		var verified int
		if err := rows.Scan(&fact, &memType, &verified); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if fact != "Bitcoin is a cryptocurrency" {
			t.Errorf("unexpected fact: %s", fact)
		}
		if memType != "SEMANTIC" {
			t.Errorf("unexpected type: %s", memType)
		}
		if verified != 1 {
			t.Error("expected verified=1")
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 FTS result, got %d", count)
	}
}

func TestDecisionsTable(t *testing.T) {
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "friday_test_dec.db")
	defer os.Remove(dbPath)
	defer Close()

	resetForTest()

	if err := Init(dbPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	d := Get()

	// Insert a decision
	_, err := d.Exec("INSERT INTO decisions (input, tool_calls, result, outcome, confidence) VALUES (?, ?, ?, ?, ?)",
		"What is BTC price?", `[{"tool":"crypto_price"}]`, "BTC is $50000", "correct", 0.9)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Query
	var input, result string
	var confidence float64
	err = d.QueryRow("SELECT input, result, confidence FROM decisions WHERE input LIKE ?", "%BTC%").Scan(&input, &result, &confidence)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if input != "What is BTC price?" {
		t.Errorf("unexpected input: %s", input)
	}
	if result != "BTC is $50000" {
		t.Errorf("unexpected result: %s", result)
	}
	if confidence != 0.9 {
		t.Errorf("unexpected confidence: %f", confidence)
	}
}
