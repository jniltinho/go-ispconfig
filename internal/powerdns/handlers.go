package powerdns

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// handleSOAInsert ports soa_insert: active Y only → MASTER domain + SOA record.
func (p *Plugin) handleSOAInsert(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	if !n.activeY() {
		return nil
	}
	origin := stripTrailingDot(n.str("origin"))
	ispID := int(n.num("id"))

	// Serial re-read from panel DB (PHP queryOneRecord dns_soa).
	serial := n.num("serial")
	if p.panelDB != nil {
		var soa model.DNSSoa
		if err := p.panelDB.WithContext(ctx).Select("serial").Take(&soa, ispID).Error; err == nil {
			serial = int64(soa.Serial)
		}
	}
	notified := int(serial)
	dom := Domain{
		Name:           origin,
		Type:           "MASTER",
		NotifiedSerial: &notified,
		ISPConfigID:    ispID,
	}
	if err := p.pdnsDB.WithContext(ctx).Create(&dom).Error; err != nil {
		return fmt.Errorf("powerdns: insert domain %s: %w", origin, err)
	}

	content := SOAContent(n.str("ns"), n.str("mbox"), origin,
		n.num("serial"), n.num("refresh"), n.num("retry"), n.num("expire"), n.num("minimum"))
	ttl := int(n.num("ttl"))
	prio := 0
	changeDate := int(time.Now().Unix())
	name := origin
	typ := "SOA"
	domainID := dom.ID
	rec := Record{
		DomainID:    &domainID,
		Name:        &name,
		Type:        &typ,
		Content:     &content,
		TTL:         &ttl,
		Prio:        &prio,
		ChangeDate:  &changeDate,
		ISPConfigID: ispID,
	}
	if err := p.pdnsDB.WithContext(ctx).Create(&rec).Error; err != nil {
		return fmt.Errorf("powerdns: insert SOA for %s: %w", origin, err)
	}

	p.zoneRediscover(ctx)
	p.handleDNSSEC(ctx, data)
	p.rectifyZone(ctx, data)
	p.notifySlave(ctx, data)
	p.queueRestart()
	return nil
}

// handleSOAUpdate ports soa_update: deactivate→delete; active update SOA;
// activate→insert + re-import active dns_rr.
func (p *Plugin) handleSOAUpdate(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	o := row(data.Old)
	if !n.activeY() {
		if !o.activeY() {
			return nil
		}
		return p.handleSOADelete(ctx, data)
	}

	ispID := int(n.num("id"))
	var existing Domain
	err := p.pdnsDB.WithContext(ctx).Where("ispconfig_id = ?", ispID).Take(&existing).Error
	if o.activeY() && err == nil {
		origin := stripTrailingDot(n.str("origin"))
		content := SOAContent(n.str("ns"), n.str("mbox"), origin,
			n.num("serial"), n.num("refresh"), n.num("retry"), n.num("expire"), n.num("minimum"))
		ttl := int(n.num("ttl"))
		changeDate := int(time.Now().Unix())
		if err := p.pdnsDB.WithContext(ctx).Model(&Record{}).
			Where("ispconfig_id = ? AND type = ?", ispID, "SOA").
			Updates(map[string]any{
				"name":        origin,
				"content":     content,
				"ttl":         ttl,
				"change_date": changeDate,
			}).Error; err != nil {
			return fmt.Errorf("powerdns: update SOA id=%d: %w", ispID, err)
		}
	} else {
		if err := p.handleSOAInsert(ctx, data); err != nil {
			return err
		}
		// Re-insert every active dns_rr for the zone (PHP foreach).
		if p.panelDB != nil {
			var rrs []model.DNSRr
			if err := p.panelDB.WithContext(ctx).
				Where("zone = ? AND active = ?", ispID, "Y").
				Find(&rrs).Error; err != nil {
				return fmt.Errorf("powerdns: list dns_rr for zone %d: %w", ispID, err)
			}
			for _, rr := range rrs {
				rrData := engine.Data{New: dnsRrToMap(rr)}
				if err := p.handleRRInsert(ctx, rrData); err != nil {
					return err
				}
			}
		}
	}

	p.handleDNSSEC(ctx, data)
	p.rectifyZone(ctx, data)
	p.notifySlave(ctx, data)
	p.queueRestart()
	return nil
}

