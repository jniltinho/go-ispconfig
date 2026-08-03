package cmd

// daemonCmd runs the persistent config-apply daemon: the sys_datalog
// consumer with module/plugin dispatch, remote actions and delayed service
// restarts (openspec change port-ispconfig3-to-go, group 4).

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"go-ispconfig/internal/clientdb"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/cron"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/dns"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/firewall"
	"go-ispconfig/internal/ftp"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mail"
	"go-ispconfig/internal/nginx"
	"go-ispconfig/internal/queue"
	"go-ispconfig/internal/shell"
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
		// The FTP and shell plugins ride along with nginx: all three consume
		// events of the web module, and an FTP or shell account only exists
		// inside a website.
		plugins := []engine.Plugin{
			nginxPlugin,
			ftp.NewPlugin(db, runner, logger),
			shell.NewPlugin(db, runner, logger),
		}
		var mailPlugin *mail.Plugin
		// Mail module: only on mail servers (mail-module-events spec:
		// server.mail_server = 1).
		if srv.MailServer == 1 && !cfg.Daemon.DisableMailModule {
			modules = append(modules, mail.NewModule())
			mailPlugin = mail.NewPlugin(db, services, runner, srv.ServerID, logger)
			plugins = append(plugins, mailPlugin,
				mail.NewMaildeliverPlugin(mailPlugin, cfg.Templates.CustomDir),
				mail.NewDkimPlugin(mailPlugin),
				mail.NewRspamdPlugin(mailPlugin, cfg.Templates.CustomDir))
			mail.RegisterServices(services)
		}
		var dnsPlugin *dns.Plugin
		if srv.DNSServer == 1 {
			dnsPlugin = dns.NewPlugin(db, services, runner, cfg.Templates.CustomDir, srv.ServerID, logger)
			modules = append(modules, dns.NewModule())
			plugins = append(plugins, dnsPlugin)
			dns.RegisterServices(services)
		}
		// Firewall module: only on firewall servers with the module
		// enabled in config.toml (firewall-module-events / design D3:
		// server.firewall_server = 1 and !disable_firewall_module).
		if srv.FirewallServer == 1 && !cfg.Daemon.DisableFirewallModule {
			fwPlugin := firewall.NewPlugin(runner, srv.ServerID, cfg.Server.Port, logger)
			fwPlugin.LoadSSHPort = func(ctx context.Context) int {
				return firewallSSHPort(ctx, db, srv.ServerID)
			}
			modules = append(modules, firewall.NewModule())
			plugins = append(plugins, fwPlugin)
		}
		// Database module: only on DB servers with the module enabled in
		// config.toml (database-module-events / design D15:
		// server.db_server = 1 and !disable_database_module).
		if srv.DBServer == 1 && !cfg.Daemon.DisableDatabaseModule {
			modules = append(modules, clientdb.NewModule())
			plugins = append(plugins, clientdb.NewPlugin(db, runner, cfg.Database.ClientDBConf, srv.ServerID, logger))
		}
		// Cron module + client-job plugin: only on web servers with the
		// module enabled in config.toml (cron-module-events / design D1:
		// server.web_server = 1 and !disable_cron_module).
		var cronPlugin *cron.Plugin
		if cron.Enabled(srv.WebServer, cfg.Daemon.DisableCronModule) {
			cronPlugin = cron.NewPlugin(db, srv.ServerID, cron.NewClientJobRunner(logger), logger)
			modules = append(modules, cron.NewModule())
			plugins = append(plugins, cronPlugin)
		}
		if err := reg.Load(modules, plugins); err != nil {
			return err
		}
		if cronPlugin != nil {
			// Legacy cutover (design D7): strip PHP ispc_* crontabs first so
			// jobs never run both under vixie-cron and in-process.
			crontabDir := cron.DefaultCrontabDir
			if sc, err := getconf.GetServerConfig(db, srv.ServerID); err == nil {
				crontabDir = cron.CrontabDir(sc)
			} else {
				logger.Warn("cron: could not load server config for crontab_dir", "error", err)
			}
			if removed, err := cron.RemoveLegacyCrontabs(crontabDir, logger); err != nil {
				logger.Error("cron: legacy cutover failed", "error", err, "dir", crontabDir)
			} else if len(removed) > 0 {
				logger.Info("cron: legacy cutover complete", "removed", len(removed), "dir", crontabDir)
			}
			if n, err := cron.LoadActiveJobs(context.Background(), db, srv.ServerID, cronPlugin.Runner(), cronPlugin.JobFactory()); err != nil {
				logger.Error("cron: loading active jobs failed", "error", err)
			} else {
				logger.Info("cron: active jobs loaded", "count", n)
			}
			cronPlugin.Runner().Start()
			defer cronPlugin.Runner().Stop()
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
		if mailPlugin != nil {
			if err := mailPlugin.RegisterPurgeJob(sched); err != nil {
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

// firewallSSHPort reads server.config [server] ssh_port for the lock-out
// guard (design D6). Missing/invalid values fall back to
// firewall.DefaultSSHPort (22).
func firewallSSHPort(_ context.Context, db *gorm.DB, serverID uint32) int {
	cfg, err := getconf.GetServerConfig(db, serverID)
	if err != nil {
		return firewall.DefaultSSHPort
	}
	raw := ""
	if sec := cfg.Raw["server"]; sec != nil {
		raw = sec["ssh_port"]
	}
	if raw == "" {
		return firewall.DefaultSSHPort
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 || n > 65535 {
		return firewall.DefaultSSHPort
	}
	return n
}
