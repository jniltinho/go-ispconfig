package api

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"go-ispconfig/internal/mail"
)

// DKIMPublishState is what a mail-domain save reports back about DNS
// publication (spec mail-dkim): the suggested TXT for manual setup and
// whether a managed zone actually received it.
type DKIMPublishState struct {
	// DNSPublished is true when a managed zone got the TXT record.
	DNSPublished bool `json:"dns_published"`
	// SuggestedRecord is the full record line for manual publication
	// (mail_domain_edit $dns_record parity), empty when DKIM is off.
	SuggestedRecord string `json:"suggested_record,omitempty"`
}

// DKIMDomain is the subset of mail_domain driving DNS publication.
type DKIMDomain struct {
	Domain   string
	Selector string
	Public   string
	Active   bool
	DKIM     bool
}

// SyncDKIMDNS reconciles the DKIM TXT record of a mail domain through
// the DNSPublisher after a save or delete (port of the
// mail_domain_edit.php update_dns flow, design D6/D7): stale records of
// the old selector/domain are withdrawn, the current record is upserted
// while the domain is active with DKIM on, and everything is withdrawn
// on disable/deactivate/delete. Publisher failures degrade to
// dns_published=false — the domain save itself never fails on DNS.
func SyncDKIMDNS(ctx context.Context, tx *gorm.DB, pub DNSPublisher, old, current *DKIMDomain) DKIMPublishState {
	state := DKIMPublishState{}
	if pub == nil {
		pub = NoopDNSPublisher{}
	}

	// Withdraw the old record when its name no longer matches (rename,
	// selector change) or publication stops (disable/deactivate/delete).
	if old != nil && old.DKIM {
		oldName := mail.DKIMRecordName(old.Selector, old.Domain)
		stops := current == nil || !current.Active || !current.DKIM
		moved := current != nil && mail.DKIMRecordName(current.Selector, current.Domain) != oldName
		if stops || moved {
			if err := pub.DeleteTXT(ctx, tx, oldName, "v=DKIM1"); err != nil {
				slog.Warn("api: could not withdraw DKIM TXT", "name", oldName, "error", err)
			}
		}
	}

	if current == nil || !current.DKIM {
		return state
	}
	data := mail.DKIMTXTValue(current.Public)
	name := mail.DKIMRecordName(current.Selector, current.Domain)
	state.SuggestedRecord = name + " 3600 IN TXT " + data
	if !current.Active {
		return state
	}
	published, err := pub.UpsertTXT(ctx, tx, name, data, 3600)
	if err != nil {
		slog.Warn("api: DKIM TXT publication failed", "name", name, "error", err)
		return state
	}
	state.DNSPublished = published
	return state
}
