package monitor

import "testing"

func TestParseRepquota(t *testing.T) {
	const raw = `*** Report for user quotas on device /dev/sda1
Block grace time: 7days; Inode grace time: 7days
                        Block limits                File limits
User            used    soft    hard  grace    used  soft  hard  grace
----------------------------------------------------------------------
root      --  123456       0       0          1234     0     0
web1      +-    5000    4000  110000  6days     100     0     0
`
	got := parseRepquota(raw)
	if len(got) != 2 {
		t.Fatalf("want 2 users, got %d: %v", len(got), got)
	}
	if e := got["web1"]; e.Used != 5000 || e.Soft != 4000 || e.Hard != 110000 {
		t.Errorf("web1 = %+v", e)
	}
	if e := got["root"]; e.Used != 123456 || e.Soft != 0 {
		t.Errorf("root = %+v", e)
	}
}

func TestParseDoveadm(t *testing.T) {
	const raw = `Username        Type    Value   Limit   %
a@example.com   STORAGE 1024    10240   10
a@example.com   MESSAGE 5       -       0
b@example.com   STORAGE 0       -       0
`
	got := parseDoveadm(raw)
	if got["a@example.com"] != 1024*1024 {
		t.Errorf("a used = %d, want bytes", got["a@example.com"])
	}
	if _, ok := got["b@example.com"]; !ok {
		t.Error("b missing")
	}
	if len(got) != 2 {
		t.Errorf("MESSAGE row leaked: %v", got)
	}
}
