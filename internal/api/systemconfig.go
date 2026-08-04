package api

// Panel-wide configuration surface (spec interface-config): read and write the
// sys_ini INI blob section by section, port of
// interface/web/admin/system_config_edit.php. Same merge semantics as the
// per-server editor — a write merges the submitted section back into the full
// parsed document, so keys the panel does not render survive the round trip —
// and the change is journalled for the audit trail (the legacy calls
// datalogUpdate('sys_ini', …) for the same reason).

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
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

// sysIniRowID is the single sys_ini row the panel configuration lives in.
const sysIniRowID = 1

// registerSystemConfigRoutes mounts /api/system/config* (admin only, gated by
// the admin_allow_system_config security policy).
func registerSystemConfigRoutes(g *echo.Group, d *Deps) {
	policy := auth.RequirePolicy(d.DB, "admin_allow_system_config")
	g.GET("/system/config", systemConfigGetHandler(d), requireAdmin, policy)
	g.PUT("/system/config/:section", systemConfigSaveHandler(d), requireAdmin, policy)
}

// loadSysIni reads the raw panel INI. A missing row is not an error: a fresh
// database has none until the first save, and every key then reads as empty,
// which is the PHP behaviour too.
func loadSysIni(db *gorm.DB) (string, getconf.Sections, error) {
	var row model.SysIni
	err := db.Select("COALESCE(config, '') AS config").
		Where("sysini_id = ?", sysIniRowID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", getconf.Sections{}, nil
	}
	if err != nil {
		return "", nil, err
	}
	return row.Config, getconf.ParseINI(getconf.StripSlashes(row.Config)), nil
}

// systemConfigGetHandler implements GET /api/system/config.
//
//	@Summary		Read the panel-wide configuration
//	@Description	Returns every parsed section of the sys_ini INI blob (sites, mail, misc, ...). Admin only, gated by the admin_allow_system_config security policy. Scope: system:read.
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	map[string]map[string]string
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/system/config [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func systemConfigGetHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		_, sections, err := loadSysIni(d.DB.WithContext(c.Request().Context()))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, sections)
	}
}

// Two deliberate semantics of this endpoint, both matching the legacy panel:
//
//   - any key name the INI grammar accepts may be written, not just the ones
//     the form renders. That is what lets an adopted ISPConfig3 database keep
//     working, and the surface is admin-only behind admin_allow_system_config;
//   - a merge never removes a key. Clearing a field stores an empty value; the
//     key stays in the blob.
//
// systemConfigSaveHandler implements PUT /api/system/config/{section}.
//
//	@Summary		Save one panel configuration section
//	@Description	Merges the submitted keys into the section, re-serialises the full INI and stores it in sys_ini, journalling the change. Keys of other sections are untouched; an empty body is refused so a broken form cannot blank a section. The password policy is validated: a minimum the panel could not satisfy is refused. Admin only, gated by admin_allow_system_config. Scope: system:write.
//	@Tags			system
//	@Accept			json
//	@Produce		json
//	@Param			section	path		string				true	"Section name (sites, mail, misc)"
//	@Param			body	body		map[string]string	true	"Section key/value pairs"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors (e.g. an unsatisfiable password policy)"
//	@Router			/system/config/{section} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func systemConfigSaveHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		name := c.Param("section")
		if !iniNameRe.MatchString(name) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid section name")
		}
		// ParseINI lowercases section headers, so "Misc" would merge into a
		// section that never matches the parsed "[misc]" and end up written as
		// a second, shadowed block in the same blob.
		name = strings.ToLower(name)
		// Decoded straight off the body, not via c.Bind: Echo's binder folds
		// path and query parameters into a map target, which would store
		// "section" as a config key.
		var body ServerConfigSection
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		if len(body) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "config section must not be empty")
		}
		for k, v := range body {
			if !iniNameRe.MatchString(k) || strings.ContainsAny(v, "\r\n") {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid config key "+k)
			}
			// Store what the validator checked: " 10 " must not reach the blob
			// (or the journal) with the spaces the numeric check ignored.
			body[k] = strings.TrimSpace(v)
		}
		if err := validateSysIniSection(name, body); err != nil {
			return err
		}

		user := ""
		if sess := auth.FromContext(c); sess != nil {
			user = sess.Username
		}
		var saved map[string]string
		err := datalogTxn(c.Request().Context(), d.DB, func(tx *gorm.DB) error {
			// Locked re-read: two concurrent section saves must not lose each
			// other's merge, exactly as in the per-server editor.
			old, sections, err := loadSysIni(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
			if err != nil {
				return err
			}
			if sections[name] == nil {
				sections[name] = map[string]string{}
			}
			maps.Copy(sections[name], body)
			ini := sections.String()
			// One upsert, not update-then-count-then-insert: a fresh database
			// has no sys_ini row, and SELECT ... FOR UPDATE locks nothing when
			// the row is absent, so two first saves would both find zero rows
			// and both insert.
			err = tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "sysini_id"}},
				DoUpdates: clause.Assignments(map[string]any{"config": ini}),
			}).Create(&model.SysIni{SysIniID: sysIniRowID, Config: ini}).Error
			if err != nil {
				return err
			}
			saved = sections[name]
			return datalog.LogSysIni(tx, old, ini, user)
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, saved)
	}
}

// validateSysIniSection applies the rules the INI grammar cannot express. Only
// the password policy is checked: it is the one setting whose bad value is
// felt immediately and everywhere, because every generated credential is
// measured against it.
func validateSysIniSection(section string, body ServerConfigSection) error {
	if section != "misc" {
		return nil
	}
	fields := map[string][]string{}
	if raw, ok := body["min_password_length"]; ok && strings.TrimSpace(raw) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n < 1 || n > sysIniMinPasswordLengthMax {
			fields["min_password_length"] = []string{"sysini_error_password_length"}
		}
	}
	if raw, ok := body["min_password_strength"]; ok && strings.TrimSpace(raw) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n < 1 || n > 3 {
			fields["min_password_strength"] = []string{"sysini_error_password_strength"}
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
