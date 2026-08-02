package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// This file implements the DNS record endpoints (spec dns-rest-api "Record
// endpoints for all supported types"): a single typed surface over dns_rr
// where every mutation is validated by the per-type metadata
// (dnsrrtypes.go), inherits the zone's server and ownership, bumps the SOA
// serial inside the same transaction (SELECT ... FOR UPDATE, design D7) and
// journals {old,new} datalog rows for both the record and the SOA.

// registerDNSRecordRoutes mounts the record CRUD under /dns.
func registerDNSRecordRoutes(g *echo.Group, d *Deps) {
	g.GET("/zones/:id/records", listDNSRecords(d))
	g.POST("/zones/:id/records", createDNSRecord(d))
	g.PUT("/records/:id", updateDNSRecord(d))
	g.DELETE("/records/:id", deleteDNSRecord(d))
}

// dnsTxn runs fn in a transaction with the datalog after-commit notifier
// (mirror of repository.txn for the multi-row DNS operations).
func dnsTxn(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	ctx, flush := datalog.NotifyAfterCommit(ctx)
	if err := db.WithContext(ctx).Transaction(fn); err != nil {
		return err
	}
	flush()
	return nil
}

// bumpZoneSerial locks the SOA row (SELECT ... FOR UPDATE), advances its
// serial via NextSerial and journals the change, all inside tx.
func bumpZoneSerial(tx *gorm.DB, zoneID uint32, username string) error {
	var soa model.DNSSoa
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", zoneID).First(&soa).Error
	if err != nil {
		return err
	}
	old := soa
	soa.Serial = NextSerial(soa.Serial, time.Now())
	if err := tx.Model(&model.DNSSoa{}).Where("id = ?", zoneID).
		Update("serial", soa.Serial).Error; err != nil {
		return err
	}
	return datalog.LogUpdate(tx, &old, &soa, username)
}

// modelToMap renders a GORM model as a JSON object keyed by DB column.
func modelToMap(ctx context.Context, db *gorm.DB, rec any) map[string]any {
	s, err := schema.Parse(rec, entitySchemaCache, db.NamingStrategy)
	if err != nil {
		return nil
	}
	out := make(map[string]any, len(s.Fields))
	rv := reflect.ValueOf(rec)
	for _, f := range s.Fields {
		v, _ := f.ValueOf(ctx, rv)
		out[f.DBName] = v
	}
	return out
}

// updateSerialWanted reads the update_serial flag (default true,
// remote-API parity) from the JSON body or the query string.
func updateSerialWanted(c *echo.Context, body map[string]any) bool {
	if body != nil {
		if v, ok := body["update_serial"].(bool); ok {
			return v
		}
	}
	if q := c.QueryParam("update_serial"); q != "" {
		if v, err := strconv.ParseBool(q); err == nil {
			return v
		}
	}
	return true
}

// loadZoneForUpdate verifies the caller may update the zone and returns it.
// A zone outside the caller's u-scope yields ErrPermissionDenied (403).
func loadZoneForUpdate(c *echo.Context, d *Deps, id *repository.Identity, zoneID any) (*model.DNSSoa, error) {
	var soa model.DNSSoa
	err := d.DB.WithContext(c.Request().Context()).
		Scopes(repository.WithPerm(id, repository.PermUpdate)).
		Where("id = ?", zoneID).First(&soa).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrPermissionDenied
	}
	if err != nil {
		return nil, err
	}
	return &soa, nil
}

