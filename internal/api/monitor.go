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
	mg.GET("/sys-log", monitorSysLogHandler(d))
	mg.POST("/sys-log/clear", monitorSysLogClearHandler(d), requireAdmin)
	mg.GET("/jobqueue", monitorJobqueueHandler(d))
	mg.GET("/jobqueue/count", monitorJobqueueCountHandler(d))
	mg.GET("/datalog", monitorDatalogListHandler(d))
	mg.GET("/datalog/:id", monitorDatalogDetailHandler(d))
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

// SysLogList is the paginated GET /api/monitor/sys-log response.
type SysLogList struct {
	// Items are the sys_log rows of the requested page, newest first.
	Items []model.SysLog `json:"items"`
	// Total is the number of rows matching the filters.
	Total int64 `json:"total"`
	// Page is the returned 1-based page number.
	Page int `json:"page"`
	// Limit is the applied page size.
	Limit int `json:"limit"`
}

// monitorSysLogHandler implements GET /api/monitor/sys-log.
//
//	@Summary		List system log entries
//	@Description	Paginated sys_log rows for the caller's readable servers, ordered by tstamp DESC (port of log_list.php). Cleared rows have loglevel 0.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int		false	"Limit to one server"
//	@Param			loglevel	query		int		false	"0 debug, 1 warning, 2 error"
//	@Param			message		query		string	false	"Message substring filter"
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Success		200			{object}	SysLogList
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/sys-log [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorSysLogHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		if len(servers) == 0 {
			return c.JSON(http.StatusOK, SysLogList{Items: []model.SysLog{}, Page: 1, Limit: 25})
		}
		page, limit := pageParams(c)
		f := monitor.SysLogFilter{
			ServerIDs: monitor.ServerIDs(servers),
			Message:   c.QueryParam("message"),
			Limit:     limit,
			Offset:    (page - 1) * limit,
		}
		if lv := c.QueryParam("loglevel"); lv != "" {
			n, err := strconv.ParseInt(lv, 10, 8)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid loglevel")
			}
			l := int8(n)
			f.Loglevel = &l
		}
		rows, total, err := monitor.ListSysLog(c.Request().Context(), d.DB, f)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []model.SysLog{}
		}
		return c.JSON(http.StatusOK, SysLogList{Items: rows, Total: total, Page: page, Limit: limit})
	}
}

// SysLogClearRequest selects the sys_log rows to clear: one id or a whole
// loglevel batch (exactly one selector).
type SysLogClearRequest struct {
	// SyslogID clears one row.
	SyslogID uint32 `json:"syslog_id,omitempty"`
	// Loglevel batch-clears every row of this level (1 warning, 2 error).
	Loglevel *int8 `json:"loglevel,omitempty"`
}

// SysLogClearResponse reports how many rows were cleared.
type SysLogClearResponse struct {
	// Cleared is the number of rows whose loglevel was set to 0.
	Cleared int64 `json:"cleared"`
}

// monitorSysLogClearHandler implements POST /api/monitor/sys-log/clear.
//
//	@Summary		Clear system log entries (admin)
//	@Description	Port of log_del.php: sets loglevel = 0 for one syslog_id or for every row of a loglevel — rows are never deleted. Admin only; no sys_datalog row is written.
//	@Tags			monitor
//	@Accept			json
//	@Produce		json
//	@Param			request	body		SysLogClearRequest	true	"One of syslog_id or loglevel"
//	@Success		200		{object}	SysLogClearResponse
//	@Failure		400		{object}	ErrorResponse	"Neither selector given"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Router			/monitor/sys-log/clear [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorSysLogClearHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var req SysLogClearRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		ctx := c.Request().Context()
		switch {
		case req.SyslogID != 0:
			n, err := monitor.ClearSysLog(ctx, d.DB, req.SyslogID)
			if err != nil {
				return err
			}
			return c.JSON(http.StatusOK, SysLogClearResponse{Cleared: n})
		case req.Loglevel != nil:
			n, err := monitor.ClearSysLogByLevel(ctx, d.DB, *req.Loglevel, nil)
			if err != nil {
				return err
			}
			return c.JSON(http.StatusOK, SysLogClearResponse{Cleared: n})
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "syslog_id or loglevel required")
		}
	}
}

