package monitor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CollectSystemUpdate runs a best-effort apt summary (Debian/Ubuntu). When
// apt is unavailable, returns no_state with an empty summary (design D4
// optional job).
func CollectSystemUpdate(ctx context.Context) (map[string]any, string, error) {
	if _, err := exec.LookPath("apt-get"); err != nil {
		return map[string]any{
			"output":      "apt-get not available",
			"unsupported": true,
		}, "no_state", nil
	}
	// Simulation only — never upgrades. Short timeout for scheduler safety.
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil && len(text) == 0 {
		return map[string]any{
			"output": fmt.Sprintf("apt-get simulate failed: %v", err),
		}, "info", nil
	}
	// Count upgradeable packages from "Inst " lines (simulate output).
	upgradable := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Inst ") {
			upgradable++
		}
	}
	state := "ok"
	if upgradable > 0 {
		state = "info"
	}
	return map[string]any{
		"output":     text,
		"upgradable": upgradable,
	}, state, nil
}
