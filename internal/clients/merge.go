package clients

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/model"
)

// mergeKind classifies how a template column merges when additional
// templates stack on the master (client_templates.inc.php switch).
type mergeKind int

const (
	// kindNumeric adds while current != -1; additional -1 promotes.
	kindNumeric mergeKind = iota
	// kindCronFrequency takes the minimum, floored at 1.
	kindCronFrequency
	// kindFlag is a y/n enum where y (less restrictive) wins.
	kindFlag
	// kindFlagSuexec is force_suexec, where n is less restrictive.
	kindFlagSuexec
	// kindUnion is a comma-separated set union (CHECKBOXARRAY/MULTIPLE).
	kindUnion
	// kindSelectCron picks the lower index in full < chrooted < url.
	kindSelectCron
	// kindDefaultServer: additionals only fill a master 0; a value of 0
	// is never written to the client (default server unchanged).
	kindDefaultServer
	// kindMasterOnly copies from the master; additionals are ignored
	// (TEXT fields like limit_web_ip fall through the PHP switch).
	kindMasterOnly
)

// mergeKinds maps every materialized client_template column to its rule.
var mergeKinds = map[string]mergeKind{
	"limit_maildomain": kindNumeric, "limit_mailbox": kindNumeric,
	"limit_mailalias": kindNumeric, "limit_mailaliasdomain": kindNumeric,
	"limit_mailforward": kindNumeric, "limit_mailcatchall": kindNumeric,
	"limit_mailrouting": kindNumeric, "limit_mail_wblist": kindNumeric,
	"limit_mailfilter": kindNumeric, "limit_fetchmail": kindNumeric,
	"limit_mailquota": kindNumeric, "limit_spamfilter_wblist": kindNumeric,
	"limit_spamfilter_user": kindNumeric, "limit_spamfilter_policy": kindNumeric,
	"limit_xmpp_domain": kindNumeric, "limit_xmpp_user": kindNumeric,
	"limit_web_domain": kindNumeric, "limit_web_quota": kindNumeric,
	"limit_web_subdomain": kindNumeric, "limit_web_aliasdomain": kindNumeric,
	"limit_ftp_user": kindNumeric, "limit_shell_user": kindNumeric,
	"limit_webdav_user": kindNumeric, "limit_aps": kindNumeric,
	"limit_dns_zone": kindNumeric, "limit_dns_slave_zone": kindNumeric,
	"limit_dns_record": kindNumeric, "limit_database": kindNumeric,
	"limit_database_postgresql": kindNumeric, "limit_database_user": kindNumeric,
	"limit_database_quota": kindNumeric, "limit_cron": kindNumeric,
	"limit_traffic_quota": kindNumeric, "limit_client": kindNumeric,
	"limit_domainmodule": kindNumeric, "limit_mailmailinglist": kindNumeric,
	"limit_openvz_vm": kindNumeric, "limit_openvz_vm_template_id": kindNumeric,

	"limit_cron_frequency": kindCronFrequency,

	"limit_mail_backup": kindFlag, "limit_relayhost": kindFlag,
	"limit_xmpp_muc": kindFlag, "limit_xmpp_anon": kindFlag,
	"limit_xmpp_vjud": kindFlag, "limit_xmpp_proxy": kindFlag,
	"limit_xmpp_status": kindFlag, "limit_xmpp_pastebin": kindFlag,
	"limit_xmpp_httparchive": kindFlag,
	"limit_cgi":              kindFlag, "limit_ssi": kindFlag,
	"limit_perl": kindFlag, "limit_ruby": kindFlag, "limit_python": kindFlag,
	"limit_hterror": kindFlag, "limit_wildcard": kindFlag,
	"limit_ssl": kindFlag, "limit_ssl_letsencrypt": kindFlag,
	"limit_backup": kindFlag, "limit_directive_snippets": kindFlag,

	"force_suexec": kindFlagSuexec,

	"web_php_options": kindUnion, "ssh_chroot": kindUnion,
	"mail_servers": kindUnion, "web_servers": kindUnion,
	"dns_servers": kindUnion, "db_servers": kindUnion,
	"xmpp_servers": kindUnion,

	"limit_cron_type": kindSelectCron,

	"default_xmppserver": kindDefaultServer, "default_slave_dnsserver": kindDefaultServer,

	"limit_web_ip": kindMasterOnly,
}

// cronTypeOrder is the tform SELECT value order (lower index wins).
var cronTypeOrder = []string{"full", "chrooted", "url"}

// mergeSchemaCache caches template/client schema parses.
var mergeSchemaCache = &sync.Map{}

// templateColumns reads the merge-relevant columns of a template as a
// column → value map via the GORM schema.
func templateColumns(tpl *model.ClientTemplate) (map[string]any, error) {
	s, err := schema.Parse(tpl, mergeSchemaCache, schema.NamingStrategy{})
	if err != nil {
		return nil, fmt.Errorf("clients: parsing template schema: %w", err)
	}
	ctx := context.Background()
	rv := reflect.ValueOf(tpl)
	out := make(map[string]any, len(mergeKinds))
	for _, f := range s.Fields {
		if _, ok := mergeKinds[f.DBName]; !ok {
			continue
		}
		v, _ := f.ValueOf(ctx, rv)
		out[f.DBName] = v
	}
	return out, nil
}