// handleSOADelete ports soa_delete: drop MASTER domain + all records.
func (p *Plugin) handleSOADelete(ctx context.Context, data engine.Data) error {
	o := row(data.Old)
	// Prefer old.id; update-deactivate path may only have new.
	ispID := int(o.num("id"))
	if ispID == 0 {
		ispID = int(row(data.New).num("id"))
	}
	var dom Domain
	err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type = ?", ispID, "MASTER").
		Take(&dom).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return fmt.Errorf("powerdns: lookup MASTER domain ispconfig_id=%d: %w", ispID, err)
	}
	if err := p.pdnsDB.WithContext(ctx).Where("domain_id = ?", dom.ID).Delete(&Record{}).Error; err != nil {
		return fmt.Errorf("powerdns: delete records domain_id=%d: %w", dom.ID, err)
	}
	if err := p.pdnsDB.WithContext(ctx).Delete(&Domain{}, dom.ID).Error; err != nil {
		return fmt.Errorf("powerdns: delete domain id=%d: %w", dom.ID, err)
	}
	p.queueRestart()
	return nil
}

// handleRRInsert ports rr_insert.
func (p *Plugin) handleRRInsert(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	if !n.activeY() {
		return nil
	}
	ispID := int(n.num("id"))
	var exists Record
	// The SOA record carries the dns_soa id in the same column, so the
	// duplicate check must ignore it (PHP omits the filter and drops the
	// dns_rr whose id equals the zone id).
	if err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type != ?", ispID, "SOA").
		Take(&exists).Error; err == nil {
		return nil // duplicate ispconfig_id
	}

	zoneID := int(n.num("zone"))
	origin, err := p.zoneOrigin(ctx, zoneID, n)
	if err != nil {
		return err
	}
	if origin == "" {
		return nil // no panel zone yet
	}
	var pdnsDom Domain
	if err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type = ?", zoneID, "MASTER").
		Take(&pdnsDom).Error; err != nil {
		// SOA not active yet — skip (PHP parity).
		return nil
	}

	typ := n.str("type")
	name := RRName(n.str("name"), origin)
	content := RRContent(typ, n.str("data"), origin)
	ttl := int(n.num("ttl"))
	prio := int(n.num("aux"))
	changeDate := int(time.Now().Unix())
	domainID := pdnsDom.ID
	rec := Record{
		DomainID:    &domainID,
		Name:        &name,
		Type:        &typ,
		Content:     &content,
		TTL:         &ttl,
		Prio:        &prio,
		ChangeDate:  &changeDate,
		ISPConfigID: ispID,
	}
	if err := p.pdnsDB.WithContext(ctx).Create(&rec).Error; err != nil {
		return fmt.Errorf("powerdns: insert RR ispconfig_id=%d: %w", ispID, err)
	}
	p.rectifyZone(ctx, data)
	return nil
}

// handleRRUpdate ports rr_update.
func (p *Plugin) handleRRUpdate(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	o := row(data.Old)
	if !n.activeY() {
		if !o.activeY() {
			return nil
		}
		return p.handleRRDelete(ctx, data)
	}
	ispID := int(n.num("id"))
	var exists Record
	err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type != ?", ispID, "SOA").
		Take(&exists).Error
	if o.activeY() && err == nil {
		zoneID := int(n.num("zone"))
		origin, err := p.zoneOrigin(ctx, zoneID, n)
		if err != nil {
			return err
		}
		typ := n.str("type")
		name := RRName(n.str("name"), origin)
		content := RRContent(typ, n.str("data"), origin)
		ttl := int(n.num("ttl"))
		prio := int(n.num("aux"))
		changeDate := int(time.Now().Unix())
		if err := p.pdnsDB.WithContext(ctx).Model(&Record{}).
			Where("ispconfig_id = ? AND type != ?", ispID, "SOA").
			Updates(map[string]any{
				"name":        name,
				"type":        typ,
				"content":     content,
				"ttl":         ttl,
				"prio":        prio,
				"change_date": changeDate,
			}).Error; err != nil {
			return fmt.Errorf("powerdns: update RR ispconfig_id=%d: %w", ispID, err)
		}
		p.rectifyZone(ctx, data)
		return nil
	}
	return p.handleRRInsert(ctx, data)
}

// handleRRDelete ports rr_delete (never deletes SOA).
func (p *Plugin) handleRRDelete(ctx context.Context, data engine.Data) error {
	o := row(data.Old)
	ispID := int(o.num("id"))
	if ispID == 0 {
		ispID = int(row(data.New).num("id"))
	}
	if err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type != ?", ispID, "SOA").
		Delete(&Record{}).Error; err != nil {
		return fmt.Errorf("powerdns: delete RR ispconfig_id=%d: %w", ispID, err)
	}
	return nil
}

// handleSlaveInsert ports slave_insert.
func (p *Plugin) handleSlaveInsert(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	if !n.activeY() {
		return nil
	}
	origin := stripTrailingDot(n.str("origin"))
	ispID := int(n.num("id"))
	master := n.str("ns")
	dom := Domain{
		Name:        origin,
		Type:        "SLAVE",
		Master:      &master,
		ISPConfigID: ispID,
	}
	if err := p.pdnsDB.WithContext(ctx).Create(&dom).Error; err != nil {
		return fmt.Errorf("powerdns: insert SLAVE %s: %w", origin, err)
	}
	p.fetchFromMaster(ctx, data)
	p.queueRestart()
	return nil
}

