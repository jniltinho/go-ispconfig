package api

// Legacy ISPConfig3 migration wizard endpoints (/api/system/migration/*,
// change add-legacy-migration D7): admin-only frontend of the shared
// import engine. Legacy credentials live only in this process's memory
// (inside the connected legacy client) — never in the database, config
// files, logs or responses.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	legacyclient "go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/model"
)

// MigrationConnectRequest is the connect/test payload. The password is
// used for the legacy login only and never stored or echoed.
type MigrationConnectRequest struct {
	// URL is the legacy panel base URL.
	URL string `json:"url" example:"https://legacy.example.com:8080"`
	// Username is the legacy remote_user name.
	Username string `json:"username" example:"migrator"`
	// Password is the legacy remote_user password.
	Password string `json:"password" example:""`
	// Insecure disables TLS certificate verification (echoed as a warning).
	Insecure bool `json:"insecure"`
}

// MigrationSelection selects the entity subset; all false means all.
type MigrationSelection struct {
	// Clients selects client import.
	Clients bool `json:"clients"`
	// Sites selects web domain/folder/folder user import.
	Sites bool `json:"sites"`
	// DNS selects DNS zone/record/slave/template import.
	DNS bool `json:"dns"`
}

// selection converts the request subset to the importer type.
func (s MigrationSelection) selection() importer.Selection {
	if !s.Clients && !s.Sites && !s.DNS {
		return importer.Selection{Clients: true, Sites: true, DNS: true}
	}
	return importer.Selection{Clients: s.Clients, Sites: s.Sites, DNS: s.DNS}
}

// MigrationRunRequest is the dry-run/execute payload.
type MigrationRunRequest struct {
	// Selection is the entity subset (empty = everything).
	Selection MigrationSelection `json:"selection"`
	// TargetServerID maps every legacy server_id; 0 uses the first local
	// server.
	TargetServerID uint32 `json:"target_server_id"`
	// AssignOrphanZonesToAdmin assigns zones with absent owners to admin.
	AssignOrphanZonesToAdmin bool `json:"assign_orphan_zones_to_admin"`
	// ConfirmMapAllToLocalServer explicitly confirms mapping a
	// multi-server legacy panel onto the single local server (execute
	// only).
	ConfirmMapAllToLocalServer bool `json:"confirm_map_all_to_local_server"`
}

// MigrationError is the wizard-specific failure payload: it names the
// legacy fault or the exact missing remote functions so the operator can
// act on it.
type MigrationError struct {
	// Error is the human-readable failure.
	Error string `json:"error"`
	// FaultCode is the legacy fault code, when the legacy panel answered
	// with a fault.
	FaultCode string `json:"fault_code,omitempty"`
	// MissingFunctions lists remote functions the remote_user lacks.
	MissingFunctions []string `json:"missing_functions,omitempty"`
}

// MigrationConnectResponse reports a successful connection test.
type MigrationConnectResponse struct {
	// Servers is the legacy server list.
	Servers []legacyclient.Record `json:"servers"`
	// MultiServer flags more than one legacy server (execute will require
	// explicit confirmation).
	MultiServer bool `json:"multi_server"`
	// Insecure echoes that TLS verification is disabled.
	Insecure bool `json:"insecure"`
	// PlainHTTP echoes an unencrypted panel URL.
	PlainHTTP bool `json:"plain_http"`
}

// MigrationPlanResponse is the dry-run result.
type MigrationPlanResponse struct {
	// Counts is the per-table create/update/skip/conflict tally.
	Counts map[string]importer.EntityCount `json:"counts"`
	// Conflicts lists every conflicting record with its reason.
	Conflicts []importer.Item `json:"conflicts"`
	// Warnings collects non-blocking notes.
	Warnings []string `json:"warnings"`
	// ResetRequired lists panel users that will need a password reset.
	ResetRequired []string `json:"reset_required"`
}

// MigrationStatus is the run snapshot (GET status and the SSE stream's
// final event). State: idle → connected → running → done|failed.
type MigrationStatus struct {
	// State is the wizard state machine position.
	State string `json:"state" example:"running"`
	// LegacyURL is the connected panel URL (no credentials).
	LegacyURL string `json:"legacy_url,omitempty"`
	// Insecure echoes disabled TLS verification.
	Insecure bool `json:"insecure,omitempty"`
	// PlainHTTP echoes an unencrypted panel URL.
	PlainHTTP bool `json:"plain_http,omitempty"`
	// MultiServer flags a multi-server legacy panel.
	MultiServer bool `json:"multi_server,omitempty"`
	// Inventory is the last fetched inventory.
	Inventory *importer.Inventory `json:"inventory,omitempty"`
	// Progress is the per-entity apply progress.
	Progress map[string]importer.Progress `json:"progress,omitempty"`
	// Error is the failure message of a failed run.
	Error string `json:"error,omitempty"`
	// Report is the final report of a finished run.
	Report *importer.Report `json:"report,omitempty"`
}

