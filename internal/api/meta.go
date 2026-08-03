package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
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
}

// serversLookupHandler returns active servers as {value,label} options for
// SPA select overrides (server_id fields). Labels are hostnames, not i18n keys.
//
//	@Summary		Server select options
//	@Description	Active rows from the server table as value/label pairs for form server_id selects.
//	@Tags			meta
//	@Produce		json
//	@Success		200	{array}	Option
//	@Failure		401	{object}	ErrorResponse
//	@Router			/meta/lookups/servers [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func serversLookupHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.Server
		err := d.DB.WithContext(c.Request().Context()).
			Select("server_id", "server_name").
			Where("active = ?", 1).
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
					Options:    f.Options,
					Validators: f.Validators,
				})
			}
			meta.Tabs = append(meta.Tabs, FormTabMeta{Name: tab.Name, Label: tab.Label, Fields: fields})
		}
		return c.JSON(http.StatusOK, meta)
	}
}
