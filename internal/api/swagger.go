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
// directly on v5 here.
func RegisterSwagger(e *echo.Echo) {
	e.GET("/swagger", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/swagger/")
	})
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
	})
}
