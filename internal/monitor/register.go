package monitor

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// Cron specs matching ISPConfig monitor cron.d schedules.
const (
	SpecEvery5Min  = "*/5 * * * *"
	SpecEvery15Min = "*/15 * * * *"
	SpecEveryMin   = "* * * * *"
	SpecHourly     = "0 * * * *"
)

// RegisterOptions configures monitor job registration.
type RegisterOptions struct {
	ServerID uint32
	MySQLDSN string
	LogPaths LogPaths
	Prober   Prober
	Version  string
	// EnableSystemUpdate registers the optional hourly system_update job.
	EnableSystemUpdate bool
	Log                *slog.Logger
}

// RegisterJobs registers all monitor_* collection jobs on the foundation
// scheduler, which mirrors each job's cron spec into sys_config group
// scheduler as {name}_spec so a standalone serve process can list metadata.
func RegisterJobs(sched *engine.Scheduler, db *gorm.DB, opts RegisterOptions) error {
	if sched == nil {
		return fmt.Errorf("monitor: nil scheduler")
	}
	if db == nil {
		return fmt.Errorf("monitor: nil db")
	}
	if opts.ServerID == 0 {
		return fmt.Errorf("monitor: server_id required")
	}
	if opts.Version != "" {
		Version = opts.Version
	}
	if opts.LogPaths.ISPConfig == "" {
		opts.LogPaths = DefaultLogPaths()
	}
	if opts.Prober == nil {
		opts.Prober = NetProber{MySQLDSN: opts.MySQLDSN}
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	type entry struct {
		name string
		spec string
		fn   engine.JobFunc
	}

	serverID := opts.ServerID
	entries := []entry{
		{"monitor_cpu_info", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectCPUInfo(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "cpu_info", data, state, 0)
		}},
		{"monitor_mem_usage", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectMemUsage(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "mem_usage", data, state, 0)
		}},
		{"monitor_disk_usage", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectDiskUsage(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "disk_usage", data, state, 0)
		}},
		{"monitor_server_load", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectServerLoad(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "server_load", data, state, 0)
		}},
		{"monitor_services", SpecEvery5Min, func(ctx context.Context) error {
			return RunServicesCollector(ctx, db, serverID, opts.Prober, opts.MySQLDSN)
		}},
		{"monitor_os_info", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectOSInfo(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "os_info", data, state, 0)
		}},
		{"monitor_kernel_info", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectKernelInfo(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "kernel_info", data, state, 0)
		}},
		{"monitor_ispc_info", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectISPCInfo(ctx)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "ispc_info", data, state, 0)
		}},
		{"monitor_sys_usage", SpecEveryMin, func(ctx context.Context) error {
			return RunSysUsageCollector(ctx, db, serverID)
		}},
		{"monitor_quota", SpecEvery15Min, func(ctx context.Context) error {
			return RunQuotaCollectors(ctx, db, serverID)
		}},
		{"monitor_sys_log", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectSysLogState(ctx, db, serverID)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "sys_log", data, state, 0)
		}},
		{"monitor_log_ispconfig", SpecEvery5Min, func(ctx context.Context) error {
			data, state, err := CollectLogTail(opts.LogPaths.ISPConfig, MaxLogLines)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "log_ispconfig", data, state, 0)
		}},
		{"monitor_log_letsencrypt", SpecEvery5Min, func(ctx context.Context) error {
			data, _, err := CollectLogTail(opts.LogPaths.LetsEncrypt, MaxLogLines)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "log_letsencrypt", data, "no_state", 0)
		}},
		{"monitor_log_messages", SpecEvery5Min, func(ctx context.Context) error {
			data, _, err := CollectLogTail(opts.LogPaths.Messages, MaxLogLines)
			if err != nil {
				return err
			}
			return Store(ctx, db, serverID, "log_messages", data, "no_state", 0)
		}},
	}

	if opts.EnableSystemUpdate {
		entries = append(entries, entry{
			name: "monitor_system_update",
			spec: SpecHourly,
			fn: func(ctx context.Context) error {
				data, state, err := CollectSystemUpdate(ctx)
				if err != nil {
					return err
				}
				return Store(ctx, db, serverID, "system_update", data, state, 0)
			},
		})
	}

	for _, e := range entries {
		if err := sched.Register(e.name, e.spec, e.fn); err != nil {
			return err
		}
	}
	log.Info("monitor jobs registered", "count", len(entries), "server_id", serverID)
	return nil
}
