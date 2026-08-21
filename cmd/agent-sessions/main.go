package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Thelost77/agent-sessions/internal/adapter"
	"github.com/Thelost77/agent-sessions/internal/config"
	"github.com/Thelost77/agent-sessions/internal/embed"
	"github.com/Thelost77/agent-sessions/internal/indexer"
	"github.com/Thelost77/agent-sessions/internal/model"
	"github.com/Thelost77/agent-sessions/internal/search"
	"github.com/Thelost77/agent-sessions/internal/store"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	cfg, configPath, err := config.Load()
	if err != nil {
		return err
	}
	if err := config.ExpandPaths(&cfg); err != nil {
		return err
	}

	switch args[0] {
	case "index":
		return runIndex(ctx, cfg, args[1:])
	case "search":
		return runSearch(ctx, cfg, args[1:])
	case "status":
		return runStatus(ctx, cfg, args[1:])
	case "doctor":
		return runDoctor(ctx, cfg, configPath, args[1:])
	case "version", "--version", "-version":
		fmt.Println(buildVersion())
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func buildVersion() string {
	if version != "dev" {
		return strings.TrimPrefix(version, "v")
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return version
}

func runIndex(ctx context.Context, cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("index", flag.ContinueOnError)
	rebuild := flags.Bool("rebuild", false, "rebuild the complete index")
	reembed := flags.Bool("reembed", false, "regenerate every embedding")
	quiet := flags.Bool("quiet", false, "suppress progress messages")
	harnesses := flags.String("harness", "", "comma-separated harness names")
	addCommonFlags(flags, &cfg)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("index does not accept positional arguments")
	}
	selected, err := selectHarnesses(cfg, *harnesses)
	if err != nil {
		return err
	}
	lock, err := indexer.AcquireLock(cfg.Index + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	if *rebuild {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(cfg.Index + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove old index: %w", err)
			}
		}
	}
	storage, err := store.Open(ctx, cfg.Index)
	if err != nil {
		return err
	}
	defer storage.Close()
	embedder, err := embed.New(cfg.Embedding.URL, cfg.Embedding.Model)
	if err != nil {
		return err
	}
	adapters, closeAdapters, err := makeAdapters(ctx, cfg, selected)
	if err != nil {
		return err
	}
	defer closeAdapters()
	progress := func(string) {}
	if !*quiet {
		progress = func(message string) { fmt.Fprintln(os.Stderr, message) }
	}
	result, runErr := (&indexer.Indexer{
		Store: storage, Adapters: adapters, Embedder: embedder,
	}).Run(ctx, indexer.Options{Reembed: *reembed, LockHeld: true, Progress: progress})
	if !*quiet {
		fmt.Printf("Discovered: %d\nUpdated: %d\nUnchanged: %d\nDeleted: %d\nParser errors: %d\nEmbedded: %d\n",
			result.Discovered, result.UpdatedSources, result.UnchangedSources,
			result.DeletedSources, result.ParserErrors, result.EmbeddedChunks)
	}
	if result.EmbeddingError != nil {
		fmt.Fprintln(os.Stderr, "warning: semantic indexing is incomplete:", result.EmbeddingError)
	}
	return runErr
}

func runSearch(ctx context.Context, cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	path := flags.String("path", "", "exact directory or parent directory")
	harnesses := flags.String("harness", "", "comma-separated harness names")
	roles := flags.String("role", "", "comma-separated roles")
	since := flags.String("since", "", "minimum session date")
	before := flags.String("before", "", "maximum session date")
	limit := flags.Int("limit", search.DefaultLimit, "maximum session results")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	explain := flags.Bool("explain", false, "show retrieval channel ranks")
	lexicalOnly := flags.Bool("lexical-only", false, "skip semantic retrieval")
	semanticThreshold := flags.Float64("semantic-threshold", cfg.Embedding.Threshold, "minimum semantic cosine similarity")
	addCommonFlags(flags, &cfg)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return fmt.Errorf("search requires a query")
	}
	if *limit < 1 || *limit > 100 {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	if math.IsNaN(*semanticThreshold) || *semanticThreshold < 0 || *semanticThreshold > 1 {
		return fmt.Errorf("semantic threshold must be between 0 and 1")
	}
	selected, err := parseHarnessList(*harnesses)
	if err != nil {
		return err
	}
	roleValues, err := parseRoles(*roles)
	if err != nil {
		return err
	}
	sinceTime, err := parseDate(*since, false)
	if err != nil {
		return fmt.Errorf("invalid --since: %w", err)
	}
	beforeTime, err := parseDate(*before, true)
	if err != nil {
		return fmt.Errorf("invalid --before: %w", err)
	}
	if *path != "" {
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		*path = filepath.Clean(absolute)
	}
	storage, err := store.Open(ctx, cfg.Index)
	if err != nil {
		return err
	}
	defer storage.Close()
	var embedder embed.Provider
	if !*lexicalOnly {
		embedder, err = embed.New(cfg.Embedding.URL, cfg.Embedding.Model)
		if err != nil {
			return err
		}
	}
	response, err := (&search.Searcher{
		Store: storage, Embedder: embedder, SemanticThreshold: *semanticThreshold,
	}).Search(ctx, query, search.Filters{
		Harnesses: selected, Path: *path, Roles: roleValues,
		Since: sinceTime, Before: beforeTime, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	}
	if response.SemanticError != "" {
		fmt.Fprintln(os.Stderr, "warning: semantic search unavailable:", response.SemanticError)
	}
	printResults(response.Results, *explain)
	return nil
}

func runStatus(ctx context.Context, cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	addCommonFlags(flags, &cfg)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("status does not accept positional arguments")
	}
	storage, err := store.Open(ctx, cfg.Index)
	if err != nil {
		return err
	}
	defer storage.Close()
	embedder, err := embed.New(cfg.Embedding.URL, cfg.Embedding.Model)
	if err != nil {
		return err
	}
	stats, err := storage.Stats(ctx, embedder.Fingerprint())
	if err != nil {
		return err
	}
	fmt.Printf("Index:       %s\nUpdated:     %s\nSources:     %d\nSessions:    %d\nChunks:      %d\nEmbeddings:  %d\nPending:     %d\nParse errors: %d\nModel:       %s\nDimensions:  %d\n",
		storage.Path(), emptyDash(stats.LastIndexedAt), stats.Sources, stats.Sessions,
		stats.Chunks, stats.Embeddings, stats.PendingEmbeddings, stats.ParserErrors,
		cfg.Embedding.Model, stats.EmbeddingDims)
	return nil
}

func runDoctor(ctx context.Context, cfg config.Config, configPath string, args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	addCommonFlags(flags, &cfg)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("doctor does not accept positional arguments")
	}
	fmt.Println("Config:", configPath)
	fmt.Println("Index: ", cfg.Index)

	selected, err := selectHarnesses(cfg, "")
	if err != nil {
		return err
	}
	adapters, closeAdapters, err := makeAdapters(ctx, cfg, selected)
	if err != nil {
		return err
	}
	defer closeAdapters()
	failures := 0
	for _, sourceAdapter := range adapters {
		sources, err := sourceAdapter.Discover(ctx)
		if err != nil {
			fmt.Printf("[FAIL] %-8s %v\n", sourceAdapter.Name(), err)
			failures++
			continue
		}
		fmt.Printf("[OK]   %-8s %d sources\n", sourceAdapter.Name(), len(sources))
	}
	storage, err := store.Open(ctx, cfg.Index)
	if err != nil {
		fmt.Println("[FAIL] index   ", err)
		failures++
	} else {
		defer storage.Close()
		fmt.Println("[OK]   index    schema and permissions")
		errorsFound, err := storage.ParserErrors(ctx)
		if err != nil {
			fmt.Println("[FAIL] errors  ", err)
			failures++
		} else {
			for _, message := range errorsFound {
				fmt.Println("[WARN] parser  ", message)
			}
		}
	}
	embedder, err := embed.New(cfg.Embedding.URL, cfg.Embedding.Model)
	if err != nil {
		fmt.Println("[FAIL] embedder ", err)
		failures++
	} else if vectors, err := embedder.Embed(ctx, []string{"agent session search health check"}); err != nil {
		fmt.Println("[FAIL] embedder ", err)
		failures++
	} else {
		fmt.Printf("[OK]   embedder %s (%d dimensions)\n", embedder.Model(), len(vectors[0]))
	}
	if failures > 0 {
		return fmt.Errorf("doctor found %d failure(s)", failures)
	}
	return nil
}