// DatalogItem is one sys_datalog row with its {old,new} payload dual-decoded
// (JSON first, PHP serialize fallback for pre-cutover history).
type DatalogItem struct {
	// DatalogID is the journal row id.
	DatalogID uint32 `json:"datalog_id"`
	// ServerID is the target server (0 = all servers).
	ServerID uint32 `json:"server_id"`
	// DBTable is the affected table.
	DBTable string `json:"dbtable" example:"web_domain"`
	// DBIdx is "column:value" of the affected primary key.
	DBIdx string `json:"dbidx" example:"domain_id:7"`
	// Action is i (insert), u (update) or d (delete).
	Action string `json:"action" example:"u"`
	// Tstamp is the change unix timestamp.
	Tstamp int32 `json:"tstamp"`
	// User is the panel user who made the change.
	User string `json:"user" example:"admin"`
	// Status is the daemon processing status.
	Status string `json:"status" example:"ok"`
	// Error is the daemon error message, if any.
	Error string `json:"error,omitempty"`
	// Data is the decoded {old,new} diff, nil when decode failed.
	Data any `json:"data,omitempty"`
	// DataRaw carries the raw payload when decoding failed.
	DataRaw string `json:"data_raw,omitempty"`
	// DecodeError is set when the payload is neither JSON nor PHP serialize.
	DecodeError string `json:"decode_error,omitempty"`
}

// datalogItem converts a sys_datalog row into the decoded API shape.
// withData=false skips the payload (list views).
func datalogItem(r model.SysDatalog, withData bool) DatalogItem {
	it := DatalogItem{
		DatalogID: r.DatalogID, ServerID: r.ServerID, DBTable: r.DBTable,
		DBIdx: r.DBIdx, Action: r.Action, Tstamp: r.Tstamp, User: r.User,
		Status: r.Status, Error: r.Error,
	}
	if withData {
		dec := monitor.DecodePayload(r.Data)
		it.Data, it.DataRaw, it.DecodeError = dec.Value, dec.Raw, dec.DecodeError
	}
	return it
}

// DatalogList is a paginated sys_datalog listing (jobqueue or history).
type DatalogList struct {
	// Items are the rows of the requested page (payload omitted; use the
	// detail endpoint).
	Items []DatalogItem `json:"items"`
	// Total is the number of rows matching the filters.
	Total int64 `json:"total"`
	// Page is the returned 1-based page number.
	Page int `json:"page"`
	// Limit is the applied page size.
	Limit int `json:"limit"`
}

// pageParams reads ?page= and ?limit= with defaults 1 / 25 (max 100).
func pageParams(c *echo.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.QueryParamOr("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(c.QueryParamOr("limit", "25"))
	if limit < 1 || limit > 100 {
		limit = 25
	}
	return page, limit
}

// datalogListJSON pages rows into the DatalogList response shape.
func datalogListJSON(c *echo.Context, rows []model.SysDatalog, total int64, page, limit int) error {
	items := make([]DatalogItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, datalogItem(r, false))
	}
	return c.JSON(http.StatusOK, DatalogList{Items: items, Total: total, Page: page, Limit: limit})
}

// monitorJobqueueHandler implements GET /api/monitor/jobqueue.
//
//	@Summary		List pending jobqueue entries
//	@Description	sys_datalog rows not yet processed by the daemon (datalog_id > server.updated, including server_id=0 all-server rows), oldest first — port of show_jobqueue.php. Covers the caller's readable servers; ?server_id= narrows to one.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int		false	"Limit to one server"
//	@Param			dbtable		query		string	false	"Affected table filter"
//	@Param			action		query		string	false	"i, u or d"
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Success		200			{object}	DatalogList
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/jobqueue [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorJobqueueHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		page, limit := pageParams(c)
		rows, total, err := monitor.ListJobqueue(c.Request().Context(), d.DB, monitor.JobqueueFilter{
			Servers: servers,
			DBTable: c.QueryParam("dbtable"),
			Action:  c.QueryParam("action"),
			Limit:   limit,
			Offset:  (page - 1) * limit,
		})
		if err != nil {
			return err
		}
		return datalogListJSON(c, rows, total, page, limit)
	}
}

