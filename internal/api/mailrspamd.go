package api

// Rspamd policy surface (spec rspamd-ui-actions): the global score
// thresholds plus global white/blacklist of one mail server, and the
// per-domain score override. Nothing here touches /etc/rspamd — every
// write goes to the database and sys_datalog, and the rspamd plugin
// (internal/mail/rspamd.go) renders the files on the owning node.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// Access values of the two global list flavours (Postfix access syntax,
// consumed by the rspamd wblist handler).
const (
	accessWhitelist = "OK"
	accessBlacklist = "REJECT"
)

// RspamdPolicy is the global Rspamd policy of one mail server: the score
// thresholds rendered into local.d/actions.conf and the global
// white/blacklist held as mail_access sender rows.
type RspamdPolicy struct {
	// ServerID is the mail server the policy applies to.
	ServerID uint32 `json:"server_id"`
	// SpamTagLevel is the add_header threshold.
	SpamTagLevel float64 `json:"spam_tag_level"`
	// SpamKillLevel is the reject threshold.
	SpamKillLevel float64 `json:"spam_kill_level"`
	// GreylistingLevel is the greylist threshold.
	GreylistingLevel float64 `json:"greylisting_level"`
	// Whitelist holds the senders always accepted.
	Whitelist []string `json:"whitelist"`
	// Blacklist holds the senders always rejected.
	Blacklist []string `json:"blacklist"`
}

// RspamdDomainPolicy is the per-domain score override, backed by a
// dedicated spamfilter_policy row bound to an "@domain" spamfilter user.
type RspamdDomainPolicy struct {
	// Domain is the mail domain the scores apply to.
	Domain string `json:"domain"`
	// SpamTagLevel is the add_header threshold.
	SpamTagLevel float64 `json:"spam_tag_level"`
	// SpamKillLevel is the reject threshold.
	SpamKillLevel float64 `json:"spam_kill_level"`
	// Inherited is true when no per-domain override exists and the
	// returned scores are the server-wide ones.
	Inherited bool `json:"inherited"`
}

// registerRspamdRoutes mounts /api/mail/rspamd/*.
func registerRspamdRoutes(g *echo.Group, d *Deps) {
	g.GET("/rspamd/policy", rspamdPolicyGet(d), requireAdmin)
	g.PUT("/rspamd/policy", rspamdPolicySave(d), requireAdmin)
	g.GET("/rspamd/domain/:domain", rspamdDomainGet(d))
	g.PUT("/rspamd/domain/:domain", rspamdDomainSave(d))
}

// rspamdServerID reads the ?server_id= parameter (default 1, the single
// node of a standard install).
func rspamdServerID(c *echo.Context) (uint32, error) {
	raw := c.QueryParam("server_id")
	if raw == "" {
		return 1, nil
	}
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "invalid server id")
	}
	return uint32(id), nil
}

// scoreField validates one threshold: Rspamd scores outside this range
// are always a typo, and a tag level above the kill level would tag mail
// that is already rejected.
func scoreField(name string, v float64, errs map[string][]string) {
	if v < 0 || v > 100 {
		errs[name] = []string{name + "_error_range"}
	}
}

