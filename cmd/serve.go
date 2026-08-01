package cmd

// serveCmd starts the panel HTTP server: /api endpoints plus the embedded SPA.
// This is a functional stub — the full Echo bootstrap (middleware stack, auth,
// handlers) arrives with the REST API core tasks.

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
	"github.com/spf13/viper"

	"go-ispconfig/internal/config"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the panel web server (API + embedded SPA)",
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		e := echo.New()
		e.Use(echoMiddleware.Recover())

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

		slog.Info("starting server", "addr", srv.Addr, "version", Version)
		var listenErr error
		if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
			listenErr = srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		} else {
			listenErr = srv.ListenAndServe()
		}
		if listenErr == http.ErrServerClosed {
			return nil
		}
		return listenErr
	},
}

// registerSPA serves the embedded Vite build: static assets by extension with
// correct MIME types, and index.html for every other path (history-mode router).
func registerSPA(e *echo.Echo, distFS fs.FS) {
	e.GET("/*", func(c *echo.Context) error {
		urlPath := c.Request().URL.Path

		ext := strings.ToLower(filepath.Ext(urlPath))
		if ext != "" {
			data, err := fs.ReadFile(distFS, strings.TrimPrefix(urlPath, "/"))
			if err != nil {
				return echo.ErrNotFound
			}
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
			c.Response().Header().Set("Content-Type", ct)
			_, _ = c.Response().Write(data)
			return nil
		}

		indexHTML, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			return echo.ErrNotFound
		}
		c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = c.Response().Write(indexHTML)
		return nil
	})
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().String("port", "", "HTTP port (overrides config)")
	_ = viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
}
