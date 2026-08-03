package api

import (
	"context"
	"fmt"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// sitesCronEntity is the /api/sites/crons surface (add-cron-module task 4.1):
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
					validator.Rule{Type: "NOTEMPTY", ErrKey: "command_error_empty"}),
				text("run_min", "run_min_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_min_error_empty"}),
				text("run_hour", "run_hour_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_hour_error_empty"}),
				text("run_mday", "run_mday_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_mday_error_empty"}),
				text("run_month", "run_month_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_month_error_empty"}),
				text("run_wday", "run_wday_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "run_wday_error_empty"}),
				checkbox("log", "log_txt", "n"),
				checkbox("active", "active_txt", "y"),
			},
		}},
	}
}

// sitesCronPrepare ports cron_edit.php onSubmit ownership: parent must be
// a readable vhost; server_id is forced from the parent; parent_domain_id
// is immutable on update.
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

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
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

// registerSitesCronEntity mounts /api/sites/crons.
func registerSitesCronEntity(g *echo.Group, d *Deps) error {
	return RegisterEntity[model.Cron](g, d, sitesCronEntity())
}
