package cmd

// daemonCmd runs the persistent config-apply daemon: the sys_datalog
// consumer with module/plugin dispatch, remote actions and delayed service
// restarts (openspec change port-ispconfig3-to-go, group 4).

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
)

var daemonCmd = &cobra.Command{
	Use:          "daemon",
	Short:        "Start the config-apply daemon (sys_datalog consumer)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := database.Open(cfg.Database.DSN)
		if err != nil {
			return err
		}

		reg := engine.NewRegistry(logger)
		// Real modules/plugins (nginx vhosts, Bind zones) register here as
		// their own openspec changes land; the engine runs with an empty
		// registry until then.
		if err := reg.Load(nil, nil); err != nil {
			return err
		}
		services := engine.NewServices(engine.SystemctlExecutor{}, logger)

		sched := engine.NewScheduler(db, logger)
		daemon, err := engine.NewDaemon(db, reg, services, sched, logger)
		if err != nil {
			return err
		}
		if err := sched.RegisterDatalogPruning(daemon.ServerID(), cfg.Daemon.DatalogRetentionDays); err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return daemon.Run(ctx, time.Duration(cfg.Daemon.TickSeconds)*time.Second)
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