// migrationManager is the single-run wizard state (design D7): one
// in-process lock, the connected legacy client (credentials only in
// memory), and the progress fan-out to SSE subscribers.
// ponytail: single in-process run lock; a job queue only if concurrent
// migrations are ever needed.
type migrationManager struct {
	mu        sync.Mutex
	client    *legacyclient.Client
	legacyURL string
	status    MigrationStatus
	running   bool
	subs      map[chan []byte]struct{}
}

// newMigrationManager returns an idle manager.
func newMigrationManager() *migrationManager {
	return &migrationManager{
		status: MigrationStatus{State: "idle"},
		subs:   map[chan []byte]struct{}{},
	}
}

// publishLocked fans one SSE frame ("event: <name>" + data) to every
// subscriber without blocking (slow subscribers drop events; the status
// snapshot recovers). The caller must hold m.mu — it protects m.subs.
func (m *migrationManager) publishLocked(name string, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	frame := []byte("event: " + name + "\ndata: " + string(payload) + "\n\n")
	for ch := range m.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

// subscribe registers an SSE listener; the returned cancel removes it.
func (m *migrationManager) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

// registerMigrationRoutes mounts the admin-only wizard endpoints.
func registerMigrationRoutes(g *echo.Group, d *Deps) {
	m := newMigrationManager()
	g.POST("/system/migration/connect", migrationConnectHandler(m), requireAdmin)
	g.GET("/system/migration/inventory", migrationInventoryHandler(m), requireAdmin)
	g.POST("/system/migration/dry-run", migrationDryRunHandler(m, d), requireAdmin)
	g.POST("/system/migration/execute", migrationExecuteHandler(m, d), requireAdmin)
	g.POST("/system/migration/reset-passwords", migrationResetHandler(m, d), requireAdmin)
	g.GET("/system/migration/status", migrationStatusHandler(m), requireAdmin)
	g.GET("/system/migration/progress", migrationProgressHandler(m), requireAdmin)
}

// migrationFailure converts a legacy-side error into the actionable
// wizard payload (fault code or exact missing function names).
func migrationFailure(err error) MigrationError {
	out := MigrationError{Error: err.Error()}
	var missErr *legacyclient.MissingGrantsError
	if errors.As(err, &missErr) {
		out.MissingFunctions = missErr.Missing
	}
	if fault, ok := legacyclient.IsFault(err); ok {
		out.FaultCode = fault.Code
	}
	return out
}

// migrationConnectHandler implements POST /api/system/migration/connect.
//
//	@Summary		Test the legacy panel connection
//	@Description	Logs into the legacy ISPConfig3 remote JSON API with the given remote_user, verifies every required *_get grant and returns the legacy server list. Credentials are held only in server memory for the wizard session — never stored, logged or echoed. Admin only.
//	@Tags			migration
//	@Accept			json
//	@Produce		json
//	@Param			request	body		MigrationConnectRequest	true	"Legacy panel coordinates"
//	@Success		200		{object}	MigrationConnectResponse
//	@Failure		400		{object}	MigrationError	"Login fault, missing grants or TLS/certificate error"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Failure		409		{object}	MigrationError	"A migration run is active"
//	@Router			/system/migration/connect [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationConnectHandler(m *migrationManager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var req MigrationConnectRequest
		if err := c.Bind(&req); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.running {
			return c.JSON(http.StatusConflict, MigrationError{Error: "a migration run is already active"})
		}

		lc, err := legacyclient.New(legacyclient.Options{
			URL: req.URL, Username: req.Username, Password: req.Password, Insecure: req.Insecure,
		})
		if err != nil {
			return c.JSON(http.StatusBadRequest, MigrationError{Error: err.Error()})
		}
		ctx := c.Request().Context()
		if err := lc.Login(ctx); err != nil {
			return c.JSON(http.StatusBadRequest, migrationFailure(err))
		}
		if err := lc.Preflight(ctx); err != nil {
			_ = lc.Close()
			return c.JSON(http.StatusBadRequest, migrationFailure(err))
		}
		servers, err := lc.ServerGetAll(ctx)
		if err != nil {
			_ = lc.Close()
			return c.JSON(http.StatusBadRequest, migrationFailure(err))
		}

		if m.client != nil {
			_ = m.client.Close() // drop a previous wizard session
		}
		m.client = lc
		m.legacyURL = req.URL
		m.status = MigrationStatus{
			State:       "connected",
			LegacyURL:   req.URL,
			Insecure:    lc.Insecure(),
			PlainHTTP:   lc.PlainHTTP(),
			MultiServer: len(servers) > 1,
		}
		return c.JSON(http.StatusOK, MigrationConnectResponse{
			Servers:     servers,
			MultiServer: len(servers) > 1,
			Insecure:    lc.Insecure(),
			PlainHTTP:   lc.PlainHTTP(),
		})
	}
}

