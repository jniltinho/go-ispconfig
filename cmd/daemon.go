package cmd

// daemonCmd runs the persistent config-apply daemon: the sys_datalog
// consumer with module/plugin dispatch, remote actions and delayed service
// restarts (openspec change port-ispconfig3-to-go, group 4).

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/dns"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mail"
	"go-ispconfig/internal/nginx"
	"go-ispconfig/internal/queue"
	"go-ispconfig/internal/web"
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

		// Server row: the dns module/plugin only load on DNS servers
		// (dns-module-events spec: server.dns_server = 1).
		srv, err := engine.GuardServer(db)
		if err != nil {
			return err
		}

		// Services registry with the web-module guard: 'httpd' maps to the
		// nginx unit behind an nginx -t check, 'bind' resolves its systemd
		// unit (bind9/named) at runtime, php-fpm units pass through.
		runner := engine.ExecRunner{}
		services := engine.NewServices(web.GuardedExecutor{
			Inner:  &dns.BindExecutor{Inner: engine.SystemctlExecutor{}},
			Runner: runner,
		}, logger)

		reg := engine.NewRegistry(logger)
		nginxPlugin := nginx.NewPlugin(db, services, runner, cfg.Templates.CustomDir, logger)
		clientModule := clients.NewModule()
		clientModule.DisableHook = cfg.Daemon.DisableClientEvents
		// The client module loads regardless of server roles: client
		// datalog rows broadcast with server_id = 0 to every node.
		modules := []engine.Module{web.NewModule(), clientModule}
		plugins := []engine.Plugin{nginxPlugin}
		// Mail module: only on mail servers (mail-module-events spec:
		// server.mail_server = 1).
		if srv.MailServer == 1 && !cfg.Daemon.DisableMailModule {
			modules = append(modules, mail.NewModule())
			mailPlugin := mail.NewPlugin(db, services, runner, srv.ServerID, logger)
			plugins = append(plugins, mailPlugin,
				mail.NewMaildeliverPlugin(mailPlugin, cfg.Templates.CustomDir))
			mail.RegisterServices(services)
		}
		var dnsPlugin *dns.Plugin
		if srv.DNSServer == 1 {
			dnsPlugin = dns.NewPlugin(db, services, runner, cfg.Templates.CustomDir, srv.ServerID, logger)
			modules = append(modules, dns.NewModule())
			plugins = append(plugins, dnsPlugin)
			dns.RegisterServices(services)
		}
		if err := reg.Load(modules, plugins); err != nil {
			return err
		}

		daemon, err := engine.NewDaemon(db, reg, services, logger)
		if err != nil {
			return err
		}

		// Register the web services: httpd plus one php-fpm service per
		// known FPM unit (server default + active server_php rows).
		fpmUnits := []string{}
		if srvCfg, err := getconf.GetServerConfig(db, daemon.ServerID()); err == nil {
			fpmUnits = append(fpmUnits, srvCfg.Web.PHPFPMInitScript)
		} else {
			logger.Warn("daemon: could not load server web config", "error", err)
		}
		var phpUnits []string
		if err := db.Table("server_php").
			Where("server_id = ? AND active = 'y'", daemon.ServerID()).
			Pluck("php_fpm_init_script", &phpUnits).Error; err != nil {
			logger.Warn("daemon: could not load server_php units", "error", err)
		}
		web.RegisterServices(services, append(fpmUnits, phpUnits...)...)
		sched := engine.NewScheduler(db, logger)
		if err := sched.RegisterDatalogPruning(daemon.ServerID(), cfg.Daemon.DatalogRetentionDays); err != nil {
			return err
		}
		if err := nginxPlugin.RegisterRenewal(sched); err != nil {
			return err
		}
		if dnsPlugin != nil {
			if err := dnsPlugin.RegisterResign(sched); err != nil {
				return err
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Embedded asynq worker (design D12): scheduler jobs run as periodic
		// tasks and datalog:ready wakes trigger immediate processing. A Redis
		// outage never stops the daemon — the tick polling below is the
		// fallback source of truth consumer.
		worker := queue.NewWorker(cfg.Queue, daemon.ServerID(), logger)
		worker.HandleSchedulerJobs(sched.RunJob)
		worker.Handle(queue.TypeDatalogReady, func(ctx context.Context, _ []byte) error {
			return daemon.Wake(ctx)
		})
		for _, j := range sched.Jobs(ctx) {
			worker.RegisterPeriodic(j.Spec, j.Name)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := worker.Run(ctx); err != nil {
				logger.Error("queue worker failed, continuing on tick polling only", "error", err)
			}
		}()
		defer wg.Wait()

		return daemon.Run(ctx, time.Duration(cfg.Daemon.TickSeconds)*time.Second)
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
