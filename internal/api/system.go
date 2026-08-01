package api

// System endpoints (task 4.5): the serve process never talks to the daemon
// directly — it reads the state mirrors the daemon persists in the database.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
)

// SchedulerJob is one scheduler job state mirrored by the daemon in
// sys_config group "scheduler" (engine.Scheduler.RunJob).
type SchedulerJob struct {
	// Name is the job identifier ("datalog_prune").
	Name string `json:"name" example:"datalog_prune"`
	// LastRun is the RFC3339 time of the last execution, empty if never run.
	LastRun string `json:"last_run" example:"2026-08-01T03:00:00Z"`
	// Status is "ok" or "error: <message>" from the last execution.
	Status string `json:"status" example:"ok"`
}

// registerSystemRoutes mounts the system endpoints on the authenticated
// group (admin-only).
func registerSystemRoutes(g *echo.Group, d *Deps) {
	g.GET("/system/scheduler", schedulerJobsHandler(d), requireAdmin)
}

// requireAdmin rejects sessions whose access level is not "admin" with 403.
// Mount after auth.RequireAuth.
func requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if sess := auth.FromContext(c); sess == nil || sess.Typ != "admin" {
			return echo.NewHTTPError(http.StatusForbidden, "admin access required")
		}
		return next(c)
	}
}

// schedulerJobsHandler implements GET /api/system/scheduler.
//
//	@Summary		Scheduler job status
//	@Description	Lists the daemon scheduler jobs with their last-run time and status, read from the sys_config "scheduler" mirror the daemon persists after every job execution. Admin only.
//	@Tags			system
//	@Produce		json
//	@Success		200	{array}		SchedulerJob
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin"
//	@Router			/system/scheduler [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func schedulerJobsHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.SysConfig
		err := d.DB.WithContext(c.Request().Context()).
			Where("`group` = ?", "scheduler").Find(&rows).Error
		if err != nil {
			return err
		}

		jobs := map[string]*SchedulerJob{}
		get := func(name string) *SchedulerJob {
			if jobs[name] == nil {
				jobs[name] = &SchedulerJob{Name: name}
			}
			return jobs[name]
		}
		for _, row := range rows {
			if name, ok := strings.CutSuffix(row.Name, "_last_run"); ok {
				get(name).LastRun = row.Value
			} else if name, ok := strings.CutSuffix(row.Name, "_status"); ok {
				get(name).Status = row.Value
			}
		}

		out := make([]SchedulerJob, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, *j)
		}
		sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
		return c.JSON(http.StatusOK, out)
	}
}
