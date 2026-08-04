package api

// Health endpoints: /healthz for load balancer liveness (no dependency is
// touched — it only proves the HTTP server answers) and /api/health for the
// operator view, which probes database, task queue, TLS certificate and
// daemon backlog when called with ?full=1.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/monitor"
)

const (
	// probeTimeout bounds every dependency probe: a health check that
	// hangs is worse than one that reports a failure.
	probeTimeout = 2 * time.Second
	// certExpiryWarning is how long before expiry the TLS certificate
	// check starts failing — enough runway to renew by hand.
	certExpiryWarning = 30 * 24 * time.Hour
	// daemonBacklogGrace is how long a pending sys_datalog row may sit
	// unprocessed before the daemon counts as behind. Below this every
	// normal write would flap the check while the daemon picks it up.
	daemonBacklogGrace = 60 * time.Second
)

// Health configures the health endpoints: what to probe and what to report.
type Health struct {
	// DB backs the database check; its failure is the only one that
	// turns the endpoint into a 503.
	DB *gorm.DB
	// Server is the local server row, used for the daemon backlog check.
	// nil (unresolved server_id) skips that check.
	Server *model.Server
	// PingQueue probes Redis/Valkey; nil skips the queue check.
	PingQueue func() error
	// TLSCert is the serving certificate path; empty skips the check.
	TLSCert string

	Version   string
	BuildDate string
	GitCommit string
}

// CheckResult is one dependency probe outcome.
type CheckResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// HealthStatus is the GET /api/health payload. Checks is only populated
// with ?full=1.
type HealthStatus struct {
	// Status is ok, degraded (a non-critical check failed) or fail (the
	// database is unreachable).
	Status     string                 `json:"status"`
	Version    string                 `json:"version"`
	BuildDate  string                 `json:"build_date,omitempty"`
	GitCommit  string                 `json:"git_commit,omitempty"`
	UptimeSec  int64                  `json:"uptime_sec"`
	ServerTime time.Time              `json:"server_time"`
	Checks     map[string]CheckResult `json:"checks,omitempty"`
}

// RegisterHealth mounts GET /api/health and GET /healthz. Both are
// unauthenticated: they expose no configuration, only liveness and the
// build identity already visible in the login page footer.
func RegisterHealth(e *echo.Echo, h Health) {
	e.GET("/api/health", healthHandler(h, time.Now()))

	// Conventional load balancer / kubernetes liveness probe: plain text,
	// no dependency touched — if the process can answer, it is alive.
	e.GET("/healthz", func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok\n")
	})
}

// healthHandler implements GET /api/health, started being the process start.
//
//	@Summary		Health check
//	@Description	Status, build identity and uptime. With ?full=1 it also probes the database, the task queue, the TLS certificate expiry and the daemon datalog backlog; the response is 503 only when the database is unreachable, a failing queue/certificate/daemon reports "degraded" with 200. No authentication required.
//	@Tags			meta
//	@Produce		json
//	@Param			full	query		int	false	"1 to run the dependency probes"
//	@Success		200		{object}	HealthStatus
//	@Failure		503		{object}	HealthStatus	"Database unreachable"
//	@Router			/health [get]
func healthHandler(h Health, started time.Time) echo.HandlerFunc {
	return func(c *echo.Context) error {
		st := HealthStatus{
			Status:     "ok",
			Version:    h.Version,
			BuildDate:  h.BuildDate,
			GitCommit:  h.GitCommit,
			UptimeSec:  int64(time.Since(started).Seconds()),
			ServerTime: time.Now(),
		}
		if c.QueryParam("full") != "1" {
			return c.JSON(http.StatusOK, st)
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), probeTimeout)
		defer cancel()
		st.Checks = h.probe(ctx)

		code := http.StatusOK
		for name, r := range st.Checks {
			if r.OK {
				continue
			}
			if name == "database" {
				st.Status, code = "fail", http.StatusServiceUnavailable
				break
			}
			st.Status = "degraded"
		}
		return c.JSON(code, st)
	}
}

// probe runs every configured dependency check.
func (h Health) probe(ctx context.Context) map[string]CheckResult {
	checks := map[string]CheckResult{}
	if h.DB != nil {
		checks["database"] = timed(func() error {
			sqlDB, err := h.DB.DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		})
		if h.Server != nil && checks["database"].OK {
			checks["daemon"] = h.checkDaemon(ctx)
		}
	}
	if h.PingQueue != nil {
		checks["queue"] = timed(h.PingQueue)
	}
	if h.TLSCert != "" {
		checks["tls_cert"] = checkTLSCert(h.TLSCert)
	}
	return checks
}

// checkDaemon reports the local server's unprocessed sys_datalog backlog.
// A backlog on its own is normal (the row was just written); one whose
// oldest entry predates daemonBacklogGrace means the daemon is not draining.
func (h Health) checkDaemon(ctx context.Context) CheckResult {
	start := time.Now()
	oldest, pending, err := monitor.ListJobqueue(ctx, h.DB, monitor.JobqueueFilter{
		Servers: []model.Server{*h.Server},
		Limit:   1,
	})
	r := CheckResult{OK: true, LatencyMS: time.Since(start).Milliseconds()}
	switch {
	case err != nil:
		r.OK, r.Error = false, err.Error()
	case pending == 0:
		r.Detail = "no pending datalog rows"
	default:
		age := time.Since(time.Unix(int64(oldest[0].Tstamp), 0))
		r.OK = age < daemonBacklogGrace
		r.Detail = fmt.Sprintf("%d pending datalog rows, oldest %ds", pending, int64(age.Seconds()))
		if !r.OK {
			r.Error = "daemon is not draining the datalog queue"
		}
	}
	return r
}

// checkTLSCert parses the serving certificate and fails once it is inside
// certExpiryWarning of its expiry — a silently expired certificate is the
// classic 3am page.
func checkTLSCert(path string) CheckResult {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Error: err.Error()}
	}
	for len(raw) > 0 {
		var block *pem.Block
		if block, raw = pem.Decode(raw); block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return CheckResult{Error: err.Error()}
		}
		left := time.Until(cert.NotAfter)
		r := CheckResult{
			OK:     left > certExpiryWarning,
			Detail: fmt.Sprintf("expires %s (%dd)", cert.NotAfter.Format(time.RFC3339), int64(left.Hours()/24)),
		}
		if !r.OK {
			r.Error = "certificate expires within 30 days"
		}
		return r
	}
	return CheckResult{Error: "no CERTIFICATE block in " + path}
}

// timed runs probe and reports its outcome with the measured latency.
func timed(probe func() error) CheckResult {
	start := time.Now()
	err := probe()
	r := CheckResult{OK: err == nil, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}
