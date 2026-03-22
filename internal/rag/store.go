package rag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sunny-ai/sunny-profile/internal/ingestion"
)

// Store is the vector knowledge base
type Store struct {
	db         *sql.DB
	embedURL   string
	embedModel string
	httpCl     *http.Client
}

func NewStore(dbPath, embedURL, embedModel string) (*Store, error) {
	if strings.HasPrefix(dbPath, "~") {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, dbPath[1:])
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, embedURL: embedURL, embedModel: embedModel,
		httpCl: &http.Client{Timeout: 30 * time.Second}}
	return s, s.migrate()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS knowledge (
		id         TEXT PRIMARY KEY,
		source_url TEXT,
		label      TEXT,
		title      TEXT,
		content    TEXT,
		embedding  TEXT,
		metadata   TEXT,
		created_at DATETIME,
		updated_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_k_label ON knowledge(label);
	CREATE TABLE IF NOT EXISTS profile_meta (
		key TEXT PRIMARY KEY, value TEXT, updated_at DATETIME
	);
	`)
	return err
}

// Ingest embeds and stores a chunk
func (s *Store) Ingest(ctx context.Context, chunk ingestion.Chunk) error {
	emb, err := s.embed(ctx, chunk.Content)
	if err != nil {
		return err
	}
	embJSON, _ := json.Marshal(emb)
	metaJSON, _ := json.Marshal(chunk.Metadata)
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO knowledge(id,source_url,label,title,content,embedding,metadata,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		chunk.ID, chunk.SourceURL, chunk.Label, chunk.Title,
		chunk.Content, string(embJSON), string(metaJSON),
		chunk.CreatedAt, time.Now(),
	)
	return err
}

// SearchResult is one retrieved chunk
type SearchResult struct {
	ID      string
	URL     string
	Label   string
	Title   string
	Content string
	Score   float32
}

// Search finds the topK most relevant chunks for a query
func (s *Store) Search(ctx context.Context, query string, topK int, labelFilter string) ([]SearchResult, error) {
	qEmb, err := s.embed(ctx, query)
	if err != nil {
		return nil, err
	}

	var rows *sql.Rows
	if labelFilter != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id,source_url,label,title,content,embedding FROM knowledge WHERE label=?`, labelFilter)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id,source_url,label,title,content,embedding FROM knowledge`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		id, url, label, title, content string
		score                           float32
	}
	var cands []candidate

	for rows.Next() {
		var id, srcURL, label, title, content, embStr string
		if err := rows.Scan(&id, &srcURL, &label, &title, &content, &embStr); err != nil {
			continue
		}
		var emb []float32
		if json.Unmarshal([]byte(embStr), &emb) != nil {
			continue
		}
		cands = append(cands, candidate{id, srcURL, label, title, content, cosineSim(qEmb, emb)})
	}

	// Sort descending by score
	for i := 1; i < len(cands); i++ {
		k := cands[i]
		j := i - 1
		for j >= 0 && cands[j].score < k.score {
			cands[j+1] = cands[j]
			j--
		}
		cands[j+1] = k
	}

	var results []SearchResult
	for i, c := range cands {
		if i >= topK || c.score < 0.3 {
			break
		}
		results = append(results, SearchResult{c.id, c.url, c.label, c.title, c.content, c.score})
	}
	return results, nil
}

// BuildRAGContext builds a context string for LLM injection
func (s *Store) BuildRAGContext(ctx context.Context, query string, maxChars int) (string, error) {
	results, err := s.Search(ctx, query, 6, "")
	if err != nil || len(results) == 0 {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("## About the user (from their public profiles)\n\n")
	total := 0
	for _, r := range results {
		piece := fmt.Sprintf("[%s - %s]\n%s\n\n", r.Label, r.Title, r.Content)
		if total+len(piece) > maxChars {
			break
		}
		sb.WriteString(piece)
		total += len(piece)
	}
	return sb.String(), nil
}

// SetMeta stores a metadata key-value
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO profile_meta(key,value,updated_at) VALUES(?,?,?)`,
		key, value, time.Now())
	return err
}

// GetMeta retrieves a metadata value
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	return v, s.db.QueryRow(`SELECT value FROM profile_meta WHERE key=?`, key).Scan(&v)
}

// Stats returns chunk counts per label
func (s *Store) Stats() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT label, count(*) FROM knowledge GROUP BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var label string
		var count int
		if rows.Scan(&label, &count) == nil {
			out[label] = count
		}
	}
	return out, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) embed(ctx context.Context, text string) ([]float32, error) {
	if len(text) > 2048 {
		text = text[:2048]
	}
	payload, _ := json.Marshal(map[string]string{"model": s.embedModel, "prompt": text})
	req, err := http.NewRequestWithContext(ctx, "POST", s.embedURL+"/api/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpCl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("embed parse: %w", err)
	}
	return result.Embedding, nil
}

func cosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	d := math.Sqrt(na) * math.Sqrt(nb)
	if d == 0 {
		return 0
	}
	return float32(dot / d)
}