func addCommonFlags(flags *flag.FlagSet, cfg *config.Config) {
	flags.StringVar(&cfg.Index, "index", cfg.Index, "index database path")
	flags.StringVar(&cfg.Embedding.URL, "embedding-url", cfg.Embedding.URL, "Ollama base URL")
	flags.StringVar(&cfg.Embedding.Model, "embedding-model", cfg.Embedding.Model, "Ollama embedding model")
}

func makeAdapters(ctx context.Context, cfg config.Config, selected []string) ([]model.Adapter, func(), error) {
	var values []model.Adapter
	var closers []model.AdapterCloser
	for _, name := range selected {
		var value model.Adapter
		switch name {
		case "pi":
			value = &adapter.Pi{Root: cfg.Sources.Pi}
		case "codex":
			value = &adapter.Codex{Root: cfg.Sources.Codex}
		case "opencode":
			value = &adapter.OpenCode{DBPath: cfg.Sources.OpenCode}
		case "claude":
			value = &adapter.Claude{Root: cfg.Sources.Claude}
		default:
			return nil, func() {}, fmt.Errorf("unsupported harness %q", name)
		}
		values = append(values, value)
		if closer, ok := value.(model.AdapterCloser); ok {
			closers = append(closers, closer)
		}
	}
	closeAll := func() {
		for _, closer := range closers {
			_ = closer.Close()
		}
	}
	_ = ctx
	return values, closeAll, nil
}

