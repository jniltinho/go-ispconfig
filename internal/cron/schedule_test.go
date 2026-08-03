package cron

import "testing"

func TestValidateRunTime(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
		valid bool
	}{
		{name: "wildcard", field: "run_min", value: "*", valid: true},
		{name: "list and range", field: "run_hour", value: "1-3,8", valid: true},
		{name: "step wildcard", field: "run_min", value: "*/5", valid: true},
		{name: "step range", field: "run_mday", value: "1-31/2", valid: true},
		{name: "spaces are ignored", field: "run_wday", value: "0, 2, 4", valid: true},
		{name: "minute upper bound", field: "run_min", value: "59", valid: true},
		{name: "hour upper bound", field: "run_hour", value: "23", valid: true},
		{name: "day starts at one", field: "run_mday", value: "1", valid: true},
		{name: "weekday seven", field: "run_wday", value: "7", valid: true},
		{name: "minute out of range", field: "run_min", value: "60", valid: false},
		{name: "hour out of range", field: "run_hour", value: "24", valid: false},
		{name: "day zero", field: "run_mday", value: "0", valid: false},
		{name: "month is separate validator", field: "run_month", value: "1", valid: false},
		{name: "descending range", field: "run_min", value: "5-2", valid: false},
		{name: "range endpoint equal", field: "run_min", value: "5-5", valid: false},
		{name: "invalid step zero", field: "run_min", value: "*/0", valid: false},
		{name: "invalid step one", field: "run_min", value: "*/1", valid: false},
		{name: "adjacent separators", field: "run_min", value: "1,,2", valid: false},
		{name: "invalid character", field: "run_min", value: "1+2", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateRunTime(test.field, test.value) == nil; got != test.valid {
				t.Fatalf("ValidateRunTime(%q, %q) valid = %v, want %v", test.field, test.value, got, test.valid)
			}
		})
	}
}

func TestValidateRunMonth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "reboot", value: "@reboot", valid: true},
		{name: "month range", value: "1-12", valid: true},
		{name: "month step", value: "*/3", valid: true},
		{name: "month zero", value: "0", valid: false},
		{name: "reboot with spaces", value: "@ reboot", valid: false},
		{name: "reboot in list", value: "@reboot,1", valid: false},
		{name: "month thirteen", value: "13", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateRunMonth(test.value) == nil; got != test.valid {
				t.Fatalf("ValidateRunMonth(%q) valid = %v, want %v", test.value, got, test.valid)
			}
		})
	}
}

func TestMinFrequencyMinutes(t *testing.T) {
	tests := []struct {
		name    string
		min     string
		hour    string
		mday    string
		month   string
		wday    string
		want    int
		wantErr bool
	}{
		{name: "every minute", min: "*", hour: "*", mday: "*", month: "*", wday: "*", want: 1},
		{name: "step", min: "*/5", hour: "*", mday: "*", month: "*", wday: "*", want: 5},
		{name: "single value", min: "5", hour: "1", mday: "1", month: "1", wday: "1", want: 60},
		{name: "list gap", min: "0,30", hour: "1", mday: "1", month: "1", wday: "1", want: 30},
		{name: "wrap around", min: "55,0", hour: "1", mday: "1", month: "1", wday: "1", want: 5},
		{name: "reboot ignores month", min: "*/5", hour: "*", mday: "*", month: "@reboot", wday: "*", want: 5},
		{name: "invalid field", min: "60", hour: "*", mday: "*", month: "*", wday: "*", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MinFrequencyMinutes(test.min, test.hour, test.mday, test.month, test.wday)
			if test.wantErr {
				if err == nil {
					t.Fatal("MinFrequencyMinutes returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("MinFrequencyMinutes returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("MinFrequencyMinutes = %d, want %d", got, test.want)
			}
		})
	}
}
