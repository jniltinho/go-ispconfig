package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// serverRoleFlags maps a role flag column to the objects it may host. The
// map is also the source of the role names accepted by requireTargetServer.
var serverRoleFlags = []string{
	"mail_server", "web_server", "dns_server", "file_server",
	"db_server", "firewall_server", "proxy_server",
}

// serverEntity is the declarative definition of the server entity (port of
// interface/web/admin/form/server.tform.php + server_edit.php). Unlike
// ISPConfig3, which only edits rows the installer created, this entity also
// creates them, so a node can be pre-registered before its installer runs
// (spec server-registry: "Pre-registering a server").
func serverEntity() *Entity {
	fields := []Field{
		{
			Name: "server_name", Label: "server_name_txt",
			Datatype: "VARCHAR", Formtype: "TEXT",
			Validators: []validator.Rule{
				{Type: "NOTEMPTY", ErrKey: "server_name_error_empty"},
				{Type: "REGEX", Regex: `^[a-zA-Z0-9][a-zA-Z0-9.\-]{0,253}[a-zA-Z0-9]$`, ErrKey: "server_name_error_regex"},
				{Type: "UNIQUE", ErrKey: "server_name_error_unique"},
			},
		},
	}
	for _, flag := range serverRoleFlags {
		fields = append(fields, Field{
			Name: flag, Label: flag + "_txt", Datatype: "INTEGER", Formtype: "CHECKBOX",
			Default: 0,
			Options: []Option{{Value: "0", Label: "no_txt"}, {Value: "1", Label: "yes_txt"}},
		})
	}
	fields = append(fields,
		selectField("mirror_server_id", "mirror_server_id_txt", "INTEGER", 0, nil),
		Field{
			Name: "active", Label: "active_txt", Datatype: "INTEGER", Formtype: "CHECKBOX",
			// Pre-registered rows start inactive: the installer on the node
			// activates them once the software is actually there.
			Default: 0,
			Options: []Option{{Value: "0", Label: "no_txt"}, {Value: "1", Label: "yes_txt"}},
		},
	)

	return &Entity{
		Name:         "server",
		Title:        "server_edit_title",
		Policy:       "admin_allow_server_services",
		AdminOnly:    true,
		Prepare:      serverPrepare,
		BeforeDelete: serverBeforeDelete,
		Tabs:         []Tab{{Name: "server", Label: "server_tab_title", Fields: fields}},
	}
}

// serverPrepare normalizes a submitted server row: the name is lowercased,
// and mirror_server_id is coerced to 0 for the illegal targets ISPConfig3
// also silently drops (self-mirror and server 1 —
// interface/web/admin/server_edit.php:78), plus mirroring a mirror.
func serverPrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	if v, ok := body["server_name"].(string); ok {
		body["server_name"] = strings.ToLower(strings.TrimSpace(v))
	}
	// server_id is assigned by AUTO_INCREMENT only; a supplied value would
	// otherwise let a caller overwrite an existing row on create.
	delete(body, "server_id")

	mirror := bodyInt(body, "mirror_server_id")
	if mirror <= 0 {
		return nil
	}
	self, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if mirror == self || self == 1 {
		body["mirror_server_id"] = float64(0)
		return nil
	}
	// A mirror of a mirror has no meaning: the source must be a real server.
	var target model.Server
	err := d.DB.WithContext(c.Request().Context()).
		Select("server_id", "mirror_server_id").Where("server_id = ?", mirror).First(&target).Error
	if err != nil || target.MirrorServerID != 0 {
		body["mirror_server_id"] = float64(0)
	}
	return nil
}

// serverReferences lists the tables whose rows pin a server row in place.
// Deleting the server would orphan them on a node that no longer exists
// (intent of interface/web/admin/server_del.php).
var serverReferences = []string{
	"web_domain", "mail_domain", "dns_soa", "dns_slave", "web_database",
	"cron", "firewall", "shell_user", "ftp_user", "server_php",
}

// serverBeforeDelete refuses the delete while anything still points at the
// server, naming the blocking tables. server_ip rows are the exception:
// they belong to the server and are removed with it.
func serverBeforeDelete(ctx context.Context, tx *gorm.DB, _ *repository.Identity, rec any) error {
	srv, ok := rec.(*model.Server)
	if !ok {
		return fmt.Errorf("api: server delete got %T", rec)
	}

	var blocking []string
	for _, table := range serverReferences {
		var n int64
		// table is a compile-time constant from serverReferences.
		if err := tx.WithContext(ctx).Table(table).
			Where("server_id = ?", srv.ServerID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			blocking = append(blocking, fmt.Sprintf("%s (%d)", table, n))
		}
	}
	var mirrors int64
	if err := tx.WithContext(ctx).Model(&model.Server{}).
		Where("mirror_server_id = ?", srv.ServerID).Count(&mirrors).Error; err != nil {
		return err
	}
	if mirrors > 0 {
		blocking = append(blocking, fmt.Sprintf("mirror_server_id (%d)", mirrors))
	}
	if len(blocking) > 0 {
		// A ValidationError is the only error shape that carries detail to
		// the client, so the blocking references ride along with the key.
		return &ValidationError{Fields: map[string][]string{
			"server_id": append([]string{"server_still_referenced"}, blocking...),
		}}
	}

	return tx.WithContext(ctx).Where("server_id = ?", srv.ServerID).
		Delete(&model.ServerIP{}).Error
}

// requireTargetServer builds a Prepare hook validating that the body's
// server_id names a server that exists, is active, is not a mirror and
// carries the role flag the object needs (spec server-registry: "Shared
// target-server validation on server_id inputs"). server_id is mandatory on
// create; on update it is only checked when the body carries it, since a
// partial update inherits the stored value.
//
// Broadcast rows (web_database_user, server_id = 0) must not use this hook.
func requireTargetServer(role string) func(*echo.Context, *Deps, map[string]any) error {
	return func(c *echo.Context, d *Deps, body map[string]any) error {
		_, present := body["server_id"]
		sid := bodyInt(body, "server_id")
		if !present && c.Request().Method != http.MethodPost {
			return nil
		}
		if sid <= 0 {
			return &ValidationError{Fields: map[string][]string{"server_id": {"server_id_error_empty"}}}
		}
		var n int64
		// role comes from serverRoleFlags, never from the request.
		err := d.DB.WithContext(c.Request().Context()).Model(&model.Server{}).
			Where("server_id = ? AND active = 1 AND mirror_server_id = 0 AND "+role+" = 1", sid).
			Count(&n).Error
		if err != nil {
			return err
		}
		if n == 0 {
			return &ValidationError{Fields: map[string][]string{"server_id": {"server_id_error_empty"}}}
		}
		return nil
	}
}

// activeTargetServers scopes a query to the servers a user may pick as a
// target: active, not a mirror (mirrors are configured from their source).
func activeTargetServers(q *gorm.DB) *gorm.DB {
	return q.Where("active = 1 AND mirror_server_id = 0")
}