// mergeRecordBody applies the submitted JSON fields onto rr and returns the
// resolved type descriptor. Absent fields keep their current values; type
// defaults to the stored dns_rr type.
func mergeRecordBody(rr *model.DNSRr, body map[string]any) (*DNSRecordType, error) {
	typeName := rr.Type
	if v, ok := body["type"].(string); ok && v != "" {
		typeName = v
	}
	rt, ok := dnsRecordTypeByName(typeName)
	if !ok {
		return nil, &ValidationError{Fields: map[string][]string{"type": {"type_error_unknown"}}}
	}
	rr.Type = rt.StoredType
	if _, ok := body["name"]; ok {
		idnLower(body, "name")
		rr.Name, _ = body["name"].(string)
	}
	if v, ok := body["data"].(string); ok {
		rr.Data = v
	}
	if _, ok := body["aux"]; ok {
		rr.Aux = uint32(bodyInt(body, "aux"))
	}
	if _, ok := body["ttl"]; ok {
		rr.TTL = uint32(bodyInt(body, "ttl"))
	}
	if v, ok := body["active"].(string); ok {
		rr.Active = v
	}
	if rr.Active != "N" {
		rr.Active = "Y"
	}
	return rt, nil
}

// listDNSRecords implements GET /api/dns/zones/{id}/records.
//
//	@Summary		List the records of a zone
//	@Description	All dns_rr rows of an accessible zone ordered by type then name (port of dns_rr_get_all_by_zone).
//	@Tags			dns
//	@Produce		json
//	@Param			id	path		int	true	"zone id"
//	@Success		200	{array}		model.DNSRr
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Zone not accessible"
//	@Router			/dns/zones/{id}/records [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func listDNSRecords(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		ctx := c.Request().Context()
		// The zone itself must be readable; the records inherit its
		// ownership so no extra per-row filter is needed.
		zoneRepo, err := repository.New[model.DNSSoa](d.DB)
		if err != nil {
			return err
		}
		var soa model.DNSSoa
		if err := zoneRepo.Get(ctx, id, c.Param("id"), &soa); err != nil {
			return err
		}
		var recs []model.DNSRr
		if err := d.DB.WithContext(ctx).
			Where("zone = ?", soa.ID).
			Order("type, name").Find(&recs).Error; err != nil {
			return err
		}
		items := make([]map[string]any, len(recs))
		for i := range recs {
			items[i] = modelToMap(ctx, d.DB, &recs[i])
		}
		return c.JSON(http.StatusOK, items)
	}
}

// createDNSRecord implements POST /api/dns/zones/{id}/records.
//
//	@Summary		Add a record to a zone
//	@Description	Creates a dns_rr row of the given type (A, AAAA, ALIAS, CAA, CNAME, DNAME, DS, HINFO, LOC, MX, NAPTR, NS, PTR, RP, SRV, SSHFP, TLSA, TXT or the TXT-derived SPF/DKIM/DMARC helpers), validated by the per-type metadata. The record inherits the zone's server and ownership; the SOA serial is bumped in the same transaction unless update_serial=false, and datalog rows are journaled for the record and the SOA.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"zone id"
//	@Param			record	body		model.DNSRr		true	"type, name, data, aux, ttl, active plus the optional update_serial flag"
//	@Success		201		{object}	model.DNSRr
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Zone not updatable by the caller"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/zones/{id}/records [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func createDNSRecord(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		if id == nil {
			return repository.ErrPermissionDenied
		}
		body := map[string]any{}
		if err := c.Bind(&body); err != nil {
			return err
		}
		zone, err := loadZoneForUpdate(c, d, id, c.Param("id"))
		if err != nil {
			return err
		}

		rr := &model.DNSRr{
			ServerID: zone.ServerID,
			Zone:     zone.ID,
			// Records always share their zone's ownership so zone and
			// records stay visible to the same users.
			SysUserID: zone.SysUserID, SysGroupID: zone.SysGroupID,
			SysPermUser: zone.SysPermUser, SysPermGroup: zone.SysPermGroup,
			SysPermOther: zone.SysPermOther,
			Active:       "Y",
		}
		rt, err := mergeRecordBody(rr, body)
		if err != nil {
			return err
		}
		if _, ok := body["aux"]; !ok {
			rr.Aux = rt.AuxDefault
		}
		if _, ok := body["ttl"]; !ok {
			rr.TTL = rt.TTLDefault
		}
		if fields := validateDNSRecord(rt, rr.Name, rr.Data, int64(rr.Aux), int64(rr.TTL)); len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}

		ctx := c.Request().Context()
		err = dnsTxn(ctx, d.DB, func(tx *gorm.DB) error {
			if err := tx.Create(rr).Error; err != nil {
				return err
			}
			if err := datalog.LogInsert(tx, rr, id.Username); err != nil {
				return err
			}
			if updateSerialWanted(c, body) {
				return bumpZoneSerial(tx, zone.ID, id.Username)
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, modelToMap(ctx, d.DB, rr))
	}
}

