package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
)

// DNSPublisher publishes or withdraws TXT records into a managed zone
// (design D7 of add-mail-module). The mail package never touches dns_rr
// SQL: record ownership comes from the matching SOA, serials advance via
// NextSerial, and every write is journaled. When no managed zone covers
// the name, UpsertTXT reports published=false and the caller surfaces
// the suggested record for manual publication.
//
// The design sketch carried an Owner parameter; it is dropped here
// because ownership always derives from the SOA row (the design's own
// upsert rule), so there is nothing for the caller to supply.
//
// Both methods run on the caller's transaction (tx) so DKIM DNS changes
// commit atomically with the mail_domain save that triggered them and
// the datalog notifier fires on that single outer commit.
type DNSPublisher interface {
	// UpsertTXT replaces any TXT with the same name and data prefix in
	// the covering zone and inserts the new record.
	UpsertTXT(ctx context.Context, tx *gorm.DB, name, data string, ttl uint32) (published bool, err error)
	// DeleteTXT removes TXT records with the exact name whose data
	// starts with dataPrefix (e.g. "v=DKIM1").
	DeleteTXT(ctx context.Context, tx *gorm.DB, name, dataPrefix string) error
}

// dbDNSPublisher is the DNS-module implementation; all reads/writes go
// through the transaction handed to each call.
type dbDNSPublisher struct{}

// NewDNSPublisher returns the database-backed publisher.
func NewDNSPublisher(_ *gorm.DB) DNSPublisher { return dbDNSPublisher{} }

// findSOA walks the name's parent labels until an active managed zone
// matches (port of mail_domain_edit find_soa_domain, generalized to any
// record name).
func (dbDNSPublisher) findSOA(ctx context.Context, tx *gorm.DB, name string) (*model.DNSSoa, error) {
	candidate := strings.TrimSuffix(name, ".") + "."
	for strings.Count(candidate, ".") > 1 {
		var soa model.DNSSoa
		err := tx.WithContext(ctx).
			Where("active = 'Y' AND origin = ?", candidate).Take(&soa).Error
		if err == nil {
			return &soa, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		dot := strings.Index(candidate, ".")
		candidate = candidate[dot+1:]
	}
	return nil, nil
}

// UpsertTXT implements DNSPublisher on the caller's transaction.
func (p dbDNSPublisher) UpsertTXT(ctx context.Context, tx *gorm.DB, name, data string, ttl uint32) (bool, error) {
	soa, err := p.findSOA(ctx, tx, name)
	if err != nil || soa == nil {
		return false, err
	}
	prefix := data
	if i := strings.Index(data, ";"); i > 0 {
		prefix = data[:i]
	}
	if _, err := p.deleteRows(ctx, tx, name, prefix); err != nil {
		return false, err
	}
	now := time.Now()
	serial := NextSerial(0, now)
	rr := &model.DNSRr{
		SysUserID: soa.SysUserID, SysGroupID: soa.SysGroupID,
		SysPermUser: soa.SysPermUser, SysPermGroup: soa.SysPermGroup,
		SysPermOther: soa.SysPermOther,
		ServerID:     soa.ServerID, Zone: soa.ID,
		Name: name, Type: "TXT", Data: data,
		Aux: 0, TTL: ttl, Active: "Y",
		Stamp: &now, Serial: &serial,
	}
	if err := tx.WithContext(ctx).Create(rr).Error; err != nil {
		return false, err
	}
	if err := datalog.LogInsert(tx, rr, "admin"); err != nil {
		return false, err
	}
	if err := bumpZoneSerial(tx, soa.ID, "admin"); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteTXT implements DNSPublisher on the caller's transaction: it
// removes the rows and bumps each touched zone's serial once.
func (p dbDNSPublisher) DeleteTXT(ctx context.Context, tx *gorm.DB, name, dataPrefix string) error {
	zones, err := p.deleteRows(ctx, tx, name, dataPrefix)
	if err != nil {
		return err
	}
	for zone := range zones {
		if err := bumpZoneSerial(tx, zone, "admin"); err != nil {
			return err
		}
	}
	return nil
}

// deleteRows removes matching TXT rows with datalog journaling and
// returns the set of zones touched (serial bumping is the caller's job,
// so an upsert bumps exactly once — mail_domain_edit parity).
func (dbDNSPublisher) deleteRows(ctx context.Context, tx *gorm.DB, name, dataPrefix string) (map[uint32]struct{}, error) {
	var rows []model.DNSRr
	err := tx.WithContext(ctx).
		Where("name = ? AND type = 'TXT' AND data LIKE ?", name, dataPrefix+"%").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	zones := map[uint32]struct{}{}
	for i := range rows {
		if err := tx.Delete(&model.DNSRr{}, rows[i].ID).Error; err != nil {
			return nil, err
		}
		if err := datalog.LogDelete(tx, &rows[i], "admin"); err != nil {
			return nil, err
		}
		zones[rows[i].Zone] = struct{}{}
	}
	return zones, nil
}

// NoopDNSPublisher never publishes (DNS module disabled): saves succeed
// and callers surface the manual TXT.
type NoopDNSPublisher struct{}

// UpsertTXT implements DNSPublisher (always unpublished).
func (NoopDNSPublisher) UpsertTXT(context.Context, *gorm.DB, string, string, uint32) (bool, error) {
	return false, nil
}

// DeleteTXT implements DNSPublisher (nothing to withdraw).
func (NoopDNSPublisher) DeleteTXT(context.Context, *gorm.DB, string, string) error { return nil }
