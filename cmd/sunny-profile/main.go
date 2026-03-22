package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/sunny-ai/sunny-profile/internal/ingestion"
	"github.com/sunny-ai/sunny-profile/internal/rag"
	"gopkg.in/yaml.v3"
)

var (
	profileFile string
	embedURL    string
	embedModel  string
	dbPath      string
)

type Config struct {
	Profile    ingestion.ProfileConfig `yaml:"profile"`
	EmbedURL   string                  `yaml:"embed_url"`
	EmbedModel string                  `yaml:"embed_model"`
	DBPath     string                  `yaml:"db_path"`
}

func main() {
	root := &cobra.Command{Use: "sunny-profile", Short: "Sunny Profile — ingest personal knowledge"}

	root.PersistentFlags().StringVar(&profileFile, "profile", "profile.yaml", "Profile sources file")
	root.PersistentFlags().StringVar(&embedURL, "embed-url", "http://promaxgb10-6116:9107", "Ollama embed URL")
	root.PersistentFlags().StringVar(&embedModel, "embed-model", "nomic-embed-text", "Embedding model name")
	root.PersistentFlags().StringVar(&dbPath, "db", "~/.config/sunny/knowledge.db", "Knowledge DB path")

	root.AddCommand(
		&cobra.Command{
			Use:   "ingest",
			Short: "Ingest all profile sources",
			RunE:  runIngest,
		},
		&cobra.Command{
			Use:   "search [query]",
			Short: "Search the knowledge base",
			Args:  cobra.MinimumNArgs(1),
			RunE:  runSearch,
		},
		&cobra.Command{
			Use:   "stats",
			Short: "Show knowledge base statistics",
			RunE:  runStats,
		},
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getStore() (*rag.Store, error) {
	return rag.NewStore(dbPath, embedURL, embedModel)
}

func runIngest(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	data, err := os.ReadFile(profileFile)
	if err != nil {
		return fmt.Errorf("read %s: %w\n\nCreate one from template: cp profile.yaml profile.local.yaml", profileFile, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if cfg.EmbedURL != "" {
		embedURL = cfg.EmbedURL
	}
	if cfg.EmbedModel != "" {
		embedModel = cfg.EmbedModel
	}
	if cfg.DBPath != "" {
		dbPath = cfg.DBPath
	}

	store, err := getStore()
	if err != nil {
		return err
	}
	defer store.Close()

	fmt.Printf("\nSunny Profile Ingestion\n")
	fmt.Printf("  Sources: %d\n", len(cfg.Profile.Sources))
	fmt.Printf("  Embed:   %s (%s)\n", embedURL, embedModel)
	fmt.Printf("  DB:      %s\n\n", dbPath)

	pipeline := ingestion.NewPipeline()
	summary, err := pipeline.Run(ctx, cfg.Profile, func(ctx context.Context, chunk ingestion.Chunk) error {
		return store.Ingest(ctx, chunk)
	})
	if err != nil {
		return err
	}
	summary.Print()

	statsMap, _ := store.Stats()
	fmt.Println("\nKnowledge base contents:")
	for label, count := range statsMap {
		fmt.Printf("  %-20s %d chunks\n", label, count)
	}
	return nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	store, err := getStore()
	if err != nil {
		return err
	}
	defer store.Close()

	query := args[0]
	fmt.Printf("Searching: %q\n\n", query)

	results, err := store.Search(context.Background(), query, 5, "")
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No results. Run sunny-profile ingest first.")
		return nil
	}
	for i, r := range results {
		fmt.Printf("[%d] %.3f  [%s]  %s\n", i+1, r.Score, r.Label, r.Title)
		fmt.Printf("    %s\n", r.URL)
		preview := r.Content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Printf("    %s\n\n", preview)
	}
	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	store, err := getStore()
	if err != nil {
		return err
	}
	defer store.Close()

	statsMap, err := store.Stats()
	if err != nil {
		return err
	}
	fmt.Println("\nSunny Knowledge Base Stats")
	fmt.Println("---------------------------")
	total := 0
	for label, count := range statsMap {
		fmt.Printf("  %-20s %d chunks\n", label, count)
		total += count
	}
	fmt.Printf("---------------------------\n  %-20s %d\n\n", "TOTAL", total)
	return nil
}