// validateScores runs the shared threshold rules of both endpoints.
func validateScores(tag, kill float64, extra map[string]float64) error {
	errs := map[string][]string{}
	scoreField("spam_tag_level", tag, errs)
	scoreField("spam_kill_level", kill, errs)
	for name, v := range extra {
		scoreField(name, v, errs)
	}
	if tag > kill {
		errs["spam_tag_level"] = []string{"spam_tag_level_error_above_kill"}
	}
	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

// normalizeList trims, lowercases and de-duplicates a submitted address
// list, refusing entries that are neither an address nor an @domain.
func normalizeList(field string, in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if strings.ContainsAny(v, " \t\r\n,;") || !strings.Contains(v, ".") {
			return nil, &ValidationError{Fields: map[string][]string{field: {field + "_error_invalid"}}}
		}
		v = strings.TrimPrefix(v, "@")
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// mailSection reads a server config, returning the raw INI (for the
// datalog diff), every parsed section and the [mail] one.
func mailSection(db *gorm.DB, serverID uint32) (string, getconf.Sections, map[string]string, error) {
	raw, sections, err := loadServerConfig(db, serverID)
	if err != nil {
		return "", nil, nil, err
	}
	return raw, sections, sections["mail"], nil
}

// globalLists loads the global white/blacklist of one server.
func globalLists(db *gorm.DB, serverID uint32) (white, black []string, err error) {
	var rows []model.MailAccess
	err = db.Where("server_id = ? AND type = ? AND access IN ?",
		serverID, "sender", []string{accessWhitelist, accessBlacklist}).Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	white, black = []string{}, []string{}
	for _, r := range rows {
		if r.Access == accessWhitelist {
			white = append(white, r.Source)
		} else {
			black = append(black, r.Source)
		}
	}
	return white, black, nil
}

// rspamdPolicyGet implements GET /api/mail/rspamd/policy.
//
//	@Summary		Read the global Rspamd policy
//	@Description	Score thresholds of the server's [mail] configuration (rendered into local.d/actions.conf by the daemon) plus the global sender white/blacklist held as mail_access rows. Admin only.
//	@Tags			mail
//	@Produce		json
//	@Param			server_id	query		int	false	"Mail server id (default 1)"
//	@Success		200			{object}	RspamdPolicy
//	@Failure		403			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse
//	@Router			/mail/rspamd/policy [get]
func rspamdPolicyGet(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		serverID, err := rspamdServerID(c)
		if err != nil {
			return err
		}
		db := d.DB.WithContext(c.Request().Context())
		_, _, cfg, err := mailSection(db, serverID)
		if err != nil {
			return err
		}
		white, black, err := globalLists(db, serverID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, RspamdPolicy{
			ServerID:         serverID,
			SpamTagLevel:     parseScore(cfg["rspamd_spam_tag_level"], 6),
			SpamKillLevel:    parseScore(cfg["rspamd_spam_kill_level"], 15),
			GreylistingLevel: parseScore(cfg["rspamd_greylisting_level"], 4),
			Whitelist:        white,
			Blacklist:        black,
		})
	}
}

// parseScore reads a stored threshold, falling back to the daemon's own
// default when unset or unparseable.
func parseScore(v string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}

// rspamdPolicySave implements PUT /api/mail/rspamd/policy.
//
//	@Summary		Save the global Rspamd policy
//	@Description	Writes the thresholds into the server's [mail] configuration and replaces the global sender white/blacklist, journalling both to sys_datalog so the owning node re-renders local.d/actions.conf and the global wblist confs. Admin only.
//	@Tags			mail
//	@Accept			json
//	@Produce		json
//	@Param			server_id	query		int				false	"Mail server id (default 1)"
//	@Param			body		body		RspamdPolicy	true	"Policy"
//	@Success		200			{object}	RspamdPolicy
//	@Failure		400			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse
//	@Router			/mail/rspamd/policy [put]
func rspamdPolicySave(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		serverID, err := rspamdServerID(c)
		if err != nil {
			return err
		}
		var body RspamdPolicy
		if err := c.Bind(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		if err := validateScores(body.SpamTagLevel, body.SpamKillLevel,
			map[string]float64{"greylisting_level": body.GreylistingLevel}); err != nil {
			return err
		}
		white, err := normalizeList("whitelist", body.Whitelist)
		if err != nil {
			return err
		}
		black, err := normalizeList("blacklist", body.Blacklist)
		if err != nil {
			return err
		}

		id := identity(c)
		err = datalogTxn(c.Request().Context(), d.DB, func(tx *gorm.DB) error {
			old, sections, _, err := mailSection(
				tx.Clauses(clause.Locking{Strength: "UPDATE"}), serverID)
			if err != nil {
				return err
			}
			if sections["mail"] == nil {
				sections["mail"] = map[string]string{}
			}
			sections["mail"]["rspamd_spam_tag_level"] = formatScore(body.SpamTagLevel)
			sections["mail"]["rspamd_spam_kill_level"] = formatScore(body.SpamKillLevel)
			sections["mail"]["rspamd_greylisting_level"] = formatScore(body.GreylistingLevel)
			ini := sections.String()
			if err := tx.Model(&model.Server{}).Where("server_id = ?", serverID).
				Update("config", ini).Error; err != nil {
				return err
			}
			if err := datalog.LogServerConfig(tx, serverID, old, ini, id.Username); err != nil {
				return err
			}
			return syncGlobalLists(tx, serverID, id, white, black)
		})
		if err != nil {
			return err
		}
		body.ServerID, body.Whitelist, body.Blacklist = serverID, white, black
		return c.JSON(http.StatusOK, body)
	}
}

// formatScore renders a threshold without a trailing ".0" so the INI and
// the rendered actions.conf stay diff-stable.
func formatScore(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// syncGlobalLists makes the mail_access sender rows of one server match
// the submitted lists exactly, journalling every insert and delete so the
// daemon adds and removes the matching global wblist confs.
func syncGlobalLists(tx *gorm.DB, serverID uint32, id *repository.Identity, white, black []string) error {
	want := map[string]string{}
	for _, s := range white {
		want[s] = accessWhitelist
	}
	for _, s := range black {
		// A source listed twice is a blacklist entry: the stricter rule
		// wins, and the unique (server_id, source, type) key allows one.
		want[s] = accessBlacklist
	}

	var rows []model.MailAccess
	err := tx.Where("server_id = ? AND type = ? AND access IN ?",
		serverID, "sender", []string{accessWhitelist, accessBlacklist}).Find(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		if want[row.Source] == row.Access {
			delete(want, row.Source)
			continue
		}
		if err := tx.Delete(&model.MailAccess{}, row.AccessID).Error; err != nil {
			return err
		}
		if err := datalog.LogDelete(tx, &row, id.Username); err != nil {
			return err
		}
	}
	for source, access := range want {
		row := &model.MailAccess{
			SysUserID: id.UserID, SysGroupID: ownerGroup(id),
			SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: int32(serverID), Source: source,
			Access: access, Type: "sender", Active: "y",
		}
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if err := datalog.LogInsert(tx, row, id.Username); err != nil {
			return err
		}
	}
	return nil
}

// ownerGroup is the group new rows are stamped with (admin group as the
// fallback, matching the dns wizard).
func ownerGroup(id *repository.Identity) uint32 {
	if id.DefaultGroup > 0 {
		return id.DefaultGroup
	}
	return 1
}

// domainPolicyName is the name of the spamfilter_policy row owned by one
// domain override; a dedicated row keeps the edit from leaking into every
// other identity that shares a policy.
func domainPolicyName(domain string) string { return "rspamd_domain_" + domain }

// rspamdDomainGet implements GET /api/mail/rspamd/domain/:domain.
//
//	@Summary		Read the per-domain Rspamd scores
//	@Description	Score thresholds of one mail domain. When the domain has no override the server-wide thresholds are returned with inherited=true. Requires read access to the mail domain.
//	@Tags			mail
//	@Produce		json
//	@Param			domain	path		string	true	"Mail domain"
//	@Success		200		{object}	RspamdDomainPolicy
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/mail/rspamd/domain/{domain} [get]
func rspamdDomainGet(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		db := d.DB.WithContext(c.Request().Context())
		dom, err := readableMailDomain(db, c)
		if err != nil {
			return err
		}
		var pol model.SpamfilterPolicy
		err = db.Where("policy_name = ?", domainPolicyName(dom.Domain)).Take(&pol).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_, _, cfg, err := mailSection(db, dom.ServerID)
			if err != nil {
				return err
			}
			return c.JSON(http.StatusOK, RspamdDomainPolicy{
				Domain:        dom.Domain,
				SpamTagLevel:  parseScore(cfg["rspamd_spam_tag_level"], 6),
				SpamKillLevel: parseScore(cfg["rspamd_spam_kill_level"], 15),
				Inherited:     true,
			})
		}
		if err != nil {
			return err
		}
		out := RspamdDomainPolicy{Domain: dom.Domain}
		if pol.RspamdSpamTagLevel != nil {
			out.SpamTagLevel = *pol.RspamdSpamTagLevel
		}
		if pol.RspamdSpamKillLevel != nil {
			out.SpamKillLevel = *pol.RspamdSpamKillLevel
		}
		return c.JSON(http.StatusOK, out)
	}
}

