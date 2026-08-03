package api

// Fail2ban live-state surface: the jail list with its banned IPs, and the
// unban action. State is read from the daemon through fail2ban-client (not
// from /var/log/fail2ban.log), so both routes are admin-only and every
// argument that reaches an argv slice is validated first.

import (
	"net"
	"net/http"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/fail2ban"
)

// fail2banClient talks to the local fail2ban daemon.
var fail2banClient = fail2ban.NewClient()

// registerFail2banRoutes mounts /api/monitor/fail2ban/* (admin only).
func registerFail2banRoutes(g *echo.Group, _ *Deps) {
	g.GET("/monitor/fail2ban/status", fail2banStatusHandler(), requireAdmin)
	g.POST("/monitor/fail2ban/unban/:jail/:ip", fail2banUnbanHandler(), requireAdmin)
}

// Fail2banStatus is the GET /api/monitor/fail2ban/status response.
type Fail2banStatus struct {
	// Available is false when fail2ban-client is absent or the daemon is
	// down; the jail list is then empty and the UI shows a hint instead of
	// an error.
	Available bool `json:"available"`
	// Jails carries one entry per live jail with its banned addresses.
	Jails []fail2ban.Status `json:"jails"`
}

// fail2banStatusHandler implements GET /api/monitor/fail2ban/status.
//
//	@Summary		Fail2ban jails and bans
//	@Description	Live fail2ban state read with `fail2ban-client status` plus `fail2ban-client status <jail>`: every jail with its failure/ban counters and currently banned addresses. A host without fail2ban (or with the daemon stopped) returns 200 with available=false and no jails, so the panel can render an explanatory empty state. Admin only.
//	@Tags			monitor
//	@Produce		json
//	@Success		200	{object}	Fail2banStatus
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin session"
//	@Router			/monitor/fail2ban/status [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func fail2banStatusHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		names, err := fail2banClient.Jails(ctx)
		if err != nil {
			return c.JSON(http.StatusOK, Fail2banStatus{})
		}
		res := Fail2banStatus{Available: true, Jails: []fail2ban.Status{}}
		for _, name := range names {
			st, err := fail2banClient.Status(ctx, name)
			if err != nil {
				st = fail2ban.Status{Name: name}
			}
			res.Jails = append(res.Jails, st)
		}
		return c.JSON(http.StatusOK, res)
	}
}

// fail2banUnbanHandler implements POST /api/monitor/fail2ban/unban/:jail/:ip.
//
//	@Summary		Unban an IP
//	@Description	Runs `fail2ban-client set <jail> unbanip <ip>` and returns the refreshed jail status. The jail name must match ^[a-zA-Z0-9._-]+$ and the address must parse as an IP, otherwise the request is rejected with 422 before any command is built. Admin only.
//	@Tags			monitor
//	@Produce		json
//	@Param			jail	path		string	true	"Jail name"
//	@Param			ip		path		string	true	"Banned IP address"
//	@Success		200		{object}	fail2ban.Status
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin session"
//	@Failure		422		{object}	ErrorResponse	"Malformed jail name or IP"
//	@Failure		500		{object}	ErrorResponse
//	@Router			/monitor/fail2ban/unban/{jail}/{ip} [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func fail2banUnbanHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		jail, ip := c.Param("jail"), c.Param("ip")
		if !fail2ban.ValidJailName(jail) {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "invalid jail name")
		}
		if net.ParseIP(ip) == nil {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "invalid ip address")
		}
		ctx := c.Request().Context()
		if err := fail2banClient.Unban(ctx, jail, ip); err != nil {
			return err
		}
		st, err := fail2banClient.Status(ctx, jail)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, st)
	}
}