// MergeTemplates materializes the master template plus the additional
// templates into the client limit columns (pure port of
// apply_client_templates). The returned map holds column → value ready
// to write onto the client row; for non-resellers limit_client is
// absent, and default servers with value 0 are omitted (must not
// overwrite the client's default).
func MergeTemplates(master *model.ClientTemplate, additionals []*model.ClientTemplate, isReseller bool) (map[string]any, error) {
	limits, err := templateColumns(master)
	if err != nil {
		return nil, err
	}
	// Master limit_client normalization (PHP head of apply).
	if isReseller && asInt(limits["limit_client"]) == 0 {
		limits["limit_client"] = int32(-1)
	} else if !isReseller && asInt(limits["limit_client"]) != 0 {
		limits["limit_client"] = int32(0)
	}

	for _, add := range additionals {
		addCols, err := templateColumns(add)
		if err != nil {
			return nil, err
		}
		for col, v := range addCols {
			if col == "limit_client" {
				// Wrong-kind template values are skipped entirely.
				if isReseller && asInt(v) == 0 {
					continue
				}
				if !isReseller && asInt(v) != 0 {
					continue
				}
			}
			switch mergeKinds[col] {
			case kindNumeric:
				cur := asInt(limits[col])
				if cur > -1 {
					if asInt(v) == -1 {
						limits[col] = int32(-1)
					} else {
						limits[col] = int32(cur + asInt(v))
					}
				}
			case kindCronFrequency:
				cur := asInt(limits[col])
				if asInt(v) < cur {
					cur = asInt(v)
				}
				if cur < 1 {
					cur = 1
				}
				limits[col] = int32(cur)
			case kindFlag:
				if asString(limits[col]) == "y" || asString(v) == "y" {
					limits[col] = "y"
				} else {
					limits[col] = "n"
				}
			case kindFlagSuexec:
				if asString(limits[col]) == "n" || asString(v) == "n" {
					limits[col] = "n"
				} else {
					limits[col] = "y"
				}
			case kindUnion:
				limits[col] = unionCSV(asString(limits[col]), asString(v))
			case kindSelectCron:
				limits[col] = lowerCronType(asString(limits[col]), asString(v))
			case kindDefaultServer:
				if asInt(limits[col]) == 0 {
					limits[col] = v
				}
			case kindMasterOnly:
				// additionals ignored
			}
		}
	}

	// Only resellers may receive limit_client from templates (guards
	// against accidental client -> reseller conversion).
	if !isReseller {
		delete(limits, "limit_client")
	}
	// A template without a default server must not change the client's.
	for col, kind := range mergeKinds {
		if kind == kindDefaultServer && asInt(limits[col]) == 0 {
			delete(limits, col)
		}
	}
	return limits, nil
}

// ApplyTemplates migrates any legacy additional list and, when the
// client references a master template, materializes master + additional
// limits onto the client row inside tx (apply_client_templates). With
// template_master = 0 (custom limits) nothing is merged.
func ApplyTemplates(ctx context.Context, tx *gorm.DB, c *model.Client) error {
	if _, err := MigrateLegacyAdditional(ctx, tx, c); err != nil {
		return err
	}
	if c.TemplateMaster == 0 {
		return nil
	}
	var master model.ClientTemplate
	err := tx.WithContext(ctx).Where("template_id = ?", c.TemplateMaster).Take(&master).Error
	if err != nil {
		return fmt.Errorf("clients: loading master template %d: %w", c.TemplateMaster, err)
	}
	assigned, err := AssignedTemplates(ctx, tx, int64(c.ClientID))
	if err != nil {
		return err
	}
	additionals := make([]*model.ClientTemplate, len(assigned))
	for i := range assigned {
		additionals[i] = &assigned[i]
	}
	limits, err := MergeTemplates(&master, additionals, IsReseller(c))
	if err != nil {
		return err
	}
	err = tx.WithContext(ctx).Model(&model.Client{}).
		Where("client_id = ?", c.ClientID).Updates(limits).Error
	if err != nil {
		return fmt.Errorf("clients: writing materialized limits: %w", err)
	}
	// Refresh the in-memory row so callers (and datalog diffs) see the
	// materialized values.
	return tx.WithContext(ctx).Take(c, c.ClientID).Error
}

// unionCSV merges two comma-separated sets preserving first-seen order.
func unionCSV(a, b string) string {
	seen := map[string]bool{}
	var out []string
	for _, part := range append(splitCSVList(a), splitCSVList(b)...) {
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return strings.Join(out, ",")
}

// splitCSVList splits a comma list dropping empties.
func splitCSVList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lowerCronType picks the lower-indexed cron type (full < chrooted <
// url, the tform SELECT order — PHP chooses min index).
func lowerCronType(a, b string) string {
	idx := func(v string) int {
		for i, o := range cronTypeOrder {
			if o == v {
				return i
			}
		}
		return len(cronTypeOrder) - 1
	}
	if idx(a) <= idx(b) {
		return a
	}
	return b
}

// asInt normalizes the numeric column value types.
func asInt(v any) int {
	switch n := v.(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	case uint32:
		return int(n)
	default:
		return 0
	}
}

// asString normalizes string column values.
func asString(v any) string {
	s, _ := v.(string)
	return s
}
