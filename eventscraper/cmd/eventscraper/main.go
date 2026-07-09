package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/jorgenunes/eventscraper/internal/api"
	"github.com/jorgenunes/eventscraper/internal/config"
	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/mcpserver"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scheduler"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	root := &cobra.Command{Use: "eventscraper", Short: "Worldwide events scraper"}
	root.AddCommand(serveCmd(), scrapeCmd(), listCmd(), mcpCmd(), migratePostgresCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func bootstrap(ctx context.Context) (config.Config, *geo.Catalog, store.Store, *scraper.Registry, *scraper.StealthClient, *scraper.ProxyPool, error) {
	cfg := config.FromEnv()
	cat, err := geo.Load(cfg.CitiesPath)
	if err != nil {
		return cfg, nil, nil, nil, nil, nil, fmt.Errorf("load cities: %w", err)
	}
	var st store.Store
	if cfg.DatabaseURL != "" {
		st, err = store.NewPostgres(ctx, cfg.DatabaseURL)
	} else {
		st, err = store.NewSQLite(cfg.DBPath)
	}
	if err != nil {
		return cfg, cat, nil, nil, nil, nil, fmt.Errorf("open store: %w", err)
	}
	if err := st.Init(ctx); err != nil {
		return cfg, cat, nil, nil, nil, nil, fmt.Errorf("init store: %w", err)
	}

	// Shared stealth client + proxy pool. An empty pool (no proxy config)
	// means direct connections — the scrapers work identically either way.
	pool := scraper.NewProxyPool(scraper.LoadProxies(cfg.ProxiesInline, cfg.ProxyListPath, cfg.ProxyListURL))
	if pool.Len() > 0 {
		slog.Info("proxy pool loaded", "count", pool.Len())
	}
	client := scraper.NewStealthClient(scraper.StealthConfig{
		Pool:       pool,
		Timeout:    20 * time.Second,
		MaxRetries: cfg.ScrapeMaxRetries,
	})

	reg := scraper.NewRegistry()
	// Free sources — always on.
	reg.Register(scraper.NewEventbrite(pool))
	reg.Register(scraper.NewSongkick(pool))
	reg.Register(scraper.NewLuma(client))
	reg.Register(scraper.NewViralagenda(client))
	// Paid sources — only when explicitly opted in.
	if !cfg.FreeOnly {
		if cfg.TicketmasterKey != "" {
			reg.Register(scraper.NewTicketmaster(cfg.TicketmasterKey, client))
		}
		if cfg.MeetupOAuthToken != "" {
			reg.Register(scraper.NewMeetup(cfg.MeetupOAuthToken))
		}
	}
	return cfg, cat, st, reg, client, pool, nil
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			cfg, cat, st, reg, client, pool, err := bootstrap(ctx)
			if err != nil {
				return err
			}
			defer st.Close()
			// Strip known placeholder image URLs from the cache so the UI
			// falls back to the EventCard placeholder instead of trying to
			// load a broken default.
			if n, err := st.ClearImageURLsMatching(ctx, []string{
				"%default-event%",
				"%default_event%",
				"%default_images%",
				"%placeholder%",
				"%no-image%",
				"%no_image%",
			}); err == nil && n > 0 {
				slog.Info("cleared placeholder images", "count", n)
			}
			sch := scheduler.New(st, reg, cat,
				scheduler.WithConcurrency(cfg.ScrapeConcurrency),
				scheduler.WithDelays(cfg.ScrapeMinDelayMS, cfg.ScrapeMaxDelayMS),
				scheduler.WithScrapeClient(client),
			)
			backend := "sqlite:" + cfg.DBPath
			if cfg.DatabaseURL != "" {
				backend = "postgres"
			}
			slog.Info("eventscraper serve",
				"port", cfg.Port, "store", backend,
				"sources", len(reg.All()), "cities", len(cat.All()),
				"freeOnly", cfg.FreeOnly, "warmupCities", cfg.WarmupCities,
				"concurrency", cfg.ScrapeConcurrency, "proxies", pool.Len(),
			)
			// Refresh the proxy list periodically (no-op without a URL source).
			go scraper.AutoReloadProxies(ctx, pool, cfg.ProxyListURL, 10*time.Minute)
			// Kick off warmup in the background so the feed is populated
			// without blocking the server start.
			go sch.Warmup(ctx, cfg.WarmupCities)
			srv := api.NewServer(cfg, st, cat, reg, sch)
			if err := srv.Serve(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve scraper data over the Model Context Protocol (stdio)",
		Long:  "Runs an MCP server on stdin/stdout exposing search_events, list_cities and list_sources tools. Logs go to stderr so they don't corrupt the protocol stream.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			_, cat, st, reg, _, _, err := bootstrap(ctx)
			if err != nil {
				return err
			}
			defer st.Close()
			slog.Info("eventscraper mcp", "transport", "stdio", "sources", len(reg.All()), "cities", len(cat.All()))
			srv := mcpserver.New(st, cat, reg)
			if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}
}

func scrapeCmd() *cobra.Command {
	var (
		srcStr  string
		cityID  string
		catStrs []string
	)
	c := &cobra.Command{
		Use:   "scrape",
		Short: "Run a one-shot scrape for a source+city",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			cfg, cat, st, reg, client, _, err := bootstrap(ctx)
			if err != nil {
				return err
			}
			defer st.Close()
			src := model.Source(srcStr)
			if !src.Valid() {
				return fmt.Errorf("invalid source %q", srcStr)
			}
			city, ok := cat.Get(cityID)
			if !ok {
				return fmt.Errorf("unknown city %q", cityID)
			}
			if _, ok := reg.Get(src); !ok {
				return fmt.Errorf("source %q not registered (free-only mode or missing creds?)", src)
			}
			cats := make([]model.Category, 0, len(catStrs))
			for _, s := range catStrs {
				c := model.Category(s)
				if !c.Valid() {
					return fmt.Errorf("invalid category %q", s)
				}
				cats = append(cats, c)
			}
			sch := scheduler.New(st, reg, cat,
				scheduler.WithConcurrency(cfg.ScrapeConcurrency),
				scheduler.WithDelays(cfg.ScrapeMinDelayMS, cfg.ScrapeMaxDelayMS),
				scheduler.WithScrapeClient(client),
			)
			sch.Run(ctx, src, city, cats)
			// Re-read what we just persisted so we can report counts.
			rows, _, _, err := st.Query(ctx, store.Query{Source: src, CityID: city.ID, Limit: 2000})
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]any{
				"source": src,
				"city":   city.ID,
				"count":  len(rows),
			})
			return nil
		},
	}
	c.Flags().StringVar(&srcStr, "source", "", "eventbrite | songkick | luma | ticketmaster | meetup")
	c.Flags().StringVar(&cityID, "city", "", "city id from cities.yaml (e.g. london)")
	c.Flags().StringSliceVar(&catStrs, "category", nil, "tech | music | business (repeat or comma-separate)")
	_ = c.MarkFlagRequired("source")
	_ = c.MarkFlagRequired("city")
	return c
}

