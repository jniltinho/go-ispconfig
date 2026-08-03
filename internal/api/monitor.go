package api

// Monitor module REST surface (add-monitor-module tasks 4.1–4.3, design D6):
// read-only views over monitor_data / sys_log / sys_datalog plus the
// admin-only sys_log clear. Every route requires the "monitor" module in the
// session's sys_user.modules CSV (admins always pass); server-scoped lists
// only cover server rows the caller may read (riud on table server).

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/monitor"
)

// registerMonitorRoutes mounts /api/monitor/* on the authenticated group.
func registerMonitorRoutes(g *echo.Group, d *Deps) {
	mg := g.Group("/monitor", requireMonitorModule)
	mg.GET("/state", monitorStateHandler(d))
	mg.GET("/data", monitorDataListHandler(d))
	mg.GET("/data/:type", monitorDataTypeHandler(d))
}

// requireMonitorModule rejects sessions without the monitor module (403).
// Mount after auth.RequireAuth.
func requireMonitorModule(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !monitor.HasMonitorModule(auth.FromContext(c)) {
			return echo.NewHTTPError(http.StatusForbidden, "monitor module required")
		}
		return next(c)
	}
}

// MonitorDataItem is one monitor_data row with its payload dual-decoded
// (JSON first, PHP serialize fallback for pre-cutover history).
type MonitorDataItem struct {
	// ServerID is the collecting server.
	ServerID uint32 `json:"server_id"`
	// Type is the check id (cpu_info, disk_usage, services, ...).
	Type string `json:"type" example:"disk_usage"`
	// Created is the collection unix timestamp.
	Created uint32 `json:"created"`
	// State is the check severity (no_state/ok/info/warning/critical/error/unknown).
	State string `json:"state" example:"ok"`
	// Data is the decoded payload (object or array), nil when decode failed.
	Data any `json:"data,omitempty"`
	// DataRaw carries the raw payload when decoding failed.
	DataRaw string `json:"data_raw,omitempty"`
	// DecodeError is set when the payload is neither JSON nor PHP serialize.
	DecodeError string `json:"decode_error,omitempty"`
}

// monitorItem converts a monitor_data row into the decoded API shape.
func monitorItem(r model.MonitorData) MonitorDataItem {
	dec := monitor.DecodePayload(r.Data)
	return MonitorDataItem{
		ServerID: r.ServerID, Type: r.Type, Created: r.Created, State: r.State,
		Data: dec.Value, DataRaw: dec.Raw, DecodeError: dec.DecodeError,
	}
}

// monitorServers loads the caller's readable servers, optionally narrowed to
// ?server_id=. A requested server outside the readable set yields an empty
// list (the API never reveals whether the server exists).
func monitorServers(c *echo.Context, d *Deps) ([]model.Server, error) {
	servers, err := monitor.ReadableServers(c.Request().Context(), d.DB, identity(c))
	if err != nil {
		return nil, err
	}
	if sid := c.QueryParam("server_id"); sid != "" {
		want, err := strconv.ParseUint(sid, 10, 32)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid server_id")
		}
		for _, s := range servers {
			if uint64(s.ServerID) == want {
				return []model.Server{s}, nil
			}
		}
		return nil, nil
	}
	return servers, nil
}

// monitorStateHandler implements GET /api/monitor/state.
//
//	@Summary		System state overview
//	@Description	Aggregated per-server system state (port of show_sys_state.php): newest monitor_data row per check type folded with the _setState severity order, plus human-readable messages for disk, load, services, updates and sys_log. Covers every server the caller may read; ?server_id= narrows to one.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int	false	"Limit to one server"
//	@Success		200			{array}		monitor.ServerState
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/state [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorStateHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		states, err := monitor.AggregateSystemState(c.Request().Context(), d.DB, servers)
		if err != nil {
			return err
		}
		if states == nil {
			states = []monitor.ServerState{}
		}
		return c.JSON(http.StatusOK, states)
	}
}

// monitorDataListHandler implements GET /api/monitor/data.
//
//	@Summary		List monitor data
//	@Description	monitor_data rows for the caller's readable servers, newest first, payload dual-decoded (JSON, PHP serialize fallback). ?latest=1 keeps only the newest row per server and type.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int		false	"Limit to one server"
//	@Param			type		query		string	false	"Check type (cpu_info, disk_usage, services, ...)"
//	@Param			state		query		string	false	"Severity filter (ok, warning, error, ...)"
//	@Param			latest		query		bool	false	"Only the newest row per server and type"	default(true)
//	@Param			limit		query		int		false	"Max rows"									default(100)
//	@Success		200			{array}		MonitorDataItem
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/data [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorDataListHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		if len(servers) == 0 {
			return c.JSON(http.StatusOK, []MonitorDataItem{})
		}
		limit, _ := strconv.Atoi(c.QueryParamOr("limit", "100"))
		if limit < 1 || limit > 500 {
			limit = 100
		}
		latest := c.QueryParamOr("latest", "1") != "0"
		rows, err := monitor.ListData(c.Request().Context(), d.DB, monitor.DataFilter{
			ServerIDs:  monitor.ServerIDs(servers),
			Type:       c.QueryParam("type"),
			State:      c.QueryParam("state"),
			LatestOnly: latest,
			Limit:      limit,
		})
		if err != nil {
			return err
		}
		out := make([]MonitorDataItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, monitorItem(r))
		}
		return c.JSON(http.StatusOK, out)
	}
}

// monitorDataTypeHandler implements GET /api/monitor/data/{type}.
//
//	@Summary		Latest monitor data for one check type
//	@Description	The newest monitor_data row of the given type (port of show_data.php). Defaults to the caller's first readable server; ?server_id= selects another readable server.
//	@Tags			monitor
//	@Produce		json
//	@Param			type		path		string	true	"Check type (cpu_info, disk_usage, services, ...)"
//	@Param			server_id	query		int		false	"Server (default: first readable)"
//	@Success		200			{object}	MonitorDataItem
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Failure		404			{object}	ErrorResponse	"No sample collected yet"
//	@Router			/monitor/data/{type} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorDataTypeHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		if len(servers) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "no accessible server")
		}
		row, err := monitor.LatestByType(c.Request().Context(), d.DB, servers[0].ServerID, c.Param("type"))
		if err != nil {
			return err // gorm.ErrRecordNotFound → 404 by the central error handler
		}
		return c.JSON(http.StatusOK, monitorItem(row))
	}
}
