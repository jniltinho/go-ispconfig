package cron

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var (
	urlScheme    = regexp.MustCompile(`^\w+://`)
	cronHostname = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,62}\.)+([a-zA-Z0-9-]{2,63})$`)
)

func ValidateCommand(command, domain string) error {
	if strings.ContainsAny(command, "\r\n\x00") {
		return errors.New("invalid cron command")
	}
	if !urlScheme.MatchString(command) {
		return nil
	}
	if strings.Contains(command, `\`) {
		return errors.New("invalid cron command")
	}
	expanded := strings.ReplaceAll(command, "{DOMAIN}", domain)
	parsed, err := url.Parse(expanded)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") || !cronHostname.MatchString(parsed.Hostname()) {
		return errors.New("invalid cron command")
	}
	return nil
}
