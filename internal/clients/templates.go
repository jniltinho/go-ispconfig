package clients

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// ParseAdditionalList parses the legacy slash-separated template id list
// (client.template_additional, e.g. "2/5/2"); duplicates are meaningful
// (each assignment adds its limits once).
func ParseAdditionalList(raw string) []int32 {
	var ids []int32
	for _, part := range strings.Split(raw, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 32)
		if err != nil || n <= 0 {
			continue
		}
		ids = append(ids, int32(n))
	}
	return ids
}

// SetAssignedTemplates reconciles client_template_assigned so the client
// carries exactly the given additional template ids, as a multiset (the
// same template may be assigned more than once and its limits add up
// each time — PHP update_client_templates old-style branch).
func SetAssignedTemplates(ctx context.Context, tx *gorm.DB, clientID int64, templateIDs []int32) error {
	needed := map[int32]int{}
	for _, id := range templateIDs {
		needed[id]++
	}
	var inDB []model.ClientTemplateAssigned
	err := tx.WithContext(ctx).Where("client_id = ?", clientID).Find(&inDB).Error
	if err != nil {
		return fmt.Errorf("clients: loading template assignments: %w", err)
	}
	for _, row := range inDB {
		needed[row.ClientTemplateID]--
	}
	for id, count := range needed {
		for ; count > 0; count-- {
			row := model.ClientTemplateAssigned{ClientID: clientID, ClientTemplateID: id}
			if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
				return fmt.Errorf("clients: assigning template %d: %w", id, err)
			}
		}
		for ; count < 0; count++ {
			err := tx.WithContext(ctx).
				Where("client_id = ? AND client_template_id = ?", clientID, id).
				Limit(1).Delete(&model.ClientTemplateAssigned{}).Error
			if err != nil {
				return fmt.Errorf("clients: unassigning template %d: %w", id, err)
			}
		}
	}
	return nil
}

// MigrateLegacyAdditional moves a legacy client.template_additional
// slash-list into client_template_assigned rows and clears the column
// (PHP apply_client_templates does the same on first save). Returns
// whether a migration happened.
func MigrateLegacyAdditional(ctx context.Context, tx *gorm.DB, c *model.Client) (bool, error) {
	if c.TemplateAdditional == "" {
		return false, nil
	}
	ids := ParseAdditionalList(c.TemplateAdditional)
	// Legacy list is merged on top of whatever is already assigned.
	var existing []model.ClientTemplateAssigned
	err := tx.WithContext(ctx).Where("client_id = ?", c.ClientID).Find(&existing).Error
	if err != nil {
		return false, fmt.Errorf("clients: loading template assignments: %w", err)
	}
	for _, row := range existing {
		ids = append(ids, row.ClientTemplateID)
	}
	if err := SetAssignedTemplates(ctx, tx, int64(c.ClientID), ids); err != nil {
		return false, err
	}
	err = tx.WithContext(ctx).Model(&model.Client{}).
		Where("client_id = ?", c.ClientID).Update("template_additional", "").Error
	if err != nil {
		return false, fmt.Errorf("clients: clearing template_additional: %w", err)
	}
	c.TemplateAdditional = ""
	return true, nil
}

// AssignedTemplates loads the additional templates of a client, in
// assignment order, resolving each id (deleted templates are skipped,
// PHP parity).
func AssignedTemplates(ctx context.Context, db *gorm.DB, clientID int64) ([]model.ClientTemplate, error) {
	var rows []model.ClientTemplateAssigned
	err := db.WithContext(ctx).Where("client_id = ?", clientID).
		Order("assigned_template_id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("clients: loading template assignments: %w", err)
	}
	var out []model.ClientTemplate
	for _, row := range rows {
		var tpl model.ClientTemplate
		err := db.WithContext(ctx).Where("template_id = ?", row.ClientTemplateID).Take(&tpl).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue // template deleted in the meantime
		}
		if err != nil {
			return nil, fmt.Errorf("clients: loading template %d: %w", row.ClientTemplateID, err)
		}
		out = append(out, tpl)
	}
	return out, nil
}
