package dns

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// rrInsert handles dns_rr_insert: regenerate the whole parent zone
// (design D2), but only when the parent SOA exists and is active.
func (p *Plugin) rrInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.rrApply(ctx, row(data.New).num("zone"), true)
}

// rrUpdate handles dns_rr_update. PHP parity: soa_update runs even when
// the parent SOA vanished or is inactive (named.conf is still rebuilt and
// a reload queued).
func (p *Plugin) rrUpdate(ctx context.Context, _ string, data engine.Data) error {
	return p.rrApply(ctx, row(data.New).num("zone"), false)
}

// rrDelete handles dns_rr_delete: the parent SOA comes from old.zone (the
// zone may already be gone — then this is a no-op like in PHP).
func (p *Plugin) rrDelete(ctx context.Context, _ string, data engine.Data) error {
	return p.rrApply(ctx, row(data.Old).num("zone"), true)
}

// rrApply loads the parent dns_soa row and delegates to the full SOA
// update path with {old,new} both set to the current SOA state (port of
// bind_plugin rr_insert/rr_update/rr_delete). requireActive skips
// missing/inactive parents (insert/delete semantics).
func (p *Plugin) rrApply(ctx context.Context, zoneID int64, requireActive bool) error {
	soa, err := p.loadSoaRow(ctx, zoneID)
	if err != nil {
		return err
	}
	if requireActive && (soa == nil || row(soa).str("active") != "Y") {
		return nil
	}
	if soa == nil {
		soa = map[string]any{}
	}
	return p.soaApply(ctx, engine.Data{Old: soa, New: soa})
}

// loadSoaRow fetches one dns_soa row as a map (nil when it does not
// exist).
func (p *Plugin) loadSoaRow(ctx context.Context, id int64) (map[string]any, error) {
	var rec map[string]any
	err := p.db.WithContext(ctx).Table("dns_soa").Where("id = ?", id).Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("dns: loading dns_soa %d: %w", id, err)
	}
	return rec, nil
}
