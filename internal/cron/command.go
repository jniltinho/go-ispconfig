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

// ValidateCommand ports validate_cron.inc.php::command_format: reject CR/LF/NUL;
// for URL commands require http/https, a hostname-shaped host after {DOMAIN}
// expansion, and no backslash.
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
	if err != nil {
		return errors.New("invalid cron command")
	}
	schemeOK := strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
	if !schemeOK || !cronHostname.MatchString(parsed.Hostname()) {
		return errors.New("invalid cron command")
	}
	return nil
}
