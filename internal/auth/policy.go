package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// PolicyGroup is the sys_config group holding the security policy flags
// (port of ISPConfig's security/security_settings.ini [permissions] section,
// design/spec: stored in the DB instead of an ini file on disk).
const PolicyGroup = "security"

// SuperadminUserID is the sys_user id with exclusive access to policies set
// to "superadmin" (ISPConfig semantics: only userid 1, not every admin).
const SuperadminUserID = 1

// policyDefaults ports the [permissions] section of ISPConfig's
// security_settings.ini.master verbatim. A flag missing from sys_config
// falls back to these values, so an unseeded database behaves exactly like
// a stock ISPConfig install.
var policyDefaults = map[string]string{
	"allow_shell_user":            "yes",
	"admin_allow_server_config":   "superadmin",
	"admin_allow_server_services": "superadmin",
	"admin_allow_server_ip":       "superadmin",
	"admin_allow_remote_users":    "superadmin",
	"admin_allow_system_config":   "superadmin",
	"admin_allow_server_php":      "superadmin",
	"admin_allow_langedit":        "superadmin",
	"admin_allow_new_admin":       "superadmin",
	"admin_allow_del_cpuser":      "superadmin",
	"admin_allow_cpuser_group":    "superadmin",
	"admin_allow_firewall_config": "superadmin",
	"admin_allow_osupdate":        "superadmin",
	"remote_api_allowed":          "yes",
	"password_reset_allowed":      "yes",
	"session_regenerate_id":       "yes",
	"reverse_proxy_panel_allowed": "none",
}

// GetPolicy returns the effective value of a security policy flag: the
// sys_config row in the "security" group when present, the ISPConfig
// default otherwise, "" for unknown flags.
func GetPolicy(db *gorm.DB, name string) (string, error) {
	var row model.SysConfig
	err := db.Where("`group` = ? AND name = ?", PolicyGroup, name).First(&row).Error
	switch {
	case err == nil:
		return row.Value, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return policyDefaults[name], nil
	default:
		return "", fmt.Errorf("loading policy %s: %w", name, err)
	}
}

// PolicyAllows evaluates a policy value for a user: "yes" allows everyone,
// "superadmin" allows only sys_user id 1, anything else ("no", "none",
// unknown) denies.
func PolicyAllows(value string, userID uint32) bool {
	switch value {
	case "yes":
		return true
	case "superadmin":
		return userID == SuperadminUserID
	default:
		return false
	}
}

// CheckPolicy reports whether the user passes the named security policy
// flag (spec requirement "Security policy flags").
func CheckPolicy(db *gorm.DB, name string, userID uint32) (bool, error) {
	value, err := GetPolicy(db, name)
	if err != nil {
		return false, err
	}
	return PolicyAllows(value, userID), nil
}

// SeedPolicyDefaults inserts the ISPConfig default security flags into
// sys_config, keeping any value an operator already customized.
func SeedPolicyDefaults(db *gorm.DB) error {
	for name, value := range policyDefaults {
		res := db.Exec(
			"INSERT IGNORE INTO sys_config (`group`, `name`, `value`) VALUES (?, ?, ?)",
			PolicyGroup, name, value)
		if res.Error != nil {
			return fmt.Errorf("seeding policy %s: %w", name, res.Error)
		}
	}
	return nil
}

// RequirePolicy is an Echo middleware enforcing a security policy flag for
// the authenticated user; failing the policy yields 403 (e.g. a non-id-1
// admin hitting a superadmin-only endpoint). Mount after Middleware and
// RequireAuth.
func RequirePolicy(db *gorm.DB, name string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			sess := FromContext(c)
			if sess == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			ok, err := CheckPolicy(db, name, sess.UserID)
			if err != nil {
				return err
			}
			if !ok {
				return echo.NewHTTPError(http.StatusForbidden, "blocked by security policy "+name)
			}
			return next(c)
		}
	}
}
