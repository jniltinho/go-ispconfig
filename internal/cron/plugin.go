package cron

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// Plugin bridges cron_* events to the ClientJobRunner (design D1/D2):
// register / replace / remove in-process jobs. It never writes under
// crontab_dir (design D10).
type Plugin struct {
	db       *gorm.DB
	serverID uint32
	log      *slog.Logger
	runner   *ClientJobRunner
	urlExec  *URLExecutor
	proc     *ProcessRunner

	// LoadParent resolves the parent web_domain (+ php_cli_binary). Nil
	// uses the default DB join. Tests inject fixed sites.
	LoadParent func(ctx context.Context, parentDomainID uint32) (SiteContext, bool, error)
	// LookupUser / LookupGroup feed privilege drop (nil → os/user).
	LookupUser  UserLookup
	LookupGroup GroupLookup
}

// NewPlugin creates the cron plugin for one server. runner must be non-nil.
func NewPlugin(db *gorm.DB, serverID uint32, runner *ClientJobRunner, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	if runner == nil {
		runner = NewClientJobRunner(log)
	}
	return &Plugin{
		db:       db,
		serverID: serverID,
		log:      log,
		runner:   runner,
		urlExec:  &URLExecutor{},
		proc: &ProcessRunner{
			Configure: ConfigureWithLookups(nil, nil),
		},
	}
}

// Name identifies the plugin in logs and the registry.
func (*Plugin) Name() string { return "cron" }

// Runner exposes the client-job runner (daemon start load / tests).
func (p *Plugin) Runner() *ClientJobRunner { return p.runner }