// begin snapshots the manager state for a read stage: it rejects an
// active run or a missing connection with a 409 and otherwise returns
// the connected client. It takes and releases the lock itself so slow
// legacy fetches never block status/SSE handlers.
func (m *migrationManager) begin(c *echo.Context) (*legacyclient.Client, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil, false, c.JSON(http.StatusConflict, MigrationError{Error: "a migration run is already active"})
	}
	if m.client == nil {
		return nil, false, c.JSON(http.StatusConflict, MigrationError{Error: "not connected: run the connection test first"})
	}
	return m.client, true, nil
}

// migrationInventoryHandler implements GET /api/system/migration/inventory.
//
//	@Summary		Legacy panel inventory
//	@Description	Fetches everything the import needs from the legacy panel (read-only) and returns the per-entity counts, the legacy server list and the multi-server guard flag. Admin only.
//	@Tags			migration
//	@Produce		json
//	@Success		200	{object}	importer.Inventory
//	@Failure		400	{object}	MigrationError	"Legacy fetch failed"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin"
//	@Failure		409	{object}	MigrationError	"Not connected or a run is active"
//	@Router			/system/migration/inventory [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationInventoryHandler(m *migrationManager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		lc, ok, err := m.begin(c)
		if !ok {
			return err
		}
		// Fetched without holding the lock: status/SSE stay responsive.
		snap, err := importer.FetchSnapshot(c.Request().Context(), lc,
			importer.Selection{Clients: true, Sites: true, DNS: true})
		if err != nil {
			return c.JSON(http.StatusBadRequest, migrationFailure(err))
		}
		inv := snap.Inventory()
		m.mu.Lock()
		m.status.Inventory = inv
		m.status.MultiServer = inv.MultiServer
		m.mu.Unlock()
		return c.JSON(http.StatusOK, inv)
	}
}

// targetServer resolves and validates the run's local server id.
func targetServer(ctx context.Context, db *gorm.DB, requested uint32) (uint32, error) {
	var server model.Server
	if requested != 0 {
		err := db.WithContext(ctx).Where("server_id = ?", requested).First(&server).Error
		if err != nil {
			return 0, fmt.Errorf("target server %d does not exist locally: %w", requested, err)
		}
		return server.ServerID, nil
	}
	if err := db.WithContext(ctx).Order("server_id").First(&server).Error; err != nil {
		return 0, fmt.Errorf("no local server row found: %w", err)
	}
	return server.ServerID, nil
}