// updateDNSRecord implements PUT /api/dns/records/{id}.
//
//	@Summary		Update a DNS record
//	@Description	Merges the submitted fields over the stored dns_rr row, revalidates them against the per-type metadata and saves under the u-flag permission scope, bumping the SOA serial in the same transaction (unless update_serial=false) with datalog rows for both.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int			true	"record id"
//	@Param			record	body		model.DNSRr	true	"Changed field values plus the optional update_serial flag"
//	@Success		200		{object}	model.DNSRr
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Record not accessible"
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/records/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func updateDNSRecord(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		if id == nil {
			return repository.ErrPermissionDenied
		}
		body := map[string]any{}
		if err := c.Bind(&body); err != nil {
			return err
		}
		ctx := c.Request().Context()

		var out map[string]any
		err := dnsTxn(ctx, d.DB, func(tx *gorm.DB) error {
			var old model.DNSRr
			err := tx.Scopes(repository.WithPerm(id, repository.PermUpdate)).
				Where("id = ?", c.Param("id")).First(&old).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrPermissionDenied
			}
			if err != nil {
				return err
			}
			rr := old
			rt, err := mergeRecordBody(&rr, body)
			if err != nil {
				return err
			}
			if fields := validateDNSRecord(rt, rr.Name, rr.Data, int64(rr.Aux), int64(rr.TTL)); len(fields) > 0 {
				return &ValidationError{Fields: fields}
			}
			if reflect.DeepEqual(&old, &rr) {
				out = modelToMap(ctx, d.DB, &rr)
				return nil // nothing changed: no UPDATE, no datalog, no bump
			}
			if err := tx.Model(&model.DNSRr{}).Where("id = ?", old.ID).
				Select("type", "name", "data", "aux", "ttl", "active").
				Updates(&rr).Error; err != nil {
				return err
			}
			if err := datalog.LogUpdate(tx, &old, &rr, id.Username); err != nil {
				return err
			}
			if updateSerialWanted(c, body) {
				if err := bumpZoneSerial(tx, rr.Zone, id.Username); err != nil {
					return err
				}
			}
			out = modelToMap(ctx, d.DB, &rr)
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

// deleteDNSRecord implements DELETE /api/dns/records/{id}.
//
//	@Summary		Delete a DNS record
//	@Description	Removes the dns_rr row under the d-flag permission scope, journals the delete and bumps the SOA serial in the same transaction (unless ?update_serial=false).
//	@Tags			dns
//	@Param			id				path	int		true	"record id"
//	@Param			update_serial	query	bool	false	"Bump the SOA serial (default true)"
//	@Success		204	"Deleted"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Record not accessible"
//	@Router			/dns/records/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func deleteDNSRecord(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		if id == nil {
			return repository.ErrPermissionDenied
		}
		err := dnsTxn(c.Request().Context(), d.DB, func(tx *gorm.DB) error {
			var old model.DNSRr
			err := tx.Scopes(repository.WithPerm(id, repository.PermDelete)).
				Where("id = ?", c.Param("id")).First(&old).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrPermissionDenied
			}
			if err != nil {
				return err
			}
			if err := tx.Where("id = ?", old.ID).Delete(&model.DNSRr{}).Error; err != nil {
				return err
			}
			if err := datalog.LogDelete(tx, &old, id.Username); err != nil {
				return err
			}
			if updateSerialWanted(c, nil) {
				return bumpZoneSerial(tx, old.Zone, id.Username)
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}
