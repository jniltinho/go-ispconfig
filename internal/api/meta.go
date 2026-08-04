package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// FormFieldMeta is one field in a form metadata response: rendering hints
// for the SPA TabbedForm plus the validator rules applied by the API.
type FormFieldMeta struct {
	// Name is the DB column and JSON key of the field.
	Name string `json:"name"`
	// Label is the i18n label key.
	Label string `json:"label"`
	// Type is the SPA control type (text, password, textarea, select,
	// checkbox), derived from the tform formtype.
	Type string `json:"type"`
	// Datatype is the tform datatype (INTEGER, VARCHAR, ...).
	Datatype string `json:"datatype"`
	// Formtype is the original tform form control name.
	Formtype string `json:"formtype"`
	// Default is the value applied when a create omits the field.
	Default any `json:"default,omitempty"`
	// Options lists the allowed values of select/checkbox fields.
	Options []Option `json:"options,omitempty"`
	// Validators are the declarative rules the API enforces.
	Validators []validator.Rule `json:"validators,omitempty"`
	// Collapsible marks a legend whose section renders folded.
	Collapsible bool `json:"collapsible,omitempty"`
}

// FormTabMeta is one tab in a form metadata response.
type FormTabMeta struct {
	// Name identifies the tab.
	Name string `json:"name"`
	// Label is the i18n label key.
	Label string `json:"label"`
	// Fields are the fields rendered on the tab.
	Fields []FormFieldMeta `json:"fields"`
}

// FormMeta is the GET /api/meta/forms/{entity} response: the tabs, fields,
// defaults and validator hints of a registered entity — the same source of
// truth the API validates against (design D5).
type FormMeta struct {
	// Name is the entity name.
	Name string `json:"name"`
	// Title is the i18n title key of the form.
	Title string `json:"title"`
	// Tabs are the form tabs.
	Tabs []FormTabMeta `json:"tabs"`
}

// formTypes maps tform formtypes to SPA TabbedForm control types.
var formTypes = map[string]string{
	"TEXT":     "text",
	"PASSWORD": "password",
	"TEXTAREA": "textarea",
	"SELECT":   "select",
	"CHECKBOX": "checkbox",
	"RADIO":    "select",
}

// registerMetaRoutes mounts the form metadata endpoint on the
// authenticated group.
func registerMetaRoutes(g *echo.Group, d *Deps) {
	g.GET("/meta/forms/:entity", formMetaHandler())
	// Shared select datasources for forms whose SELECT options are not
	// static in the entity definition (server list, …).
	g.GET("/meta/lookups/servers", serversLookupHandler(d))
	g.GET("/meta/lookups/client-groups", clientGroupsLookupHandler(d))
	g.GET("/meta/lookups/server-ips", serverIPsLookupHandler(d))
	g.GET("/meta/lookups/spamfilter-policies", spamfilterPoliciesLookupHandler(d))
}

// spamfilterPoliciesLookupHandler returns the readable spamfilter policies
// as {value,label} options for the mail domain Spamfilter select. The
// policies entity is admin-only, but a client still has to see the
// policies it may assign (legacy getAuthSQL('r') datasource).
//
//	@Summary		Spamfilter policy select options
//	@Description	Readable rows from spamfilter_policy as value/label pairs for the mail domain Spamfilter select.
//	@Tags			meta
//	@Produce		json
//	@Success		200	{array}		Option
//	@Failure		401	{object}	ErrorResponse
//	@Router			/meta/lookups/spamfilter-policies [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func spamfilterPoliciesLookupHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.SpamfilterPolicy
		err := d.DB.WithContext(c.Request().Context()).Model(&model.SpamfilterPolicy{}).
			Scopes(repository.WithPerm(identity(c), repository.PermRead)).
			Select("id", "policy_name").Order("policy_name").Find(&rows).Error
		if err != nil {
			return err
		}
		out := make([]Option, 0, len(rows))
		for _, r := range rows {
			out = append(out, Option{Value: strconv.FormatUint(uint64(r.ID), 10), Label: r.PolicyName})
		}
		return c.JSON(http.StatusOK, out)
	}
}

// clientGroupRow is one row of the client-groups lookup join.
type clientGroupRow struct {
	GroupID     uint32 `gorm:"column:groupid"`
	CompanyName string `gorm:"column:company_name"`
	ContactName string `gorm:"column:contact_name"`
	Username    string `gorm:"column:username"`
}

// clientGroupsLookupHandler returns the client sys_groups as {value,label}
// options for admin client-assignment selects (tform client_group_id
// datasource: sys_group joined to client). Non-admin sessions get an empty
// list — the SPA only renders the field for admins anyway.
//
//	@Summary		Client group select options
//	@Description	sys_group rows of clients (client_id > 0) as value/label pairs, label "company :: contact (username)". Empty for non-admin sessions.
//	@Tags			meta
//	@Produce		json
//	@Success		200	{array}		Option
//	@Failure		401	{object}	ErrorResponse
//	@Router			/meta/lookups/client-groups [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func clientGroupsLookupHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		out := []Option{}
		sess := auth.FromContext(c)
		if sess == nil || sess.Typ != "admin" {
			return c.JSON(http.StatusOK, out)
		}
		var rows []clientGroupRow
		err := d.DB.WithContext(c.Request().Context()).
			Table("sys_group").
			Select("sys_group.groupid, client.company_name, client.contact_name, client.username").
			Joins("JOIN client ON client.client_id = sys_group.client_id").
			Where("sys_group.client_id > 0").
			Order("client.company_name, client.contact_name").
			Scan(&rows).Error
		if err != nil {
			return err
		}
		for _, r := range rows {
			label := r.ContactName
			if r.Username != "" {
				label += " (" + r.Username + ")"
			}
			if r.CompanyName != "" {
				label = r.CompanyName + " :: " + label
			}
			out = append(out, Option{Value: strconv.FormatUint(uint64(r.GroupID), 10), Label: label})
		}
		return c.JSON(http.StatusOK, out)
	}
}

