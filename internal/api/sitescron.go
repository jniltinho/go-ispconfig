package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/cron"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// sitesCronEntity is the /api/sites/crons surface (add-cron-module task 4.1+):
// port of cron.tform.php / sites_cron_* under the Sites module.
func sitesCronEntity() *Entity {
	return &Entity{
		Name:  "crons",
		Title: "cron_edit_title",
		Prepare: func(c *echo.Context, deps *Deps, id *repository.Identity, body map[string]any) error {
			return sitesCronPrepare(c, deps, id, body)
		},
		AfterInsert:  sitesCronAfterInsert,
		BeforeUpdate: sitesCronBeforeUpdate,
		// siteChildDecorate's password redaction is a no-op for cron rows;
		// reused for datalog state + _server_name/_parent_domain columns.
		Decorate: siteChildDecorate("cron", "id"),
		ListFilters: map[string]ListFilterFunc{
			"_server_name":   relatedNameFilter("server_id", "server", "server_id", "server_name"),
			"_parent_domain": relatedNameFilter("parent_domain_id", "web_domain", "domain_id", "domain"),
		},
		Tabs: []Tab{{
			Name: "cron", Label: "cron_tab_txt",
			Fields: []Field{
				selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "parent_domain_id_error_empty"}),
				// server_id is derived from the parent; declared so list/get
				// expose it. Create body value is overwritten in Prepare.
				{Name: "server_id", Label: "server_id_txt", Datatype: "INTEGER", Formtype: "TEXT"},
				selectField("type", "cron_type_txt", "VARCHAR", model.CronTypeURL, []Option{
					{Value: model.CronTypeURL, Label: "cron_type_url_txt"},
					{Value: model.CronTypeChrooted, Label: "cron_type_chrooted_txt"},
					{Value: model.CronTypeFull, Label: "cron_type_full_txt"},
				}),
				text("command", "command_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "command_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "command_error_format", Fn: checkCronCommand}),
				text("run_min", "run_min_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_min_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "run_min_error_format", Fn: checkCronRunMin}),
				text("run_hour", "run_hour_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_hour_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "run_hour_error_format", Fn: checkCronRunHour}),
				text("run_mday", "run_mday_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_mday_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "run_mday_error_format", Fn: checkCronRunMday}),
				text("run_month", "run_month_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_month_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "run_month_error_format", Fn: checkCronRunMonth}),
				text("run_wday", "run_wday_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_wday_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "run_wday_error_format", Fn: checkCronRunWday}),
				checkbox("log", "log_txt", "n"),
				checkbox("active", "active_txt", "y"),
			},
		}},
	}
}

// Schedule / command CUSTOM validators (cron.tform.php validate_cron classes).

func checkCronRunMin(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateRunTime("run_min", value); err != nil {
		return "run_min_error_format"
	}
	return ""
}

func checkCronRunHour(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateRunTime("run_hour", value); err != nil {
		return "run_hour_error_format"
	}
	return ""
}

func checkCronRunMday(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateRunTime("run_mday", value); err != nil {
		return "run_mday_error_format"
	}
	return ""
}

func checkCronRunWday(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateRunTime("run_wday", value); err != nil {
		return "run_wday_error_format"
	}
	return ""
}

func checkCronRunMonth(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateRunMonth(value); err != nil {
		return "run_month_error_format"
	}
	return ""
}

// checkCronCommand ports command_format. {DOMAIN} expands with a dummy
// hostname so field-level validation works before parent resolution; Prepare
// re-checks with the real parent domain when available.
func checkCronCommand(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if err := cron.ValidateCommand(value, "placeholder.example"); err != nil {
		return "command_error_format"
	}
	return ""
}

// sitesCronPrepare ports cron_edit.php onSubmit ownership: parent must be
// a readable vhost; server_id is forced from the parent; parent_domain_id
// is immutable on update; type is auto-derived from command + owner limits.
func sitesCronPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	fields := map[string][]string{}

	var old *model.Cron
	if idParam := c.Param("id"); idParam != "" {
		old = &model.Cron{}
		if err := d.DB.WithContext(ctx).Take(old, idParam).Error; err != nil {
			return err
		}
	}

	pid := bodyInt(body, "parent_domain_id")
	if pid == 0 && old != nil {
		pid = int64(old.ParentDomainID)
	}
	var parent *model.WebDomain
	if pid <= 0 {
		fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_empty")
	} else {
		var err error
		parent, err = loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		if parent.Type != "vhost" {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_invalid")
		}
	}

	if old != nil {
		// parent_domain_id is edit_disabled in PHP cron.tform.php.
		if v := bodyInt(body, "parent_domain_id"); v != 0 && uint32(v) != old.ParentDomainID {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_immutable")
		}
		body["parent_domain_id"] = float64(old.ParentDomainID)
		body["server_id"] = float64(old.ServerID)
	} else if parent != nil {
		body["server_id"] = float64(parent.ServerID)
		body["parent_domain_id"] = float64(parent.DomainID)
	}

	// Domain-aware command check + type auto-derivation (cron_edit.php onSubmit).
	cmd := bodyString(body, "command")
	if cmd == "" && old != nil {
		cmd = old.Command
	}
	domain := ""
	if parent != nil {
		domain = parent.Domain
	}
	if cmd != "" {
		if err := cron.ValidateCommand(cmd, domain); err != nil {
			fields["command"] = append(fields["command"], "command_error_format")
		}
		ownerLimit, adminOwned := sitesCronOwnerLimit(ctx, d.DB, parent)
		body["type"] = cron.DeriveType(cmd, ownerLimit, adminOwned)
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}

	// Client limits (non-admin): count / type / frequency (task 4.3).
	return sitesCronEnforceLimits(ctx, d.DB, id, body, old)
}

// bodyString reads a string JSON body value.
func bodyString(body map[string]any, key string) string {
	v, _ := body[key].(string)
	return v
}

// sitesCronOwnerLimit resolves the parent site's client limit_cron_type
// (sys_group → client). Missing client (admin-owned site) returns adminOwned.
func sitesCronOwnerLimit(ctx context.Context, db *gorm.DB, parent *model.WebDomain) (limitType string, adminOwned bool) {
	if parent == nil || parent.SysGroupID == 0 || parent.SysGroupID == 1 {
		return "", true
	}
	cli, err := clientByGroup(ctx, db, parent.SysGroupID)
	if err != nil || cli == nil || cli.LimitCronType == "" {
		return "", true
	}
	return cli.LimitCronType, false
}

// clientByGroup loads the client row owning a sys_group (nil if none).
func clientByGroup(ctx context.Context, db *gorm.DB, groupID uint32) (*model.Client, error) {
	if groupID == 0 || groupID == 1 {
		return nil, nil
	}
	var cli model.Client
	err := db.WithContext(ctx).Raw(`
		SELECT c.*
		FROM sys_group g
		INNER JOIN client c ON c.client_id = g.client_id
		WHERE g.groupid = ?
		LIMIT 1`, groupID).Scan(&cli).Error
	if err != nil {
		return nil, err
	}
	if cli.ClientID == 0 {
		return nil, nil
	}
	return &cli, nil
}

// sitesCronEnforceLimits ports cron_edit.php limit checks for non-admins:
// limit_cron count (create only), limit_cron_type, limit_cron_frequency.
// Admins bypass. Returns *LimitError (403) on veto.
func sitesCronEnforceLimits(ctx context.Context, db *gorm.DB, id *repository.Identity, body map[string]any, old *model.Cron) error {
	if id == nil || id.IsAdmin() {
		return nil
	}
	cli, err := clientByGroup(ctx, db, id.DefaultGroup)
	if err != nil {
		return err
	}
	if cli == nil {
		return nil
	}

	// Count limit on create only (PHP onSubmit).
	if old == nil && cli.LimitCron >= 0 {
		var n int64
		if err := db.WithContext(ctx).Model(&model.Cron{}).
			Where("sys_groupid = ?", id.DefaultGroup).Count(&n).Error; err != nil {
			return err
		}
		if n >= int64(cli.LimitCron) {
			return &LimitError{Key: "error.limit_cron"}
		}
	}

	// Type limit after auto-derivation (PHP onInsertSave/onUpdateSave).
	typ := bodyString(body, "type")
	if typ == "" && old != nil {
		typ = old.Type
	}
	switch cli.LimitCronType {
	case model.CronTypeURL:
		if typ != "" && typ != model.CronTypeURL {
			return &LimitError{Key: "error.limit_cron_type"}
		}
	case model.CronTypeChrooted:
		if typ == model.CronTypeFull {
			return &LimitError{Key: "error.limit_cron_type"}
		}
	}

	// Frequency floor (PHP cron_min_freq vs limit_cron_frequency).
	if cli.LimitCronFrequency > 1 {
		runMin := bodyString(body, "run_min")
		runHour := bodyString(body, "run_hour")
		runMday := bodyString(body, "run_mday")
		runMonth := bodyString(body, "run_month")
		runWday := bodyString(body, "run_wday")
		if old != nil {
			if runMin == "" {
				runMin = old.RunMin
			}
			if runHour == "" {
				runHour = old.RunHour
			}
			if runMday == "" {
				runMday = old.RunMday
			}
			if runMonth == "" {
				runMonth = old.RunMonth
			}
			if runWday == "" {
				runWday = old.RunWday
			}
		}
		// Incomplete schedules are left to field validators.
		if runMin != "" && runHour != "" && runMday != "" && runMonth != "" && runWday != "" {
			freq, err := cron.MinFrequencyMinutes(runMin, runHour, runMday, runMonth, runWday)
			if err == nil && freq < int(cli.LimitCronFrequency) {
				return &LimitError{Key: "error.limit_cron_frequency"}
			}
		}
	}
	return nil
}

// sitesCronAfterInsert stamps sys_groupid from the parent site before the
// datalog insert is written (PHP onAfterInsert parity).
func sitesCronAfterInsert(_ context.Context, tx *gorm.DB, _ *repository.Identity, recAny any, _ map[string]any) error {
	return sitesCronSyncParent(tx, recAny.(*model.Cron))
}

// sitesCronBeforeUpdate keeps sys_groupid aligned with the (immutable) parent.
func sitesCronBeforeUpdate(_ context.Context, tx *gorm.DB, _ *repository.Identity, _ map[string]any, _, recAny any) error {
	return sitesCronSyncParent(tx, recAny.(*model.Cron))
}

func sitesCronSyncParent(tx *gorm.DB, rec *model.Cron) error {
	if rec.ParentDomainID == 0 {
		return nil
	}
	var web model.WebDomain
	if err := tx.Select("sys_groupid", "server_id").Where("domain_id = ?", rec.ParentDomainID).Take(&web).Error; err != nil {
		return fmt.Errorf("api: loading parent domain %d: %w", rec.ParentDomainID, err)
	}
	rec.SysGroupID = web.SysGroupID
	rec.ServerID = web.ServerID
	return nil
}

// CronRunItem is one page row of GET /api/sites/crons/:id/runs.
type CronRunItem struct {
	// SyslogID is the sys_log primary key.
	SyslogID uint32 `json:"syslog_id"`
	// Loglevel is the daemon log level stored on the row.
	Loglevel int8 `json:"loglevel"`
	// Tstamp is the sys_log unix timestamp.
	Tstamp uint32 `json:"tstamp"`
	// Status is ok|exit|timeout|error from the cron_run message.
	Status string `json:"status"`
	// Exit is the process exit code or HTTP status.
	Exit int `json:"exit"`
	// Start is the run start unix time.
	Start int64 `json:"start"`
	// End is the run end unix time.
	End int64 `json:"end"`
	// Type is the job type at execution time.
	Type string `json:"type"`
	// Output is the bounded output tail.
	Output string `json:"output"`
}

// registerSitesCronEntity mounts /api/sites/crons CRUD plus run history.
func registerSitesCronEntity(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.Cron](g, d, sitesCronEntity()); err != nil {
		return err
	}
	g.GET("/crons/:id/runs", sitesCronRunsHandler(d))
	return nil
}

