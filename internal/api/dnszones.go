package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// This file implements the zone endpoints beyond the generic entity CRUD
// (spec dns-rest-api "Zone endpoints"): id lookup by origin
// (dns_zone_get_id), status toggle (dns_zone_set_status) and DNSSEC toggle
// (dns_zone_set_dnssec).

// registerDNSZoneRoutes mounts the extra zone routes under /dns.
func registerDNSZoneRoutes(g *echo.Group, d *Deps) {
	g.GET("/zones/origin/:origin", dnsZoneIDByOrigin(d))
	g.POST("/zones/:id/status", dnsZoneSetStatus(d))
	g.POST("/zones/:id/dnssec", dnsZoneSetDNSSEC(d))
}

// DNSZoneIDResponse is the id-by-origin lookup response.
type DNSZoneIDResponse struct {
	// ID is the dns_soa primary key.
	ID uint32 `json:"id"`
}

// dnsZoneIDByOrigin implements GET /api/dns/zones/origin/{origin}.
//
//	@Summary		Zone id by origin
//	@Description	Resolves an origin (with or without trailing dot) to the id of an accessible zone (port of dns_zone_get_id). 404 when no accessible zone matches.
//	@Tags			dns
//	@Produce		json
//	@Param			origin	path		string	true	"Zone origin"	example(example.com)
//	@Success		200		{object}	DNSZoneIDResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse	"No accessible zone with this origin"
//	@Router			/dns/zones/origin/{origin} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneIDByOrigin(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		origin := strings.ToLower(strings.TrimSuffix(c.Param("origin"), ".")) + "."
		var soa model.DNSSoa
		err := d.DB.WithContext(c.Request().Context()).
			Scopes(repository.WithPerm(id, repository.PermRead)).
			Where("origin = ?", origin).First(&soa).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "zone not found")
		}
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, DNSZoneIDResponse{ID: soa.ID})
	}
}

// dnsZoneMutate loads the zone under the caller's u-scope, applies mutate,
// bumps the serial and saves through the permission-checked repository
// (datalog diff included). mutate returns a *ValidationError to veto.
func dnsZoneMutate(c *echo.Context, d *Deps, mutate func(soa *model.DNSSoa) error) error {
	id := identity(c)
	if id == nil {
		return repository.ErrPermissionDenied
	}
	ctx := c.Request().Context()
	repo, err := repository.New[model.DNSSoa](d.DB)
	if err != nil {
		return err
	}
	var soa model.DNSSoa
	if err := repo.Get(ctx, id, c.Param("id"), &soa); err != nil {
		return err
	}
	if ok, err := repo.CheckPerm(ctx, id, soa.ID, repository.PermUpdate); err != nil {
		return err
	} else if !ok {
		return repository.ErrPermissionDenied
	}
	before := soa
	if err := mutate(&soa); err != nil {
		return err
	}
	if soa == before {
		return c.JSON(http.StatusOK, modelToMap(ctx, d.DB, &soa))
	}
	soa.Serial = NextSerial(soa.Serial, time.Now())
	if err := repo.Update(ctx, id, &soa); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, modelToMap(ctx, d.DB, &soa))
}

// DNSZoneStatusRequest is the status toggle body.
type DNSZoneStatusRequest struct {
	// Status is "active" or "inactive" (dns_zone_set_status semantics).
	Status string `json:"status"`
}

// dnsZoneSetStatus implements POST /api/dns/zones/{id}/status.
//
//	@Summary		Activate or deactivate a zone
//	@Description	Port of dns_zone_set_status: sets dns_soa.active to Y/N, bumps the serial and journals the change; the daemon then adds/removes the zone from named.conf.local.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"zone id"
//	@Param			request	body		DNSZoneStatusRequest	true	"active or inactive"
//	@Success		200		{object}	model.DNSSoa
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/dns/zones/{id}/status [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneSetStatus(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		req := new(DNSZoneStatusRequest)
		if err := c.Bind(req); err != nil {
			return err
		}
		return dnsZoneMutate(c, d, func(soa *model.DNSSoa) error {
			switch strings.ToLower(req.Status) {
			case "active":
				soa.Active = "Y"
			case "inactive":
				soa.Active = "N"
			default:
				return &ValidationError{Fields: map[string][]string{"status": {"status_error_invalid"}}}
			}
			return nil
		})
	}
}

// DNSZoneDNSSECRequest is the DNSSEC toggle body.
type DNSZoneDNSSECRequest struct {
	// Wanted is Y/N (dns_soa.dnssec_wanted).
	Wanted string `json:"dnssec_wanted"`
	// Algo optionally replaces dnssec_algo (CSV subset of
	// NSEC3RSASHA1/ECDSAP256SHA256).
	Algo string `json:"dnssec_algo"`
}

// dnsZoneSetDNSSEC implements POST /api/dns/zones/{id}/dnssec.
//
//	@Summary		Toggle DNSSEC on a zone
//	@Description	Port of dns_zone_set_dnssec: sets dnssec_wanted (Y/N) and optionally dnssec_algo, bumps the serial and journals the change; the daemon then runs the key/sign lifecycle.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"zone id"
//	@Param			request	body		DNSZoneDNSSECRequest	true	"dnssec_wanted Y/N and optional dnssec_algo"
//	@Success		200		{object}	model.DNSSoa
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/dns/zones/{id}/dnssec [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsZoneSetDNSSEC(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		req := new(DNSZoneDNSSECRequest)
		if err := c.Bind(req); err != nil {
			return err
		}
		return dnsZoneMutate(c, d, func(soa *model.DNSSoa) error {
			switch strings.ToUpper(req.Wanted) {
			case "Y":
				soa.DNSSECWanted = "Y"
			case "N":
				soa.DNSSECWanted = "N"
			default:
				return &ValidationError{Fields: map[string][]string{"dnssec_wanted": {"dnssec_wanted_error_invalid"}}}
			}
			if req.Algo != "" {
				if key, _ := dnssecAlgoRule().Validate(nil, req.Algo); key != "" {
					return &ValidationError{Fields: map[string][]string{"dnssec_algo": {key}}}
				}
				soa.DNSSECAlgo = req.Algo
			}
			return nil
		})
	}
}
