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
	root.AddCommand(serveCmd(), scrapeCmd(), listCmd(), mcpCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func bootstrap(ctx context.Context) (config.Config, *geo.Catalog, store.Store, *scraper.Registry, error) {
	cfg := config.FromEnv()
	cat, err := geo.Load(cfg.CitiesPath)
	if err != nil {
		return cfg, nil, nil, nil, fmt.Errorf("load cities: %w", err)
	}
	st, err := store.NewSQLite(cfg.DBPath)
	if err != nil {
		return cfg, cat, nil, nil, fmt.Errorf("open store: %w", err)
	}
	if err := st.Init(ctx); err != nil {
		return cfg, cat, nil, nil, fmt.Errorf("init store: %w", err)
	}
	reg := scraper.NewRegistry()
	// Free sources — always on.
	reg.Register(scraper.NewEventbrite())
	reg.Register(scraper.NewSongkick())
	reg.Register(scraper.NewLuma())
	reg.Register(scraper.NewViralagenda())
	// Paid sources — only when explicitly opted in.
	if !cfg.FreeOnly {
		if cfg.TicketmasterKey != "" {
			reg.Register(scraper.NewTicketmaster(cfg.TicketmasterKey))
		}
		if cfg.MeetupOAuthToken != "" {
			reg.Register(scraper.NewMeetup(cfg.MeetupOAuthToken))
		}
	}
	return cfg, cat, st, reg, nil
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			cfg, cat, st, reg, err := bootstrap(ctx)
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
			sch := scheduler.New(st, reg, cat)
			slog.Info("eventscraper serve",
				"port", cfg.Port, "db", cfg.DBPath,
				"sources", len(reg.All()), "cities", len(cat.All()),
				"freeOnly", cfg.FreeOnly, "warmupCities", cfg.WarmupCities,
			)
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
			_, cat, st, reg, err := bootstrap(ctx)
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
			cfg, cat, st, reg, err := bootstrap(ctx)
			if err != nil {
				return err
			}
			_ = cfg
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
			sch := scheduler.New(st, reg, cat)
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

func listCmd() *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "List cities or sources"}
	c.AddCommand(&cobra.Command{
		Use:   "cities",
		Short: "Show all configured cities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cat, st, _, err := bootstrap(cmd.Context())
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
			_, _, st, reg, err := bootstrap(cmd.Context())
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