// migrationDryRunHandler implements POST /api/system/migration/dry-run.
//
//	@Summary		Build the migration plan (dry-run)
//	@Description	Classifies every legacy record as create/update/skip-identical/conflict against the local database, without writing anything. Returns per-entity counts, the full conflict list with reasons and the password-reset user list. Admin only.
//	@Tags			migration
//	@Accept			json
//	@Produce		json
//	@Param			request	body		MigrationRunRequest	true	"Selection and target server"
//	@Success		200		{object}	MigrationPlanResponse
//	@Failure		400		{object}	MigrationError	"Legacy fetch or planning failed"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Failure		409		{object}	MigrationError	"Not connected or a run is active"
//	@Router			/system/migration/dry-run [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationDryRunHandler(m *migrationManager, d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var req MigrationRunRequest
		if err := c.Bind(&req); err != nil {
			return err
		}
		lc, ok, err := m.begin(c)
		if !ok {
			return err
		}
		ctx := c.Request().Context()
		sel := req.Selection.selection()
		// Fetched and planned without holding the lock: status/SSE stay
		// responsive during the (potentially long) legacy reads.
		snap, err := importer.FetchSnapshot(ctx, lc, sel)
		if err != nil {
			return c.JSON(http.StatusBadRequest, migrationFailure(err))
		}
		target, err := targetServer(ctx, d.DB, req.TargetServerID)
		if err != nil {
			return c.JSON(http.StatusBadRequest, MigrationError{Error: err.Error()})
		}
		plan, err := importer.BuildPlan(ctx, d.DB, snap, importer.Options{
			Selection:                sel,
			TargetServerID:           target,
			AssignOrphanZonesToAdmin: req.AssignOrphanZonesToAdmin,
		})
		if err != nil {
			return c.JSON(http.StatusBadRequest, MigrationError{Error: err.Error()})
		}
		conflicts := plan.Conflicts()
		if conflicts == nil {
			conflicts = []importer.Item{}
		}
		return c.JSON(http.StatusOK, MigrationPlanResponse{
			Counts:        plan.Counts(),
			Conflicts:     conflicts,
			Warnings:      plan.Warnings,
			ResetRequired: plan.ResetRequired(),
		})
	}
}

// migrationExecuteHandler implements POST /api/system/migration/execute.
//
//	@Summary		Execute the migration
//	@Description	Re-fetches the legacy snapshot, rebuilds the plan and applies it in a background goroutine (progress via SSE/status; the run survives page reloads). A multi-server legacy panel is rejected unless confirm_map_all_to_local_server is set. One run at a time. Admin only.
//	@Tags			migration
//	@Accept			json
//	@Produce		json
//	@Param			request	body		MigrationRunRequest	true	"Selection, target server and multi-server confirmation"
//	@Success		202		{object}	MigrationStatus		"Run started"
//	@Failure		400		{object}	MigrationError		"Multi-server without confirmation, or planning failed"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse	"Not an admin"
//	@Failure		409		{object}	MigrationError	"Not connected or a run is already active"
//	@Router			/system/migration/execute [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationExecuteHandler(m *migrationManager, d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var req MigrationRunRequest
		if err := c.Bind(&req); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.running {
			return c.JSON(http.StatusConflict, MigrationError{Error: "a migration run is already active"})
		}
		lc := m.client
		if lc == nil {
			return c.JSON(http.StatusConflict, MigrationError{Error: "not connected: run the connection test first"})
		}
		if m.status.MultiServer && !req.ConfirmMapAllToLocalServer {
			return c.JSON(http.StatusBadRequest, MigrationError{
				Error: "the legacy panel reports multiple servers; multi-server topologies are not supported — set confirm_map_all_to_local_server to map everything onto the single local server",
			})
		}
		target, err := targetServer(c.Request().Context(), d.DB, req.TargetServerID)
		if err != nil {
			return c.JSON(http.StatusBadRequest, MigrationError{Error: err.Error()})
		}

		m.running = true
		m.status.State = "running"
		m.status.Progress = map[string]importer.Progress{}
		m.status.Error = ""
		m.status.Report = nil

		// The run must survive the request (page reloads reattach via
		// status/SSE), so it uses its own context, not the request's.
		go m.run(context.Background(), d.DB, lc, req.Selection.selection(), target, req.AssignOrphanZonesToAdmin)
		return c.JSON(http.StatusAccepted, m.status)
	}
}

// runTimeout bounds a background run so a wedged legacy panel (TCP
// half-open) can never leave the wizard locked until a process restart.
const runTimeout = 4 * time.Hour