// OnLoad subscribes to cron_insert/update/delete.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	// Wire privilege drop with plugin lookups (may be set after NewPlugin).
	p.proc.Configure = ConfigureWithLookups(p.LookupUser, p.LookupGroup)

	for _, event := range []string{"cron_insert", "cron_update", "cron_delete"} {
		ev := event
		if err := r.RegisterEvent(ev, func(ctx context.Context, _ string, data engine.Data) error {
			return p.handle(ctx, ev, data)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) handle(ctx context.Context, event string, data engine.Data) error {
	switch event {
	case "cron_delete":
		return p.onDelete(ctx, data)
	default:
		return p.onUpsert(ctx, data)
	}
}

func (p *Plugin) onDelete(_ context.Context, data engine.Data) error {
	old := row(data.Old)
	id := uint32(old.num("id"))
	if id == 0 {
		return nil
	}
	if sid := uint32(old.num("server_id")); sid != 0 && sid != p.serverID {
		return nil
	}
	p.runner.Remove(id)
	return nil
}

func (p *Plugin) onUpsert(ctx context.Context, data engine.Data) error {
	newRow := row(data.New)
	id := uint32(newRow.num("id"))
	if id == 0 {
		p.log.Warn("cron: upsert missing id")
		return nil
	}
	if sid := uint32(newRow.num("server_id")); sid != 0 && sid != p.serverID {
		return nil
	}

	// Inactive → ensure absent (spec: inactive job is not scheduled).
	if newRow.str("active") != "y" {
		p.runner.Remove(id)
		return nil
	}

	parentID := uint32(newRow.num("parent_domain_id"))
	if parentID == 0 {
		p.log.Warn("cron: parent domain not set", "id", id)
		p.runner.Remove(id)
		return nil
	}

	site, ok, err := p.loadParent(ctx, parentID)
	if err != nil {
		return fmt.Errorf("cron: loading parent domain %d: %w", parentID, err)
	}
	if !ok {
		p.log.Warn("cron: parent domain not found", "id", id, "parent_domain_id", parentID)
		p.runner.Remove(id)
		return nil
	}
	if hasDisallowedOwner(site) {
		p.log.Warn("cron: websites (and crons) cannot be owned by the root user or group",
			"id", id, "system_user", site.SystemUser, "system_group", site.SystemGroup)
		p.runner.Remove(id)
		return nil
	}

	jobType := newRow.str("type")
	if jobType == "" {
		jobType = model.CronTypeURL
	}
	command := newRow.str("command")
	logFlag := newRow.str("log")
	if logFlag == "" {
		logFlag = "n"
	}

	job := Job{
		ID:       id,
		RunMin:   newRow.str("run_min"),
		RunHour:  newRow.str("run_hour"),
		RunMday:  newRow.str("run_mday"),
		RunMonth: newRow.str("run_month"),
		RunWday:  newRow.str("run_wday"),
		Run: p.makeRun(model.Cron{
			ID:             id,
			ServerID:       p.serverID,
			ParentDomainID: parentID,
			Type:           jobType,
			Command:        command,
			Log:            logFlag,
			Active:         "y",
		}, site),
	}
	if err := p.runner.Add(job); err != nil {
		p.log.Error("cron: registering job failed", "id", id, "error", err)
		return nil // do not fail the datalog cycle on a single bad schedule
	}
	return nil
}

func (p *Plugin) makeRun(job model.Cron, site SiteContext) JobFunc {
	// Capture site snapshot for datalog-driven registration; daemon-start
	// load uses makeRunWithParentLookup to re-resolve at fire time.
	siteSnap := site
	return func(ctx context.Context) {
		res, security := p.execute(ctx, job, siteSnap)
		if err := WriteRunLog(ctx, p.db, RunLogInput{
			ServerID:       p.serverID,
			CronID:         job.ID,
			ParentDomainID: job.ParentDomainID,
			Type:           job.Type,
			Log:            job.Log,
			Result:         res,
			Security:       security,
		}); err != nil {
			p.log.Error("cron: writing run log failed", "id", job.ID, "error", err)
		}
	}
}

func (p *Plugin) execute(ctx context.Context, job model.Cron, site SiteContext) (RunResult, bool) {
	switch job.Type {
	case model.CronTypeURL:
		return p.urlExec.Execute(ctx, job.Command, site.Domain), false
	case model.CronTypeFull, model.CronTypeChrooted:
		// Pre-check credentials fail-closed (design D5).
		if _, err := ResolveSiteCredentials(site, p.LookupUser, p.LookupGroup); err != nil {
			res := RunResult{
				Status:   StatusError,
				ExitCode: -1,
				Output:   err.Error(),
				Err:      err,
			}
			return res, true
		}
		spec, err := BuildExecSpec(job.Command, job.Type, site)
		if err != nil {
			return RunResult{Status: StatusError, ExitCode: -1, Output: err.Error(), Err: err}, strings.Contains(err.Error(), "insecure")
		}
		return p.proc.Run(ctx, spec, site), false
	default:
		err := fmt.Errorf("unknown cron type %q", job.Type)
		return RunResult{Status: StatusError, ExitCode: -1, Output: err.Error(), Err: err}, false
	}
}

func (p *Plugin) loadParent(ctx context.Context, parentDomainID uint32) (SiteContext, bool, error) {
	if p.LoadParent != nil {
		return p.LoadParent(ctx, parentDomainID)
	}
	return loadParentDomain(ctx, p.db, parentDomainID)
}

// loadParentDomain joins web_domain to server_php for php_cli_binary.
func loadParentDomain(ctx context.Context, db *gorm.DB, parentDomainID uint32) (SiteContext, bool, error) {
	if db == nil {
		return SiteContext{}, false, fmt.Errorf("cron: no database for parent lookup")
	}
	type parentRow struct {
		DomainID     uint32 `gorm:"column:domain_id"`
		Domain       string `gorm:"column:domain"`
		DocumentRoot string `gorm:"column:document_root"`
		SystemUser   string `gorm:"column:system_user"`
		SystemGroup  string `gorm:"column:system_group"`
		PHPCLIBinary string `gorm:"column:php_cli_binary"`
	}
	var pr parentRow
	err := db.WithContext(ctx).Raw(`
SELECT w.domain_id, w.domain, w.document_root, w.system_user, w.system_group,
       COALESCE(p.php_cli_binary, '') AS php_cli_binary
FROM web_domain w
LEFT JOIN server_php p ON w.server_php_id = p.server_php_id
WHERE w.domain_id = ?
LIMIT 1`, parentDomainID).Scan(&pr).Error
	if err != nil {
		return SiteContext{}, false, err
	}
	if pr.DomainID == 0 {
		return SiteContext{}, false, nil
	}
	return SiteContext{
		Domain:       pr.Domain,
		DocumentRoot: pr.DocumentRoot,
		SystemUser:   pr.SystemUser,
		SystemGroup:  pr.SystemGroup,
		PHPCLIBinary: pr.PHPCLIBinary,
	}, true, nil
}

// Site owners must look like web<N> / client<N>, mirroring PHP
// is_allowed_user / is_allowed_group with $restrict_names = true
// (server/lib/classes/system.inc.php). The regexes subsume the PHP name
// blacklist (root, ispconfig, vmail, getmail) and charset check; the uid/gid
// floor stays in ResolveSiteCredentials.
var (
	siteUserRE  = regexp.MustCompile(`^web\d+$`)
	siteGroupRE = regexp.MustCompile(`^client\d+$`)
)

// hasDisallowedOwner reports whether the parent site's system_user /
// system_group may not run cron jobs (PHP cron_plugin parity).
func hasDisallowedOwner(site SiteContext) bool {
	return !siteUserRE.MatchString(strings.TrimSpace(site.SystemUser)) ||
		!siteGroupRE.MatchString(strings.TrimSpace(site.SystemGroup))
}

// JobFactory builds a JobFactory that uses this plugin's executors
// (daemon-start LoadActiveJobs).
func (p *Plugin) JobFactory() JobFactory {
	return func(job model.Cron) JobFunc {
		return p.makeRunWithParentLookup(job)
	}
}

// makeRunWithParentLookup returns a JobFunc that re-resolves the parent site
// at execution time (self-healing if the site PHP binary changed).
func (p *Plugin) makeRunWithParentLookup(job model.Cron) JobFunc {
	return func(ctx context.Context) {
		site, ok, err := p.loadParent(ctx, job.ParentDomainID)
		if err != nil || !ok {
			p.log.Warn("cron: active job missing parent at run time", "id", job.ID, "error", err)
			return
		}
		// Jobs loaded at daemon start never pass the event gate, so re-check
		// the owner here too.
		if hasDisallowedOwner(site) {
			p.log.Warn("cron: websites (and crons) cannot be owned by the root user or group",
				"id", job.ID, "system_user", site.SystemUser, "system_group", site.SystemGroup)
			return
		}
		res, security := p.execute(ctx, job, site)
		if err := WriteRunLog(ctx, p.db, RunLogInput{
			ServerID:       p.serverID,
			CronID:         job.ID,
			ParentDomainID: job.ParentDomainID,
			Type:           job.Type,
			Log:            job.Log,
			Result:         res,
			Security:       security,
		}); err != nil {
			p.log.Error("cron: writing run log failed", "id", job.ID, "error", err)
		}
	}
}