func selectHarnesses(cfg config.Config, requested string) ([]string, error) {
	if requested != "" {
		return parseHarnessList(requested)
	}
	var values []string
	for _, name := range []string{"pi", "codex", "opencode", "claude"} {
		if cfg.Harnesses[name] {
			values = append(values, name)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no harnesses are enabled")
	}
	return values, nil
}

func parseHarnessList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	allowed := map[string]bool{"pi": true, "codex": true, "opencode": true, "claude": true}
	seen := map[string]bool{}
	var result []string
	for _, item := range strings.Split(value, ",") {
		name := strings.ToLower(strings.TrimSpace(item))
		if !allowed[name] {
			return nil, fmt.Errorf("unsupported harness %q", name)
		}
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result, nil
}

func parseRoles(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	allowed := map[string]bool{"user": true, "assistant": true}
	var result []string
	for _, item := range strings.Split(value, ",") {
		role := strings.ToLower(strings.TrimSpace(item))
		if !allowed[role] {
			return nil, fmt.Errorf("unsupported role %q", role)
		}
		result = append(result, role)
	}
	return result, nil
}

func parseDate(value string, endOfDay bool) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339")
	}
	if endOfDay {
		parsed = parsed.Add(24*time.Hour - time.Millisecond)
	}
	return parsed, nil
}

func printResults(results []search.Result, explain bool) {
	if len(results) == 0 {
		fmt.Println("No matching sessions.")
		return
	}
	for _, result := range results {
		date := result.UpdatedAt.Format("2006-01-02")
		if result.UpdatedAt.IsZero() {
			date = "unknown date"
		}
		fmt.Printf("%d. [%s] %s\n   %s\n", result.CombinedRank, result.Harness, result.Name, date)
		if result.CWD != "" {
			fmt.Println("   " + result.CWD)
		}
		fmt.Println("   Session: " + result.SessionID)
		if result.ParentID != "" {
			fmt.Println("   Parent:  " + result.ParentID)
		}
		if result.EntryID != "" {
			fmt.Println("   Entry:   " + result.EntryID)
		}
		if result.Excerpt != "" {
			fmt.Printf("\n   %s\n", result.Excerpt)
		}
		if explain {
			parts := []string{}
			if result.Ranks.Exact > 0 {
				parts = append(parts, "exact="+strconv.Itoa(result.Ranks.Exact))
			}
			if result.Ranks.Lexical > 0 {
				parts = append(parts, "lexical="+strconv.Itoa(result.Ranks.Lexical))
			}
			if result.Ranks.Fuzzy > 0 {
				parts = append(parts, "fuzzy="+strconv.Itoa(result.Ranks.Fuzzy))
			}
			if result.Ranks.Semantic > 0 {
				semantic := "semantic=" + strconv.Itoa(result.Ranks.Semantic)
				semantic += " (similarity=" + strconv.FormatFloat(result.SemanticSimilarity, 'f', 3, 64) + ")"
				parts = append(parts, semantic)
			}
			fmt.Println("\n   Ranks: " + strings.Join(parts, ", "))
		}
		fmt.Printf("\n   Resume:\n   %s\n\n", result.Resume)
	}
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printUsage() {
	commands := []string{"index", "search", "status", "doctor", "version"}
	sort.Strings(commands)
	fmt.Println("Search local coding-agent sessions.")
	fmt.Println("\nUsage:\n  agent-sessions <command> [options]")
	fmt.Println("\nCommands:")
	for _, command := range commands {
		fmt.Println("  " + command)
	}
	fmt.Println("\nExamples:")
	fmt.Println("  agent-sessions index")
	fmt.Println(`  agent-sessions search --path ~/projects/qr-codes "QR codes without assets"`)
	fmt.Println("  agent-sessions status")
}
