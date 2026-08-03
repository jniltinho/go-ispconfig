package cron

import (
	"regexp"

	"go-ispconfig/internal/model"
)

var urlCommand = regexp.MustCompile(`(?i)^https?://`)

// DeriveType ports cron_edit.php::onSubmit type auto-derivation: URL scheme
// forces type=url; otherwise owner limit_cron_type maps to full/chrooted;
// admin-owned sites default to full.
func DeriveType(command, ownerLimitType string, adminOwned bool) string {
	if urlCommand.MatchString(command) {
		return model.CronTypeURL
	}
	if adminOwned {
		return model.CronTypeFull
	}
	if ownerLimitType == model.CronTypeFull {
		return model.CronTypeFull
	}
	return model.CronTypeChrooted
}