// handleSlaveUpdate ports slave_update.
func (p *Plugin) handleSlaveUpdate(ctx context.Context, data engine.Data) error {
	n := row(data.New)
	o := row(data.Old)
	if !n.activeY() {
		if !o.activeY() {
			return nil
		}
		return p.handleSlaveDelete(ctx, data)
	}
	if o.activeY() {
		origin := stripTrailingDot(n.str("origin"))
		ispID := int(n.num("id"))
		master := n.str("ns")
		if err := p.pdnsDB.WithContext(ctx).Model(&Domain{}).
			Where("ispconfig_id = ? AND type = ?", ispID, "SLAVE").
			Updates(map[string]any{
				"name":   origin,
				"type":   "SLAVE",
				"master": master,
			}).Error; err != nil {
			return fmt.Errorf("powerdns: update SLAVE ispconfig_id=%d: %w", ispID, err)
		}
		var dom Domain
		if err := p.pdnsDB.WithContext(ctx).
			Where("ispconfig_id = ? AND type = ?", ispID, "SLAVE").
			Take(&dom).Error; err == nil {
			// Purge AXFR cache records (ispconfig_id = 0).
			_ = p.pdnsDB.WithContext(ctx).
				Where("domain_id = ? AND ispconfig_id = 0", dom.ID).
				Delete(&Record{}).Error
		}
		p.fetchFromMaster(ctx, data)
		p.queueRestart()
		return nil
	}
	return p.handleSlaveInsert(ctx, data)
}

// handleSlaveDelete ports slave_delete.
func (p *Plugin) handleSlaveDelete(ctx context.Context, data engine.Data) error {
	o := row(data.Old)
	ispID := int(o.num("id"))
	if ispID == 0 {
		ispID = int(row(data.New).num("id"))
	}
	var dom Domain
	err := p.pdnsDB.WithContext(ctx).
		Where("ispconfig_id = ? AND type = ?", ispID, "SLAVE").
		Take(&dom).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return fmt.Errorf("powerdns: lookup SLAVE ispconfig_id=%d: %w", ispID, err)
	}
	if err := p.pdnsDB.WithContext(ctx).Where("domain_id = ?", dom.ID).Delete(&Record{}).Error; err != nil {
		return err
	}
	if err := p.pdnsDB.WithContext(ctx).Delete(&Domain{}, dom.ID).Error; err != nil {
		return err
	}
	p.queueRestart()
	return nil
}

// zoneOrigin resolves the zone origin (without trailing dot) for an RR.
// When the panel DB is nil (pure mapper tests), origin may come from payload.
func (p *Plugin) zoneOrigin(ctx context.Context, zoneID int, n row) (string, error) {
	if p.panelDB != nil && zoneID > 0 {
		var soa model.DNSSoa
		if err := p.panelDB.WithContext(ctx).Select("origin").Take(&soa, zoneID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return "", nil
			}
			return "", fmt.Errorf("powerdns: load dns_soa %d: %w", zoneID, err)
		}
		return stripTrailingDot(soa.Origin), nil
	}
	// Fallback: parent origin on payload (tests).
	if o := stripTrailingDot(n.str("origin")); o != "" {
		return o, nil
	}
	return "", nil
}

func dnsRrToMap(rr model.DNSRr) map[string]any {
	return map[string]any{
		"id":        rr.ID,
		"server_id": rr.ServerID,
		"zone":      rr.Zone,
		"name":      rr.Name,
		"type":      rr.Type,
		"data":      rr.Data,
		"aux":       rr.Aux,
		"ttl":       rr.TTL,
		"active":    rr.Active,
	}
}

// Control-plane hooks (filled in section 3); safe no-ops until wired.
func (p *Plugin) zoneRediscover(ctx context.Context) {
	if p == nil {
		return
	}
	p.doRediscover(ctx)
}

func (p *Plugin) notifySlave(ctx context.Context, data engine.Data) {
	if p == nil {
		return
	}
	p.doNotify(ctx, data)
}

func (p *Plugin) fetchFromMaster(ctx context.Context, data engine.Data) {
	if p == nil {
		return
	}
	p.doRetrieve(ctx, data)
}

func (p *Plugin) rectifyZone(ctx context.Context, data engine.Data) {
	if p == nil {
		return
	}
	p.doRectify(ctx, data)
}

func (p *Plugin) handleDNSSEC(ctx context.Context, data engine.Data) {
	if p == nil {
		return
	}
	p.doHandleDNSSEC(ctx, data)
}

func (p *Plugin) queueRestart() {
	if p == nil || p.services == nil {
		return
	}
	p.services.RestartServiceDelayed(ServiceName, engine.ActionRestart)
}