// readableMailDomain resolves the :domain path parameter under the
// caller's read scope; an unreadable or unknown domain is a 404.
func readableMailDomain(db *gorm.DB, c *echo.Context) (*model.MailDomain, error) {
	name := strings.ToLower(strings.TrimSpace(c.Param("domain")))
	if name == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid domain")
	}
	var dom model.MailDomain
	err := db.Model(&model.MailDomain{}).
		Scopes(repository.WithPerm(identity(c), repository.PermRead)).
		Where("domain = ?", name).Take(&dom).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, echo.NewHTTPError(http.StatusNotFound, "mail domain not found")
	}
	if err != nil {
		return nil, err
	}
	return &dom, nil
}

// rspamdDomainSave implements PUT /api/mail/rspamd/domain/:domain.
//
//	@Summary		Save the per-domain Rspamd scores
//	@Description	Upserts the domain's dedicated spamfilter policy and its "@domain" spamfilter user, journalling the user row to sys_datalog so the daemon re-renders the domain settings file and its child mailboxes. Requires read access to the mail domain.
//	@Tags			mail
//	@Accept			json
//	@Produce		json
//	@Param			domain	path		string				true	"Mail domain"
//	@Param			body	body		RspamdDomainPolicy	true	"Scores"
//	@Success		200		{object}	RspamdDomainPolicy
//	@Failure		400		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/mail/rspamd/domain/{domain} [put]
func rspamdDomainSave(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var body RspamdDomainPolicy
		if err := c.Bind(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		if err := validateScores(body.SpamTagLevel, body.SpamKillLevel, nil); err != nil {
			return err
		}
		ctx := c.Request().Context()
		dom, err := readableMailDomain(d.DB.WithContext(ctx), c)
		if err != nil {
			return err
		}
		id := identity(c)
		err = datalogTxn(ctx, d.DB, func(tx *gorm.DB) error {
			return saveDomainPolicy(tx, dom, id, body)
		})
		if err != nil {
			return err
		}
		body.Domain, body.Inherited = dom.Domain, false
		return c.JSON(http.StatusOK, body)
	}
}

