package cron

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	cronFieldChars = regexp.MustCompile(`^[0-9,/*-]+$`)
	cronToken      = regexp.MustCompile(`^((\d+)(-(\d+))?|\*)(/([1-9]\d*))?$`)
	cronSeparators = regexp.MustCompile(`[-,\/][-\,\/]`)
)

type cronField struct {
	min  int
	max  int
	unit int
}

var cronFields = map[string]cronField{
	"run_min":   {min: 0, max: 59, unit: 1},
	"run_hour":  {min: 0, max: 23, unit: 60},
	"run_mday":  {min: 1, max: 31, unit: 1440},
	"run_month": {min: 1, max: 12, unit: 1440 * 28},
	"run_wday":  {min: 0, max: 7, unit: 1440},
}

// ValidateRunTime ports validate_cron.inc.php::run_time_format for min/hour/
// mday/wday: charset, ranges, step/range syntax. run_month must use
// ValidateRunMonth (which also allows @reboot).
func ValidateRunTime(field, value string) error {
	if field == "run_month" {
		return errors.New("run_month requires month validation")
	}
	return validateRunField(field, value)
}

// ValidateRunMonth ports validate_cron.inc.php::run_month_format: same rules
// as ValidateRunTime plus the literal @reboot token.
func ValidateRunMonth(value string) error {
	if value == "@reboot" {
		return nil
	}
	return validateRunField("run_month", value)
}

func validateRunField(field, value string) error {
	limits, ok := cronFields[field]
	if !ok || field == "run_month" && value == "@reboot" {
		if !ok {
			return fmt.Errorf("unknown cron field %q", field)
		}
		return nil
	}

	value = strings.ReplaceAll(value, " ", "")
	if value == "" || !cronFieldChars.MatchString(value) || cronSeparators.MatchString(value) {
		return errors.New("invalid cron schedule")
	}

	for token := range strings.SplitSeq(value, ",") {
		matches := cronToken.FindStringSubmatch(token)
		if matches == nil {
			return errors.New("invalid cron schedule")
		}
		if matches[2] != "" {
			first, err := strconv.Atoi(matches[2])
			if err != nil || first < limits.min || first > limits.max {
				return errors.New("invalid cron schedule")
			}
			if matches[4] != "" {
				last, err := strconv.Atoi(matches[4])
				if err != nil || last < limits.min || last > limits.max || last <= first {
					return errors.New("invalid cron schedule")
				}
			}
		}
		if matches[6] != "" {
			step, err := strconv.Atoi(matches[6])
			if err != nil || step < 2 || step > limits.max-1 {
				return errors.New("invalid cron schedule")
			}
		}
	}
	return nil
}

// MinFrequencyMinutes ports validate_cron.inc.php cron_min_freq: the smallest
// interval in minutes implied by the five schedule fields (wrap-around, single
// value, and */n step cases). @reboot month is skipped for the frequency floor.
func MinFrequencyMinutes(runMin, runHour, runMday, runMonth, runWday string) (int, error) {
	fields := []struct {
		name  string
		value string
	}{
		{"run_min", runMin},
		{"run_hour", runHour},
		{"run_mday", runMday},
		{"run_month", runMonth},
		{"run_wday", runWday},
	}

	minimum := -1
	for _, field := range fields {
		if field.name == "run_month" && field.value == "@reboot" {
			continue
		}
		if err := validateRunField(field.name, field.value); err != nil {
			return 0, fmt.Errorf("%s: %w", field.name, err)
		}
		raw, err := fieldFrequency(field.name, field.value)
		if err != nil {
			return 0, err
		}
		// PHP only stores a field's frequency when it stays inside the
		// field's own cycle (validate_cron.inc.php:219): a once-per-cycle
		// field such as run_hour="0" says nothing about the interval — the
		// coarser fields do. Without this guard "0 0 * * *" (daily) would
		// report 60 minutes instead of 1440.
		limits := cronFields[field.name]
		if raw <= 0 || raw > limits.max {
			continue
		}
		frequency := raw * limits.unit
		if minimum < 0 || frequency < minimum {
			minimum = frequency
		}
	}
	if minimum < 0 {
		return 0, errors.New("cron schedule has no recurring fields")
	}
	return minimum, nil
}

func fieldFrequency(field, value string) (int, error) {
	limits := cronFields[field]
	value = strings.ReplaceAll(value, " ", "")
	used := make([]int, 0, limits.max-limits.min+1)
	for token := range strings.SplitSeq(value, ",") {
		matches := cronToken.FindStringSubmatch(token)
		if matches == nil {
			return 0, errors.New("invalid cron schedule")
		}
		from, to, step := limits.min, limits.max, 1
		if matches[1] != "*" {
			from, _ = strconv.Atoi(matches[2])
			to = from
			if matches[4] != "" {
				to, _ = strconv.Atoi(matches[4])
			}
		}
		if matches[6] != "" {
			step, _ = strconv.Atoi(matches[6])
		}
		for value := from; value <= to; value += step {
			used = append(used, value)
		}
	}
	sort.Ints(used)
	unique := used[:0]
	for _, value := range used {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	minimum := -1
	for index := 1; index < len(unique); index++ {
		gap := unique[index] - unique[index-1]
		if minimum < 0 || gap < minimum {
			minimum = gap
		}
	}
	wrap := unique[0] - limits.min + limits.max - unique[len(unique)-1] + 1
	if minimum < 0 || wrap < minimum {
		minimum = wrap
	}
	return minimum, nil
}
