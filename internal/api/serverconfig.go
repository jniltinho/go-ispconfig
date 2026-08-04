package api

// Per-server configuration surface (spec server-config-sync): read and write
// the server.config INI blob section by section, port of
// interface/web/admin/server_config_edit.php:96-181. A write merges the
// submitted section back into the full parsed document, so keys the panel
// does not render survive the round trip, and journals a sys_datalog row for
// dbtable=server so the owning node applies the change on its next cycle.

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// registerServerConfigRoutes mounts /api/servers/:id/config* (admin only).
func registerServerConfigRoutes(g *echo.Group, d *Deps) {
	g.GET("/servers/:id/config", serverConfigGetHandler(d), requireAdmin)
	g.GET("/servers/:id/config/:section", serverConfigSectionHandler(d), requireAdmin)
	g.PUT("/servers/:id/config/:section", serverConfigSaveHandler(d), requireAdmin)
}

// iniNameRe is the section/key grammar the INI parser accepts
// (getconf.ParseINI); anything else would be written and then dropped on the
// next read.
var iniNameRe = regexp.MustCompile(`^[\w\d_]+$`)

// ServerConfigSection is the GET/PUT body for one config section: the raw
// key/value pairs of the INI section, untyped because the panel must round
// trip keys it does not know.
type ServerConfigSection map[string]string

// serverConfigID parses and validates the :id path parameter.
func serverConfigID(c *echo.Context) (uint32, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid server id")
	}
	return uint32(id), nil
}

// loadServerConfig reads the raw INI of one server, mapping a missing row to
// 404 so a stale panel link does not surface as a 500.
func loadServerConfig(db *gorm.DB, serverID uint32) (string, getconf.Sections, error) {
	var srv model.Server
	err := db.Select("COALESCE(config, '') AS config").Take(&srv, serverID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, echo.NewHTTPError(http.StatusNotFound, "server not found")
	}
	if err != nil {
		return "", nil, err
	}
	return srv.Config, getconf.ParseINI(getconf.StripSlashes(srv.Config)), nil
}

// serverConfigGetHandler implements GET /api/servers/:id/config.
//
//	@Summary		Read a server's full configuration
//	@Description	Returns every parsed section of the server.config INI blob. Admin only.
//	@Tags			servers
//	@Produce		json
//	@Param			id	path		int	true	"Server id"
//	@Success		200	{object}	map[string]map[string]string
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/servers/{id}/config [get]
func serverConfigGetHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := serverConfigID(c)
		if err != nil {
			return err
		}
		_, sections, err := loadServerConfig(d.DB.WithContext(c.Request().Context()), id)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, sections)
	}
}

// serverConfigSectionHandler implements GET /api/servers/:id/config/:section.
//
//	@Summary		Read one configuration section
//	@Description	Returns the key/value pairs of one section (web, mail, dns, jailkit, ...) of a server's configuration. An absent section reads as an empty object, matching the INI parser. Admin only.
//	@Tags			servers
//	@Produce		json
//	@Param			id		path		int		true	"Server id"
//	@Param			section	path		string	true	"Section name"
//	@Success		200		{object}	map[string]string
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/servers/{id}/config/{section} [get]
func serverConfigSectionHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := serverConfigID(c)
		if err != nil {
			return err
		}
		_, sections, err := loadServerConfig(d.DB.WithContext(c.Request().Context()), id)
		if err != nil {
			return err
		}
		section := sections[c.Param("section")]
		if section == nil {
			section = map[string]string{}
		}
		return c.JSON(http.StatusOK, section)
	}
}

// serverConfigSaveHandler implements PUT /api/servers/:id/config/:section.
//
//	@Summary		Save one configuration section
//	@Description	Merges the submitted keys into the section, re-serialises the full INI and stores it, then journals a sys_datalog row for dbtable=server so the owning node applies it. Keys of other sections are untouched; an empty body is refused so a broken form cannot blank a section. Keys this port acts on are validated (the [server] section's ssh_port must be a TCP port). Admin only.
//	@Tags			servers
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"Server id"
//	@Param			section	path		string				true	"Section name"
//	@Param			body	body		map[string]string	true	"Section key/value pairs"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors (e.g. an out-of-range ssh_port)"
//	@Router			/servers/{id}/config/{section} [put]
func serverConfigSaveHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := serverConfigID(c)
		if err != nil {
			return err
		}
		name := c.Param("section")
		if !iniNameRe.MatchString(name) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid section name")
		}
		// Decoded straight off the request body, not via c.Bind: Echo's
		// binder also folds path and query parameters into a map target,
		// which would store "id" and "section" as config keys.
		var body ServerConfigSection
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		// A section that parses to zero keys is refused rather than stored:
		// upstream never writes an empty section either, and doing so would
		// silently drop every key the node relies on.
		if len(body) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "config section must not be empty")
		}
		// A name or value the INI grammar cannot express would be written
		// out and then silently dropped by the next parse — losing the
		// stored config one key at a time. Refuse it at the boundary.
		for k, v := range body {
			if !iniNameRe.MatchString(k) || strings.ContainsAny(v, "\r\n") {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid config key "+k)
			}
		}
		if err := validateConfigSection(name, body); err != nil {
			return err
		}

		user := ""
		if sess := auth.FromContext(c); sess != nil {
			user = sess.Username
		}
		var saved map[string]string
		err = datalogTxn(c.Request().Context(), d.DB, func(tx *gorm.DB) error {
			// Re-read inside the transaction under SELECT ... FOR UPDATE:
			// two concurrent section saves must not lose each other's merge.
			old, sections, err := loadServerConfig(tx.Clauses(clause.Locking{Strength: "UPDATE"}), id)
			if err != nil {
				return err
			}
			if sections[name] == nil {
				sections[name] = map[string]string{}
			}
			for k, v := range body {
				sections[name][k] = v
			}
			ini := sections.String()
			if err := tx.Model(&model.Server{}).Where("server_id = ?", id).
				Update("config", ini).Error; err != nil {
				return err
			}
			saved = sections[name]
			return datalog.LogServerConfig(tx, id, old, ini, user)
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, saved)
	}
}

// validateConfigSection applies the per-field rules that the INI grammar
// cannot express. Only the keys this port acts on are checked: a
// compatibility field is written for ISPConfig3's benefit and validating it
// here would mean guessing what the PHP daemon accepts.
func validateConfigSection(section string, body ServerConfigSection) error {
	if section != "server" {
		return nil
	}
	// An out-of-range SSH port would be silently ignored by the firewall
	// module (it falls back to 22), leaving the operator convinced they had
	// changed something. Refuse it at the boundary instead.
	if port, ok := body["ssh_port"]; ok && strings.TrimSpace(port) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(port))
		if err != nil || n < 1 || n > 65535 {
			return &ValidationError{Fields: map[string][]string{
				"ssh_port": {"srvcfg_error_ssh_port"},
			}}
		}
	}
	return nil
}
