package cron

import (
	"regexp"

	"go-ispconfig/internal/model"
)

var urlCommand = regexp.MustCompile(`(?i)^https?://`)

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