func migratePostgresCmd() *cobra.Command {
	var from, to string
	var batch int
	c := &cobra.Command{
		Use:   "migrate-postgres",
		Short: "Copy events, scrape status and geo-addresses from a SQLite DB into Postgres",
		Long:  "Idempotent (upsert-based) one-way copy from a SQLite file into a Postgres/PostGIS store. Safe to re-run. Target DSN comes from --to or $DATABASE_URL.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if to == "" {
				to = os.Getenv("DATABASE_URL")
			}
			if to == "" {
				return fmt.Errorf("target Postgres DSN required: pass --to or set DATABASE_URL")
			}
			src, err := store.NewSQLite(from)
			if err != nil {
				return fmt.Errorf("open source sqlite: %w", err)
			}
			defer src.Close()
			dst, err := store.NewPostgres(ctx, to)
			if err != nil {
				return fmt.Errorf("open target postgres: %w", err)
			}
			defer dst.Close()
			if err := dst.Init(ctx); err != nil {
				return fmt.Errorf("init target schema: %w", err)
			}
			stats, err := store.MigrateSQLiteToPostgres(ctx, src, dst, batch)
			if err != nil {
				return err
			}
			slog.Info("migration complete",
				"events", stats.Events, "scrapes", stats.Scrapes,
				"geoAddresses", stats.GeoAddresses,
			)
			return nil
		},
	}
	c.Flags().StringVar(&from, "from", "./eventscraper.db", "source SQLite database path")
	c.Flags().StringVar(&to, "to", "", "target Postgres DSN (default $DATABASE_URL)")
	c.Flags().IntVar(&batch, "batch", 500, "rows per batch")
	return c
}

func listCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "List cities or sources"}
	c.AddCommand(&cobra.Command{
		Use:   "cities",
		Short: "Show all configured cities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cat, st, _, _, _, err := bootstrap(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			for _, c := range cat.All() {
				fmt.Printf("%-20s %s (%s)\n", c.ID, c.Name, c.Country)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "sources",
		Short: "Show all configured sources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _, st, reg, _, _, err := bootstrap(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			for _, src := range model.AllSources() {
				_, ok := reg.Get(src)
				state := "disabled"
				if ok {
					state = "enabled"
				}
				fmt.Printf("%-15s %s\n", src, state)
			}
			return nil
		},
	})
	return c
}