// sitesCronRunsHandler serves GET /api/sites/crons/:id/runs: paginated
// sys_log rows matching the cron_run id=<id> convention, gated on read
// permission for the parent cron row (task 4.4).
func sitesCronRunsHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		cronID, err := strconv.ParseUint(c.Param("id"), 10, 32)
		if err != nil || cronID == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
		}
		// Gate on read permission for the cron row itself.
		repo, err := repository.New[model.Cron](d.DB)
		if err != nil {
			return err
		}
		var job model.Cron
		if err := repo.Get(c.Request().Context(), id, uint32(cronID), &job); err != nil {
			return err
		}

		page, _ := strconv.Atoi(c.QueryParamOr("page", "1"))
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(c.QueryParamOr("limit", "25"))
		if limit < 1 || limit > 100 {
			limit = 25
		}

		prefix := cron.RunMessagePrefix(uint32(cronID))
		q := d.DB.WithContext(c.Request().Context()).Model(&model.SysLog{}).
			Where("message LIKE ?", prefix+"%")

		var total int64
		if err := q.Count(&total).Error; err != nil {
			return err
		}
		var rows []model.SysLog
		if err := q.Order("syslog_id DESC").
			Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		items := make([]CronRunItem, 0, len(rows))
		for _, row := range rows {
			parsed, ok := cron.ParseRunMessage(row.Message)
			item := CronRunItem{
				SyslogID: row.SyslogID,
				Loglevel: row.Loglevel,
				Tstamp:   row.Tstamp,
				Status:   parsed.Status,
				Exit:     parsed.ExitCode,
				Start:    parsed.StartUnix,
				End:      parsed.EndUnix,
				Type:     parsed.Type,
				Output:   parsed.Output,
			}
			if !ok {
				// Surface the raw message as output when parsing fails.
				item.Output = row.Message
			}
			items = append(items, item)
		}
		return c.JSON(http.StatusOK, cronRunListResponse{
			Items: items, Total: total, Page: page, Limit: limit,
		})
	}
}

// cronRunListResponse mirrors ListResponse for typed run-history rows.
type cronRunListResponse struct {
	Items []CronRunItem `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}
