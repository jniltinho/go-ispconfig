// Package validator ports the ISPConfig3 tform field validators (design D5,
// interface/lib/classes/tform_base.inc.php validateField): declarative rules
// attached to entity fields, evaluated against the submitted string value
// and reported as i18n error keys.
package validator

import (
	"context"
	"fmt"
	"net/mail"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// Context carries the data a validator may need beyond the field value
// itself: the database handle and table scope for UNIQUE, and the primary
// key of the record being updated (nil on create).
type Context struct {
	// Ctx is the request context.
	Ctx context.Context
	// DB is the database handle used by UNIQUE.
	DB *gorm.DB
	// Table is the entity table UNIQUE queries.
	Table string
	// Column is the DB column of the field being validated.
	Column string
	// PKColumn is the primary key column of Table.
	PKColumn string
	// PK is the primary key of the record being updated; nil on create.
	// UNIQUE excludes this record from its duplicate check.
	PK any
}

// Func is a CUSTOM validator: it receives the validation context and the
// field value and returns an i18n error key, or "" when the value is valid.
type Func func(vc *Context, value string) string

// Rule is one declarative field validator, mirroring a tform validator
// entry. Type selects the check; ErrKey is the i18n key reported on
// failure. Rules marshal to JSON as form metadata hints for the SPA.
type Rule struct {
	// Type is the validator name: REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT,
	// ISPOSITIVE, ISIPV4, ISIPV6, ISIP or CUSTOM.
	Type string `json:"type"`
	// Regex is the pattern of a REGEX rule (Go syntax).
	Regex string `json:"regex,omitempty"`
	// AllowEmpty skips the check for empty values (tform allowempty 'y';
	// honored by UNIQUE, ISEMAIL, ISINT, ISPOSITIVE and ISIP). Without it,
	// an empty value fails those checks — a numeric rule must never be
	// silently satisfied by an absent value.
	AllowEmpty bool `json:"allow_empty,omitempty"`
	// ErrKey is the i18n message key reported on failure (tform errmsg).
	ErrKey string `json:"errmsg"`
	// Fn is the check of a CUSTOM rule; not exported as metadata.
	Fn Func `json:"-"`
}

// Validate checks value against the rule. It returns the i18n error key on
// validation failure ("" when valid) and a non-nil err only on
// infrastructure failure (e.g. the UNIQUE query erroring), which callers
// must surface as a server error, not a validation message. Unknown rule
// types fail closed with an error key so a typo in an entity definition
// cannot silently skip validation.
func (r Rule) Validate(vc *Context, value string) (key string, err error) {
	var ok bool
	switch r.Type {
	case "REGEX":
		re, compErr := regexp.Compile(r.Regex)
		ok = compErr == nil && re.MatchString(value)
	case "UNIQUE":
		if r.AllowEmpty && value == "" {
			return "", nil
		}
		var n int64
		if err := uniqueQuery(vc, value).Count(&n).Error; err != nil {
			return "", fmt.Errorf("validator: UNIQUE check on %s.%s: %w", vc.Table, vc.Column, err)
		}
		ok = n == 0
	case "NOTEMPTY":
		ok = value != ""
	case "ISEMAIL":
		if r.AllowEmpty && value == "" {
			return "", nil
		}
		ok = isEmail(value)
	case "ISINT":
		if r.AllowEmpty && value == "" {
			return "", nil
		}
		_, parseErr := strconv.ParseInt(value, 10, 64)
		ok = parseErr == nil
	case "ISPOSITIVE":
		if r.AllowEmpty && value == "" {
			return "", nil
		}
		n, parseErr := strconv.ParseInt(value, 10, 64)
		ok = parseErr == nil && n >= 1
	case "ISIPV4":
		addr, parseErr := netip.ParseAddr(value)
		ok = parseErr == nil && addr.Is4()
	case "ISIPV6":
		addr, parseErr := netip.ParseAddr(value)
		ok = parseErr == nil && addr.Is6()
	case "ISIP":
		if r.AllowEmpty && value == "" {
			return "", nil
		}
		_, parseErr := netip.ParseAddr(value)
		ok = parseErr == nil
	case "CUSTOM":
		if r.Fn == nil {
			return r.errKey(), nil
		}
		return r.Fn(vc, value), nil
	default:
		ok = false
	}
	if ok {
		return "", nil
	}
	return r.errKey(), nil
}

// errKey returns the configured error key, falling back to a key derived
// from the rule type so a failure is never silent.
func (r Rule) errKey() string {
	if r.ErrKey != "" {
		return r.ErrKey
	}
	return "error.validation." + strings.ToLower(r.Type)
}

// uniqueQuery builds the UNIQUE duplicate-check query: count of rows in the
// entity table holding the same value, excluding the record being updated.
// Table and column names come from entity definitions in code, never from
// request input.
func uniqueQuery(vc *Context, value string) *gorm.DB {
	q := vc.DB.WithContext(vc.Ctx).Table(vc.Table).
		Where(fmt.Sprintf("`%s` = ?", vc.Column), value)
	if vc.PK != nil {
		q = q.Where(fmt.Sprintf("`%s` != ?", vc.PKColumn), vc.PK)
	}
	return q
}

// isEmail ports the tform ISEMAIL check: a parseable single address with no
// '+' in the local part (PHP disallows it explicitly).
func isEmail(value string) bool {
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value {
		return false
	}
	local, _, found := strings.Cut(addr.Address, "@")
	return found && !strings.Contains(local, "+")
}
