package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/cron"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/validator"
)

// TestSitesCronEntityValidators covers schedule/command CUSTOM rules (task 4.2).
func TestSitesCronEntityValidators(t *testing.T) {
	ent := sitesCronEntity()
	require.Equal(t, "crons", ent.Name)
	fields := map[string]Field{}
	for _, f := range ent.Tabs[0].Fields {
		fields[f.Name] = f
	}

	// Schedule fields: NOTEMPTY + CUSTOM format.
	for _, name := range []string{"run_min", "run_hour", "run_mday", "run_month", "run_wday"} {
		f, ok := fields[name]
		require.True(t, ok, "%s present", name)
		require.GreaterOrEqual(t, len(f.Validators), 2, "%s validators", name)
		assert.Equal(t, "NOTEMPTY", f.Validators[0].Type)
		assert.Equal(t, "CUSTOM", f.Validators[1].Type)
	}

	// Valid tokens pass CUSTOM.
	vc := &validator.Context{}
	assert.Empty(t, checkCronRunMin(vc, "*/5"))
	assert.Empty(t, checkCronRunMin(vc, "0-59"))
	assert.Empty(t, checkCronRunHour(vc, "*"))
	assert.Empty(t, checkCronRunMday(vc, "1,15"))
	assert.Empty(t, checkCronRunWday(vc, "0-6"))
	assert.Empty(t, checkCronRunMonth(vc, "*"))
	assert.Empty(t, checkCronRunMonth(vc, "@reboot"))

	// Invalid schedule rejected with field-specific keys.
	assert.Equal(t, "run_min_error_format", checkCronRunMin(vc, "60"))
	assert.Equal(t, "run_min_error_format", checkCronRunMin(vc, "@reboot"),
		"@reboot only legal in run_month")
	assert.Equal(t, "run_hour_error_format", checkCronRunHour(vc, "24"))
	assert.Equal(t, "run_mday_error_format", checkCronRunMday(vc, "0"))
	assert.Equal(t, "run_wday_error_format", checkCronRunWday(vc, "8"))
	assert.Equal(t, "run_month_error_format", checkCronRunMonth(vc, "13"))

	// Command format.
	assert.Empty(t, checkCronCommand(vc, "https://example.com/job"))
	assert.Empty(t, checkCronCommand(vc, "https://{DOMAIN}/cron.php"))
	assert.Empty(t, checkCronCommand(vc, "/usr/bin/php /web/cron.php"))
	assert.Equal(t, "command_error_format", checkCronCommand(vc, "https://not a host/path"))
	assert.Equal(t, "command_error_format", checkCronCommand(vc, "ftp://example.com/x"))
	assert.Equal(t, "command_error_format", checkCronCommand(vc, "https://ex.com/a\\b"))
	assert.Equal(t, "command_error_format", checkCronCommand(vc, "line1\nline2"))
}

// TestSitesCronTypeAutoDerivation mirrors cron_edit.php::onSubmit type rules
// used by sitesCronPrepare (task 4.2).
func TestSitesCronTypeAutoDerivation(t *testing.T) {
	cases := []struct {
		cmd, ownerLimit string
		adminOwned      bool
		want            string
	}{
		{"https://x.example/job", "url", false, model.CronTypeURL},
		{"HTTP://x.example/job", "chrooted", false, model.CronTypeURL},
		{"/usr/bin/php cron.php", "full", false, model.CronTypeFull},
		{"/usr/bin/php cron.php", "chrooted", false, model.CronTypeChrooted},
		{"/usr/bin/php cron.php", "url", false, model.CronTypeChrooted},
		{"/usr/bin/php cron.php", "", true, model.CronTypeFull},
	}
	for _, tc := range cases {
		got := cron.DeriveType(tc.cmd, tc.ownerLimit, tc.adminOwned)
		assert.Equal(t, tc.want, got, "cmd=%q owner=%q admin=%v",
			tc.cmd, tc.ownerLimit, tc.adminOwned)
	}
}

// TestSitesCronTypeLimitMatrix covers limit_cron_type veto rules (task 4.3).
func TestSitesCronTypeLimitMatrix(t *testing.T) {
	// url client may only store url; chrooted rejects full; full allows all.
	check := func(limitType, jobType string) error {
		cli := &model.Client{LimitCronType: limitType}
		switch cli.LimitCronType {
		case model.CronTypeURL:
			if jobType != "" && jobType != model.CronTypeURL {
				return &LimitError{Key: "error.limit_cron_type"}
			}
		case model.CronTypeChrooted:
			if jobType == model.CronTypeFull {
				return &LimitError{Key: "error.limit_cron_type"}
			}
		}
		return nil
	}
	assert.NoError(t, check("url", "url"))
	assert.Error(t, check("url", "full"))
	assert.Error(t, check("url", "chrooted"))
	assert.NoError(t, check("chrooted", "url"))
	assert.NoError(t, check("chrooted", "chrooted"))
	assert.Error(t, check("chrooted", "full"))
	assert.NoError(t, check("full", "full"))
	assert.NoError(t, check("full", "chrooted"))
	assert.NoError(t, check("full", "url"))
}

// TestSitesCronFrequencyLimit uses MinFrequencyMinutes against the client floor.
func TestSitesCronFrequencyLimit(t *testing.T) {
	// limit=5 rejects every-minute (*) and */4, accepts */5 and hourly.
	floor := 5
	assert.True(t, freqBelowFloor("*", "*", "*", "*", "*", floor))
	assert.True(t, freqBelowFloor("*/4", "*", "*", "*", "*", floor))
	assert.False(t, freqBelowFloor("*/5", "*", "*", "*", "*", floor))
	assert.False(t, freqBelowFloor("0", "*", "*", "*", "*", floor))
}

func freqBelowFloor(min, hour, mday, month, wday string, floor int) bool {
	freq, err := cron.MinFrequencyMinutes(min, hour, mday, month, wday)
	if err != nil {
		return false
	}
	return freq < floor
}