// saveDomainPolicy upserts the policy row and the "@domain" spamfilter
// user that binds it, writing the datalog row on the user (the policy
// table has no daemon hook, the user event is what re-renders).
func saveDomainPolicy(tx *gorm.DB, dom *model.MailDomain, id *repository.Identity, body RspamdDomainPolicy) error {
	tag, kill := body.SpamTagLevel, body.SpamKillLevel
	var pol model.SpamfilterPolicy
	err := tx.Where("policy_name = ?", domainPolicyName(dom.Domain)).Take(&pol).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		pol = model.SpamfilterPolicy{
			SysUserID: dom.SysUserID, SysGroupID: dom.SysGroupID,
			SysPermUser: "riud", SysPermGroup: "riud",
			PolicyName:          domainPolicyName(dom.Domain),
			RspamdSpamTagLevel:  &tag,
			RspamdSpamKillLevel: &kill,
			RspamdSpamTagMethod: "add_header",
			RspamdGreylisting:   "n",
		}
		if err := tx.Create(&pol).Error; err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		pol.RspamdSpamTagLevel, pol.RspamdSpamKillLevel = &tag, &kill
		if err := tx.Model(&model.SpamfilterPolicy{}).Where("id = ?", pol.ID).
			Updates(map[string]any{
				"rspamd_spam_tag_level":  tag,
				"rspamd_spam_kill_level": kill,
			}).Error; err != nil {
			return err
		}
	}

	email := "@" + dom.Domain
	var user model.SpamfilterUser
	err = tx.Where("server_id = ? AND email = ?", dom.ServerID, email).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = model.SpamfilterUser{
			SysUserID: dom.SysUserID, SysGroupID: dom.SysGroupID,
			SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: dom.ServerID, Priority: 5,
			PolicyID: pol.ID, Email: email, Fullname: dom.Domain, Local: "Y",
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return datalog.LogInsert(tx, &user, id.Username)
	}
	if err != nil {
		return err
	}
	old := user
	if old.PolicyID == pol.ID {
		// The policy table has no daemon hook, so a threshold-only edit
		// leaves the user row byte-identical and buildDiff would suppress
		// the datalog row — and nothing would ever re-render the file.
		// Reporting the previous policy as unset forces the update
		// through; the payload the daemon reads is `new`, which is
		// correct either way.
		old.PolicyID = 0
	}
	user.PolicyID = pol.ID
	if err := tx.Model(&model.SpamfilterUser{}).Where("id = ?", user.ID).
		Update("policy_id", pol.ID).Error; err != nil {
		return err
	}
	return datalog.LogUpdate(tx, &old, &user, id.Username)
}
