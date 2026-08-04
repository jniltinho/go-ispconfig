package cmd

// serveCmd starts the panel HTTP server: the /api REST endpoints (session
// auth, CRUD entities, swagger) plus the embedded SPA.

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/spf13/cobra"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/queue"
)

var serveCmd = &cobra.Command{
	Use:          "serve",
	Short:        "Start the panel web server (API + embedded SPA)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: cfg.Log.Slog(),
		})))

		if port, _ := cmd.Flags().GetInt("port"); port != 0 {
			cfg.Server.Port = port
		}
		certFile, keyFile, err := resolveTLS(cfg.Server, configDir())
		if err != nil {
			return err
		}

		db, err := database.Open(cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}

		// Instant-wake queue producer (design D12): every datalog write made
		// through the API enqueues a datalog:ready task for the local server
		// so its daemon processes the change without waiting for the poll
		// tick. Failure to resolve the server row only disables the wake —
		// the daemon's tick polling remains the fallback consumer.
		queueClient := queue.NewClient(cfg.Queue, slog.Default())
		defer queueClient.Close() //nolint:errcheck // best-effort close on shutdown
		if localServer, err := engine.ResolveServer(db, cfg.ServerID); err != nil {
			slog.Warn("could not resolve local server row, datalog ready notifications disabled",
				"error", err)
		} else {
			datalog.SetReadyNotifier(queueClient.ReadyNotifier(localServer.ServerID))
		}

		e := echo.New()
		e.Use(echoMiddleware.Recover())
		// Security headers: HSTS is only emitted by the middleware on TLS
		// requests; the CSP allows just the embedded SPA's own assets
		// (inline styles are injected by the Vue toolchain).
		e.Use(echoMiddleware.SecureWithConfig(echoMiddleware.SecureConfig{
			ContentTypeNosniff:    "nosniff",
			XFrameOptions:         "DENY",
			HSTSMaxAge:            31536000,
			ContentSecurityPolicy: "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'",
			ReferrerPolicy:        "no-referrer",
		}))

		deps := &api.Deps{
			DB:       db,
			Sessions: auth.NewStore(db, 0),
			Config:   cfg,
			// nil when mail.smtp_host is unset: messaging endpoints refuse,
			// welcome emails are skipped.
			Mailer: clients.NewSMTPMailer(cfg.Mail.SMTPHost, cfg.Mail.SMTPPort,
				cfg.Mail.SMTPUser, cfg.Mail.SMTPPass, cfg.Mail.From),
			// DKIM TXT records publish into locally managed DNS zones.
			DNSPub: api.NewDNSPublisher(db),
		}
		// Client limit_* enforcement behind the foundation create hook
		// (add-client-module D5); registered before the routes mount.
		api.RegisterLimitHook(clients.LimitHook(db))
		if err := api.Register(e, deps); err != nil {
			return fmt.Errorf("registering API: %w", err)
		}
		api.RegisterSwagger(e, deps.Sessions, cfg.Server.SwaggerPublic)

		e.GET("/api/health", func(c *echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{
				"status":  "ok",
				"version": Version,
			})
		})

		distFS, err := fs.Sub(globalFS, "web/dist")
		if err != nil {
			return fmt.Errorf("failed to open embedded web/dist: %w", err)
		}
		registerSPA(e, distFS)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		srv := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
			Handler:           e,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			<-ctx.Done()
			slog.Info("shutting down server")
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutCancel()
			_ = srv.Shutdown(shutCtx)
		}()

		slog.Info("starting server", "addr", srv.Addr, "https", certFile != "", "version", Version)
		var listenErr error
		if certFile != "" {
			listenErr = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			listenErr = srv.ListenAndServe()
		}
		if listenErr == http.ErrServerClosed {
			return nil
		}
		return listenErr
	},
}

// registerSPA serves the embedded Vite build: files that exist in the embed
// are served with correct MIME types, everything else falls back to
// index.html (history-mode router — including paths with dots such as FQDNs).
// Unmatched /api/* paths get a JSON 404, never index.html.
func registerSPA(e *echo.Echo, distFS fs.FS) {
	e.GET("/*", func(c *echo.Context) error {
		urlPath := c.Request().URL.Path
		if urlPath == "/api" || strings.HasPrefix(urlPath, "/api/") {
			return echo.ErrNotFound // Echo default handler renders JSON 404
		}

		if name := strings.TrimPrefix(urlPath, "/"); name != "" {
			if data, err := fs.ReadFile(distFS, name); err == nil {
				ext := strings.ToLower(filepath.Ext(name))
				ct := mime.TypeByExtension(ext)
				if ct == "" {
					ct = "application/octet-stream"
				}
				if ext == ".js" || ext == ".mjs" {
					ct = "application/javascript; charset=utf-8"
				}
				if strings.HasPrefix(urlPath, "/assets/") {
					c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				return c.Blob(http.StatusOK, ct, data)
			}
		}

		indexHTML, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			return echo.ErrNotFound
		}
		c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		return c.HTMLBlob(http.StatusOK, indexHTML)
	})
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().Int("port", 0, "HTTP port (overrides config; 0 = use config value)")
}
