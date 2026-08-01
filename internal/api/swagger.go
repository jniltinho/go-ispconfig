package api

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"
	swaggerFiles "github.com/swaggo/files/v2"
	"github.com/swaggo/swag"

	"go-ispconfig/internal/auth"

	// Register the generated OpenAPI spec (make swagger) with the swag
	// runtime so ReadDoc can serve it.
	_ "go-ispconfig/internal/api/docs"
)

// swaggerInitializer points the embedded Swagger UI at the generated spec
// instead of the default petstore example.
const swaggerInitializer = `window.onload = function() {
  window.ui = SwaggerUIBundle({
    url: "/swagger/doc.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    layout: "StandaloneLayout"
  });
};
`

// RegisterSwagger serves the embedded Swagger UI at /swagger/ (design D11):
// static assets from the swaggo/files embed, the generated spec at
// /swagger/doc.json. echo-swagger targets Echo v4, so the UI is wired
// directly on v5 here. Unless public (config server.swagger_public, meant
// for development), the UI and spec require an admin session — the spec
// enumerates the whole attack surface and stays off the anonymous internet.
func RegisterSwagger(e *echo.Echo, sessions auth.SessionGetter, public bool) {
	var mw []echo.MiddlewareFunc
	if !public {
		mw = []echo.MiddlewareFunc{auth.Middleware(sessions), auth.RequireAuth(), requireAdmin}
	}
	e.GET("/swagger", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/swagger/")
	}, mw...)
	e.GET("/swagger/*", func(c *echo.Context) error {
		name := strings.TrimPrefix(c.Request().URL.Path, "/swagger/")
		switch name {
		case "", "index.html":
			name = "index.html"
		case "doc.json":
			doc, err := swag.ReadDoc()
			if err != nil {
				return err
			}
			return c.Blob(http.StatusOK, "application/json; charset=utf-8", []byte(doc))
		case "swagger-initializer.js":
			return c.Blob(http.StatusOK, "application/javascript; charset=utf-8", []byte(swaggerInitializer))
		}
		data, err := fs.ReadFile(swaggerFiles.FS, name)
		if err != nil {
			return echo.ErrNotFound
		}
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		return c.Blob(http.StatusOK, ct, data)
	}, mw...)
}
