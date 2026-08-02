package clientdb

import "testing"

func TestRewriteViewCreate(t *testing.T) {
	in := "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER VIEW `v_items` AS select `c1_vdb`.`items`.`id` AS `id` from `c1_vdb`.`items`"
	got := rewriteViewCreate(in, "c1_vdb", "c1_vnew")
	if got == "" {
		t.Fatal("empty rewrite")
	}
	if !containsAll(got, "`c1_vnew`.`v_items`", "`c1_vnew`.`items`") {
		t.Fatalf("unexpected rewrite: %s", got)
	}
	if containsAll(got, "DEFINER=") {
		t.Fatalf("DEFINER not stripped: %s", got)
	}
	if containsAll(got, "c1_vdb") {
		t.Fatalf("old schema remains: %s", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !stringContains(s, p) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
