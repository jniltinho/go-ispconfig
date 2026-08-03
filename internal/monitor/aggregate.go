package monitor

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// Types that contribute messages and severity to system state (design D5).
var stateContributingTypes = []string{
	"disk_usage",
	"server_load",
	"services",
	"system_update",
	"sys_log",
}

// Message is one human-readable check result for the system state UI.
type Message struct {
	// Severity bucket: ok, info, warning, critical, error, unknown.
	Severity string `json:"severity"`
	// Type is the monitor_data type that produced the message.
	Type string `json:"type"`
	// Text is a short English summary (i18n keys resolved by the UI).
	Text string `json:"text"`
}

// CheckSummary is the latest sample summary for one type.
type CheckSummary struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Created uint32 `json:"created"`
}

// ServerState is the aggregated system-state view for one server.
type ServerState struct {
	ServerID    uint32         `json:"server_id"`
	ServerName  string         `json:"server_name"`
	State       string         `json:"state"`
	OSName      string         `json:"os_name,omitempty"`
	OSVersion   string         `json:"os_version,omitempty"`
	ISPCName    string         `json:"ispc_name,omitempty"`
	ISPCVersion string         `json:"ispc_version,omitempty"`
	Messages    []Message      `json:"messages"`
	Counts      map[string]int `json:"counts"`
	Checks      []CheckSummary `json:"checks"`
}

// AggregateServerState builds the per-server overview from newest monitor_data
// rows (port of show_sys_state.php _getServerState / _processDbState).
func AggregateServerState(ctx context.Context, db *gorm.DB, serverID uint32, serverName string) (ServerState, error) {
	out := ServerState{
		ServerID:   serverID,
		ServerName: serverName,
		State:      "ok",
		Messages:   []Message{},
		Counts: map[string]int{
			"ok": 0, "info": 0, "warning": 0, "critical": 0, "error": 0, "unknown": 0,
		},
		Checks: []CheckSummary{},
	}

	// Newest row per type: fetch all rows ordered by created desc and keep first of each type.
	var rows []model.MonitorData
	err := db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Order("created DESC").
		Find(&rows).Error
	if err != nil {
		return out, fmt.Errorf("monitor: load data for server %d: %w", serverID, err)
	}

	newest := map[string]model.MonitorData{}
	for _, r := range rows {
		if _, ok := newest[r.Type]; !ok {
			newest[r.Type] = r
		}
	}

	for typ, row := range newest {
		out.Checks = append(out.Checks, CheckSummary{
			Type: typ, State: row.State, Created: row.Created,
		})
		// openvz_beancounter is ignored in PHP; we skip similarly.
		if typ == "openvz_beancounter" {
			continue
		}
		// Always fold severity for all types (PHP folds every type's state).
		out.State = SetState(out.State, row.State)

		if typ == "os_info" {
			dec := DecodePayload(row.Data)
			if m, ok := asStringMap(dec.Value); ok {
				if v, ok := m["name"].(string); ok {
					out.OSName = v
				}
				if v, ok := m["version"].(string); ok {
					out.OSVersion = v
				}
			}
		}
		if typ == "ispc_info" {
			dec := DecodePayload(row.Data)
			if m, ok := asStringMap(dec.Value); ok {
				if v, ok := m["name"].(string); ok {
					out.ISPCName = v
				}
				if v, ok := m["version"].(string); ok {
					out.ISPCVersion = v
				}
			}
		}

		if msg, ok := messageForType(typ, row.State); ok {
			out.Messages = append(out.Messages, msg)
			sev := msg.Severity
			if _, exists := out.Counts[sev]; exists {
				out.Counts[sev]++
			}
		}
	}

	// Ensure contributing types with no data still surface as unknown when
	// we want empty overview honesty — only when no rows at all.
	if len(newest) == 0 {
		out.State = "unknown"
	}
	return out, nil
}

// AggregateSystemState builds overview cards for every accessible server.
func AggregateSystemState(ctx context.Context, db *gorm.DB, servers []model.Server) ([]ServerState, error) {
	out := make([]ServerState, 0, len(servers))
	for _, s := range servers {
		st, err := AggregateServerState(ctx, db, s.ServerID, s.ServerName)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func messageForType(typ, state string) (Message, bool) {
	// Only types that contribute UI messages (design D5).
	switch typ {
	case "disk_usage", "server_load", "services", "system_update", "sys_log":
	default:
		return Message{}, false
	}
	// no_state does not produce a message.
	if state == "no_state" {
		return Message{}, false
	}
	// ok messages are still listed (under the ok bucket) so the UI can show
	// healthy checks.
	return Message{Severity: state, Type: typ, Text: typeMessageText(typ, state)}, true
}

func typeMessageText(typ, state string) string {
	// Stable English keys the UI can also i18n; concise for API consumers.
	switch typ {
	case "disk_usage":
		switch state {
		case "ok":
			return "Disk usage is OK"
		case "info":
			return "Disk is going full"
		case "warning":
			return "Disk is nearly full"
		case "critical":
			return "Disk is very full"
		case "error":
			return "Disk is full"
		default:
			return "Disk usage unknown"
		}
	case "server_load":
		switch state {
		case "ok":
			return "Server load is OK"
		case "info":
			return "Server load is elevated"
		case "warning":
			return "Server load is high"
		case "critical":
			return "Server load is critical"
		case "error":
			return "Server load is extreme"
		default:
			return "Server load unknown"
		}
	case "services":
		if state == "ok" {
			return "All monitored services are running"
		}
		return "One or more services are down"
	case "system_update":
		switch state {
		case "ok":
			return "System is up to date"
		case "info":
			return "System updates are available"
		default:
			return "System update status unknown"
		}
	case "sys_log":
		switch state {
		case "ok":
			return "No open system log warnings"
		case "warning":
			return "Open system log warnings"
		case "error":
			return "Open system log errors"
		default:
			return "System log status unknown"
		}
	default:
		return typ + ": " + state
	}
}

// ContributingTypes returns the type list used for message aggregation.
func ContributingTypes() []string {
	out := make([]string, len(stateContributingTypes))
	copy(out, stateContributingTypes)
	return out
}