// run executes fetch → plan → apply → report in the background.
func (m *migrationManager) run(ctx context.Context, db *gorm.DB, lc *legacyclient.Client,
	sel importer.Selection, target uint32, orphansToAdmin bool) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	finished := false
	finish := func(errRun error, report *importer.Report) {
		finished = true
		// The wizard is done with the legacy panel either way: end the
		// remote session so credentials stop being live. A new stage
		// requires a fresh connect.
		_ = lc.Close()
		m.mu.Lock()
		m.running = false
		m.client = nil
		if errRun != nil {
			m.status.State = "failed"
			m.status.Error = errRun.Error()
		} else {
			m.status.State = "done"
			m.status.Report = report
		}
		m.publishLocked("status", m.status)
		m.mu.Unlock()
	}
	// A panic in the run must fail the run, never the API process.
	defer func() {
		if r := recover(); r != nil && !finished {
			finish(fmt.Errorf("panic during migration run: %v", r), nil)
		}
	}()

	snap, err := importer.FetchSnapshot(ctx, lc, sel)
	if err != nil {
		finish(err, nil)
		return
	}
	plan, err := importer.BuildPlan(ctx, db, snap, importer.Options{
		Selection: sel, TargetServerID: target, AssignOrphanZonesToAdmin: orphansToAdmin,
	})
	if err != nil {
		finish(err, nil)
		return
	}
	counts, err := importer.Apply(ctx, db, plan, func(p importer.Progress) {
		m.mu.Lock()
		m.status.Progress[p.Entity] = p
		m.publishLocked("progress", p)
		m.mu.Unlock()
	})
	if err != nil {
		finish(err, nil)
		return
	}

	m.mu.Lock()
	input := importer.ReportInput{
		LegacyHost:  importer.LegacyHost(m.legacyURL),
		Insecure:    m.status.Insecure,
		PlainHTTP:   m.status.PlainHTTP,
		MultiServer: m.status.MultiServer,
	}
	m.mu.Unlock()
	finish(nil, importer.BuildReport(plan, counts, input))
}

// migrationResetHandler implements POST /api/system/migration/reset-passwords.
//
//	@Summary		Bulk one-time password-reset tokens
//	@Description	Generates one one-time reset token per panel user recreated by the finished run (their imported password is an unusable placeholder). The cleartext tokens appear only in this response; the database stores digests. Admin only.
//	@Tags			migration
//	@Produce		json
//	@Success		200	{array}		importer.ResetToken
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin"
//	@Failure		409	{object}	MigrationError	"No finished run with reset-required users"
//	@Router			/system/migration/reset-passwords [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationResetHandler(m *migrationManager, d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.status.Report == nil || len(m.status.Report.ResetRequired) == 0 {
			return c.JSON(http.StatusConflict, MigrationError{Error: "no finished run with users requiring a password reset"})
		}
		tokens, err := importer.GenerateResetTokens(c.Request().Context(), d.DB, m.status.Report.ResetRequired)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, MigrationError{Error: err.Error()})
		}
		return c.JSON(http.StatusOK, tokens)
	}
}

// migrationStatusHandler implements GET /api/system/migration/status.
//
//	@Summary		Migration run status snapshot
//	@Description	Returns the wizard state machine snapshot (state, inventory, per-entity progress, error, final report). Polling this endpoint is the documented fallback for proxies that buffer SSE. Admin only.
//	@Tags			migration
//	@Produce		json
//	@Success		200	{object}	MigrationStatus
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin"
//	@Router			/system/migration/status [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationStatusHandler(m *migrationManager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		return c.JSON(http.StatusOK, m.status)
	}
}

// migrationProgressHandler implements GET /api/system/migration/progress.
//
//	@Summary		Migration progress stream (SSE)
//	@Description	Server-sent events stream: first the current status snapshot, then one event per apply progress step ({entity,done,total}) and a final status event when the run finishes or fails. Use the status endpoint as a polling fallback. Admin only.
//	@Tags			migration
//	@Produce		text/event-stream
//	@Success		200	{string}	string	"SSE stream"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse	"Not an admin"
//	@Router			/system/migration/progress [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func migrationProgressHandler(m *migrationManager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Reverse proxies (nginx) must not buffer the stream.
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)

		write := func(frame []byte) error {
			if _, err := w.Write(frame); err != nil {
				return err
			}
			return rc.Flush()
		}

		// Subscribe before building the snapshot so no event published in
		// between is lost (a duplicate progress event is harmless).
		events, cancel := m.subscribe()
		defer cancel()

		// Replay the current snapshot so late subscribers catch up.
		m.mu.Lock()
		snapshot, err := json.Marshal(m.status)
		m.mu.Unlock()
		if err == nil {
			frame := append([]byte("event: status\ndata: "), snapshot...)
			if err := write(append(frame, '\n', '\n')); err != nil {
				return nil //nolint:nilerr // client went away; nothing to do
			}
		}

		// Heartbeat comments keep idle proxy connections alive while
		// fetch/plan run before the first progress event.
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		done := c.Request().Context().Done()
		for {
			select {
			case <-done:
				return nil
			case <-heartbeat.C:
				if err := write([]byte(": ping\n\n")); err != nil {
					return nil //nolint:nilerr // client went away; nothing to do
				}
			case frame := <-events:
				if err := write(frame); err != nil {
					return nil //nolint:nilerr // client went away; nothing to do
				}
			}
		}
	}
}
