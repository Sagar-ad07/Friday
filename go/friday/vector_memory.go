package friday

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/friday-prototype/friday-go/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────
// Vector Memory — semantic search via Ollama embeddings + SQLite
// ──────────────────────────────────────────────────────────────────────

const (
	embeddingDim    = 768
	embeddingModel  = "nomic-embed-text"
	embeddingBatch  = 32
	vectorTable     = "memory_vectors"
)

var (
	embedClient *EmbeddingClient
	embedOnce   sync.Once
)

// EmbeddingClient wraps Ollama's /api/embeddings endpoint
type EmbeddingClient struct {
	baseURL    string
	httpClient *http.Client
	model      string
	dim        int
}

// InitEmbeddings initializes the embedding client (call once at startup).
func InitEmbeddings(ollamaURL, model string) error {
	var err error
	embedOnce.Do(func() {
		if model == "" {
			model = embeddingModel
		}
		embedClient = &EmbeddingClient{
			baseURL: strings.TrimSuffix(ollamaURL, "/"),
			httpClient: &http.Client{
				Timeout: 60 * time.Second,
				Transport: &http.Transport{
					MaxIdleConns:        10,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			},
			model: model,
			dim:   embeddingDim,
		}
		if d, e := embedClient.getDim(context.Background()); e == nil && d > 0 {
			embedClient.dim = d
		}
		err = embedClient.ensureVectorTable()
	})
	return err
}

func (c *EmbeddingClient) getDim(ctx context.Context) (int, error) {
	testVec, err := c.embedSingle(ctx, "dimension test")
	if err != nil {
		return 0, err
	}
	return len(testVec), nil
}

func (c *EmbeddingClient) ensureVectorTable() error {
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fact_id INTEGER NOT NULL UNIQUE,
			embedding BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(fact_id) REFERENCES memory_facts(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_%s_fact ON %s(fact_id);
	`, vectorTable, vectorTable, vectorTable)
	_, err := db.Get().Exec(ddl)
	return err
}

// EmbedAndStore computes embedding for a fact and stores it.
func (c *EmbeddingClient) EmbedAndStore(ctx context.Context, factID int64, text string) error {
	vec, err := c.embedSingle(ctx, text)
	if err != nil {
		return fmt.Errorf("embedding failed: %w", err)
	}
	blob := floatsToBlob(vec)
	_, err = db.Get().ExecContext(ctx,
		fmt.Sprintf("INSERT OR REPLACE INTO %s (fact_id, embedding) VALUES (?, ?)", vectorTable),
		factID, blob)
	return err
}

// embedSingle calls Ollama for one text.
func (c *EmbeddingClient) embedSingle(ctx context.Context, text string) ([]float32, error) {
	reqBody := map[string]any{
		"model":  c.model,
		"prompt": text,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embeddings %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return result.Embedding, nil
}

// SearchVectors returns top-k fact IDs by cosine similarity.
func (c *EmbeddingClient) SearchVectors(ctx context.Context, query string, k int) ([]int64, []float32, error) {
	qVec, err := c.embedSingle(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	if k <= 0 {
		k = 10
	}

	rows, err := db.Get().QueryContext(ctx,
		fmt.Sprintf("SELECT fact_id, embedding FROM %s", vectorTable))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type scoredFact struct {
		id    int64
		score float32
	}
	var scored []scoredFact

	for rows.Next() {
		var factID int64
		var blob []byte
		if err := rows.Scan(&factID, &blob); err != nil {
			continue
		}
		vec := blobToFloats(blob)
		if len(vec) != len(qVec) {
			continue
		}
		sim := cosineSim(qVec, vec)
		scored = append(scored, scoredFact{factID, sim})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Partial sort top-k
	for i := 0; i < k && i < len(scored); i++ {
		maxIdx := i
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[maxIdx].score {
				maxIdx = j
			}
		}
		scored[i], scored[maxIdx] = scored[maxIdx], scored[i]
	}

	if len(scored) > k {
		scored = scored[:k]
	}
	ids := make([]int64, len(scored))
	sims := make([]float32, len(scored))
	for i, s := range scored {
		ids[i] = s.id
		sims[i] = s.score
	}
	return ids, sims, nil
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────

func floatsToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func blobToFloats(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSim(a, b []float32) float32 {
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(na))) * float32(math.Sqrt(float64(nb))))
}

// ──────────────────────────────────────────────────────────────────────
// Public API
// ──────────────────────────────────────────────────────────────────────

// EmbedFact computes and stores embedding for a fact ID.
func EmbedFact(ctx context.Context, factID int64, text string) ([]float32, error) {
	if embedClient == nil {
		return nil, fmt.Errorf("embeddings not initialized: call InitEmbeddings() at startup")
	}
	vec, err := embedClient.embedSingle(ctx, text)
	if err != nil {
		return nil, err
	}
	if err := embedClient.EmbedAndStore(ctx, factID, text); err != nil {
		return nil, err
	}
	return vec, nil
}

// SemanticRecall returns verified facts semantically similar to query.
func SemanticRecall(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	if embedClient == nil {
		return nil, fmt.Errorf("embeddings not initialized")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}

	ids, scores, err := embedClient.SearchVectors(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []map[string]any{}, nil
	}

	// Fetch fact details
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	q := fmt.Sprintf("SELECT id, fact, type, verified, tags, created_at FROM memory_facts WHERE id IN (%s)", placeholders)
	args := make([]any, len(ids))
	for i, v := range ids {
		args[i] = v
	}

	rows, err := db.Get().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id int64
		var fact, memType, tags string
		var verified int
		var createdAt time.Time
		if err := rows.Scan(&id, &fact, &memType, &verified, &tags, &createdAt); err != nil {
			continue
		}
		var score float32
		for i, fid := range ids {
			if fid == id {
				score = scores[i]
				break
			}
		}
		results = append(results, map[string]any{
			"id":          id,
			"fact":        fact,
			"type":        memType,
			"verified":    verified == 1,
			"tags":        tags,
			"created_at":  createdAt.Format(time.RFC3339),
			"similarity":  score,
		})
	}
	return results, nil
}

// BackfillEmbeddings computes embeddings for all existing facts without one.
func BackfillEmbeddings(ctx context.Context, batchSize int) (int, error) {
	if embedClient == nil {
		return 0, fmt.Errorf("embeddings not initialized")
	}
	if batchSize <= 0 {
		batchSize = embeddingBatch
	}

	rows, err := db.Get().QueryContext(ctx, `
		SELECT f.id, f.fact FROM memory_facts f
		LEFT JOIN memory_vectors v ON f.id = v.fact_id
		WHERE v.fact_id IS NULL
		LIMIT ?`, batchSize)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int64
		var fact string
		if err := rows.Scan(&id, &fact); err != nil {
			continue
		}
		if _, err := EmbedFact(ctx, id, fact); err != nil {
			log.Printf("[EMBED] backfill fact %d failed: %v", id, err)
			continue
		}
		count++
	}
	return count, nil
}