// ServerIPOption is one row of the server-ips lookup: a vhost-enabled IP
// with its server and type so the SPA can split IPv4/IPv6 selects.
type ServerIPOption struct {
	ServerID  uint32 `json:"server_id"`
	IPType    string `json:"ip_type" example:"IPv4"`
	IPAddress string `json:"ip_address" example:"10.0.0.1"`
}

// serverIPsLookupHandler returns the vhost-enabled server IPs for the
// ip_address / ipv6_address form selects (legacy web_vhost_domain_edit IP
// datasource).
//
//	@Summary		Server IP select options
//	@Description	Vhost-enabled rows from server_ip with server id and IPv4/IPv6 type. Non-admin sessions only see shared IPs (client_id = 0).
//	@Tags			meta
//	@Produce		json
//	@Success		200	{array}		ServerIPOption
//	@Failure		401	{object}	ErrorResponse
//	@Router			/meta/lookups/server-ips [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serverIPsLookupHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		q := d.DB.WithContext(c.Request().Context()).
			Table("server_ip").
			Select("server_id", "ip_type", "ip_address").
			Where("virtualhost = ?", "y").
			Order("ip_address")
		sess := auth.FromContext(c)
		if sess == nil || sess.Typ != "admin" {
			// ponytail: non-admins only see shared IPs; per-client IPs need
			// the session client id, add when client-owned IPs land.
			q = q.Where("client_id = 0")
		}
		rows := []ServerIPOption{}
		if err := q.Scan(&rows).Error; err != nil {
			return err
		}
		return c.JSON(http.StatusOK, rows)
	}
}

// serversLookupHandler returns active servers as {value,label} options for
// SPA select overrides (server_id fields). Labels are hostnames, not i18n keys.
//
//	@Summary		Server select options
//	@Description	Active rows from the server table as value/label pairs for form server_id selects.
//	@Tags			meta
//	@Produce		json
//	@Success		200	{array}		Option
//	@Failure		401	{object}	ErrorResponse
//	@Router			/meta/lookups/servers [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serversLookupHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.Server
		err := d.DB.WithContext(c.Request().Context()).
			Select("server_id", "server_name").
			// Mirrors are configured from their source server, so they are
			// never a valid target (spec server-registry: pickers exclude
			// mirrors and inactive servers).
			Scopes(activeTargetServers).
			Order("server_id").
			Find(&rows).Error
		if err != nil {
			return err
		}
		out := make([]Option, 0, len(rows))
		for _, r := range rows {
			out = append(out, Option{
				Value: strconv.FormatUint(uint64(r.ServerID), 10),
				Label: r.ServerName,
			})
		}
		return c.JSON(http.StatusOK, out)
	}
}

// formMetaHandler implements GET /api/meta/forms/{entity}. AdminOnly tabs
// and fields are omitted for non-admin sessions (tform admin-only tabs), so
// clients render exactly the form the API accepts from them.
//
//	@Summary		Form metadata of an entity
//	@Description	Returns the tabs, fields, types, defaults and validator hints of a registered entity so the SPA renders its form from the same source of truth used for validation. Admin-only tabs/fields are omitted for non-admin sessions.
//	@Tags			meta
//	@Produce		json
//	@Param			entity	path		string	true	"Entity name"	example(server_ip)
//	@Success		200		{object}	FormMeta
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse	"Unknown entity"
//	@Router			/meta/forms/{entity} [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func formMetaHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		ent, ok := lookupEntity(c.Param("entity"))
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "unknown entity")
		}
		sess := auth.FromContext(c)
		admin := sess != nil && sess.Typ == "admin"
		meta := FormMeta{Name: ent.Name, Title: ent.Title, Tabs: []FormTabMeta{}}
		for _, tab := range ent.Tabs {
			if tab.AdminOnly && !admin {
				continue
			}
			fields := []FormFieldMeta{}
			for _, f := range tab.Fields {
				if f.AdminOnly && !admin {
					continue
				}
				typ, ok := formTypes[f.Formtype]
				if !ok {
					typ = strings.ToLower(f.Formtype)
				}
				fields = append(fields, FormFieldMeta{
					Name:       f.Name,
					Label:      f.Label,
					Type:       typ,
					Datatype:   f.Datatype,
					Formtype:   f.Formtype,
					Default:    f.Default,
					Options:     f.Options,
					Validators:  f.Validators,
					Collapsible: f.Collapsible,
				})
			}
			meta.Tabs = append(meta.Tabs, FormTabMeta{Name: tab.Name, Label: tab.Label, Fields: fields})
		}
		return c.JSON(http.StatusOK, meta)
	}
}