// JobqueueCount reports the number of pending jobqueue entries.
type JobqueueCount struct {
	// Count is the number of unprocessed sys_datalog rows.
	Count int64 `json:"count"`
}

// monitorJobqueueCountHandler implements GET /api/monitor/jobqueue/count.
//
//	@Summary		Count pending jobqueue entries
//	@Description	Number of sys_datalog rows not yet processed by the daemon across the caller's readable servers (including server_id=0 all-server rows). ?server_id= narrows to one server.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int	false	"Limit to one server"
//	@Success		200			{object}	JobqueueCount
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/jobqueue/count [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorJobqueueCountHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		total, err := monitor.CountJobqueue(c.Request().Context(), d.DB, servers, nil)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, JobqueueCount{Count: total})
	}
}

// monitorDatalogListHandler implements GET /api/monitor/datalog.
//
//	@Summary		List datalog history
//	@Description	Full sys_datalog change journal (processed and pending), newest first — port of datalog_list.php. Covers the caller's readable servers plus server_id=0 all-server rows; payload omitted, use the detail endpoint.
//	@Tags			monitor
//	@Produce		json
//	@Param			server_id	query		int		false	"Limit to one server"
//	@Param			dbtable		query		string	false	"Affected table filter"
//	@Param			action		query		string	false	"i, u or d"
//	@Param			user		query		string	false	"Panel user filter"
//	@Param			page		query		int		false	"1-based page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(25)
//	@Success		200			{object}	DatalogList
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse	"Session lacks the monitor module"
//	@Router			/monitor/datalog [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorDatalogListHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		servers, err := monitorServers(c, d)
		if err != nil {
			return err
		}
		if len(servers) == 0 {
			return c.JSON(http.StatusOK, DatalogList{Items: []DatalogItem{}, Page: 1, Limit: 25})
		}
		page, limit := pageParams(c)
		// server_id=0 rows target all servers and are visible alongside any
		// readable server.
		ids := append(monitor.ServerIDs(servers), 0)
		rows, total, err := monitor.ListDatalogHistory(c.Request().Context(), d.DB, monitor.DatalogHistoryFilter{
			ServerIDs: ids,
			DBTable:   c.QueryParam("dbtable"),
			Action:    c.QueryParam("action"),
			User:      c.QueryParam("user"),
			Limit:     limit,
			Offset:    (page - 1) * limit,
		})
		if err != nil {
			return err
		}
		return datalogListJSON(c, rows, total, page, limit)
	}
}

// monitorDatalogDetailHandler implements GET /api/monitor/datalog/{id}.
//
//	@Summary		Datalog entry detail
//	@Description	One sys_datalog row with its {old,new} field diff dual-decoded (JSON first, PHP serialize fallback) — port of datalog_view.php, read-only (no undo). 404 when the row targets a server the caller may not read.
//	@Tags			monitor
//	@Produce		json
//	@Param			id	path		int	true	"datalog_id"
//	@Success		200	{object}	DatalogItem
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Session lacks the monitor module"
//	@Failure		404	{object}	ErrorResponse	"Unknown id or unreadable server"
//	@Router			/monitor/datalog/{id} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func monitorDatalogDetailHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := strconv.ParseUint(c.Param("id"), 10, 32)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
		}
		servers, err := monitor.ReadableServers(c.Request().Context(), d.DB, identity(c))
		if err != nil {
			return err
		}
		row, err := monitor.GetDatalog(c.Request().Context(), d.DB, uint32(id))
		if err != nil {
			return err // gorm.ErrRecordNotFound → 404 by the central error handler
		}
		readable := row.ServerID == 0 && len(servers) > 0
		for _, s := range servers {
			if s.ServerID == row.ServerID {
				readable = true
			}
		}
		if !readable {
			return echo.NewHTTPError(http.StatusNotFound, "not found")
		}
		return c.JSON(http.StatusOK, datalogItem(row, true))
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
