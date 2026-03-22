package ingestion

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ProfileConfig is the top-level ingestion configuration
type ProfileConfig struct {
	Sources []Source `yaml:"sources" json:"sources"`
}

// IngestCallback is called for each extracted chunk
type IngestCallback func(ctx context.Context, chunk Chunk) error

// Pipeline orchestrates all source ingestion
type Pipeline struct {
	crawler *Crawler
	github  *GitHubCrawler
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		crawler: NewCrawler(),
		github:  NewGitHubCrawler(),
	}
}

// Run ingests all sources and calls cb per chunk
func (p *Pipeline) Run(ctx context.Context, cfg ProfileConfig, cb IngestCallback) (*Summary, error) {
	summary := &Summary{StartTime: time.Now()}
	for _, src := range cfg.Sources {
		fmt.Printf("  -> Ingesting [%s] %s\n", src.Label, src.URL)
		chunks, err := p.ingestSource(ctx, src)
		if err != nil {
			fmt.Printf("     error: %v\n", err)
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", src.URL, err))
			continue
		}
		fmt.Printf("     %d chunks\n", len(chunks))
		for _, chunk := range chunks {
			if err := cb(ctx, chunk); err != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("store %s: %v", truncate(chunk.ID, 8), err))
				continue
			}
			summary.ChunksStored++
		}
	}
	summary.Duration = time.Since(summary.StartTime)
	return summary, nil
}

func (p *Pipeline) ingestSource(ctx context.Context, src Source) ([]Chunk, error) {
	// GitHub: use API for structured data + HTML crawl
	if strings.Contains(src.URL, "github.com") {
		parts := strings.Split(strings.TrimSuffix(src.URL, "/"), "/")
		if len(parts) >= 4 {
			username := parts[len(parts)-1]
			if apiChunks, err := p.github.ExtractProfile(ctx, username); err == nil {
				htmlChunks, _ := p.crawler.Crawl(ctx, src)
				return append(apiChunks, htmlChunks...), nil
			}
		}
	}
	return p.crawler.Crawl(ctx, src)
}

// Summary holds ingestion results
type Summary struct {
	ChunksStored int
	Errors       []string
	Duration     time.Duration
	StartTime    time.Time
}

func (s *Summary) Print() {
	fmt.Println("\n=== Ingestion Complete ===")
	fmt.Printf("Chunks stored: %d\n", s.ChunksStored)
	fmt.Printf("Duration:      %s\n", s.Duration.Round(time.Millisecond))
	if len(s.Errors) > 0 {
		fmt.Printf("Errors:        %d\n", len(s.Errors))
		for _, e := range s.Errors {
			fmt.Printf("  ! %s\n", e)
		}
	}
	fmt.Println("==========================")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
