package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gorm.io/gorm"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	legacyclient "go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/model"
)

// migrateFromOpts are the parsed migrate-from flags. The password lives
// only in process memory and is never logged or written anywhere.
type migrateFromOpts struct {
	url            string
	user           string
	password       string
	only           string
	dryRun         bool
	insecure       bool
	mapAllToLocal  bool
	resetPasswords bool
	orphansToAdmin bool
}

var mfOpts migrateFromOpts

// migrateFromCmd imports clients, web domains and DNS zones from a legacy
// PHP ISPConfig3 panel over its remote JSON API (read-only against the
// source; design add-legacy-migration D8).
var migrateFromCmd = &cobra.Command{
	Use:   "migrate-from",
	Short: "Import clients, sites and DNS from a legacy ISPConfig3 panel",
	Long: `Imports clients (with recreated panel users/groups), web domains, folders
and DNS zones/records from a legacy PHP ISPConfig 3.1+ panel over its remote
JSON API. The legacy panel is only read, never modified.

Requires a legacy remote_user with the read (*_get) grants for client, sites
and dns. Use --dry-run first: it prints the full plan and every conflict
without writing anything locally.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mfOpts.password == "" {
			pw, err := promptPassword(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			mfOpts.password = pw
		}
		openDB := func() (*gorm.DB, error) {
			cfg, err := config.Load()
			if err != nil {
				return nil, err
			}
			return database.Open(cfg.Database.DSN)
		}
		return runMigrateFrom(cmd.Context(), mfOpts, openDB, cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func init() {
	f := migrateFromCmd.Flags()
	f.StringVar(&mfOpts.url, "url", "", "legacy panel base URL, e.g. https://legacy.example.com:8080 (required)")
	f.StringVar(&mfOpts.user, "user", "", "legacy remote_user name (required)")
	f.StringVar(&mfOpts.password, "password", "", "legacy remote_user password (prompted when omitted, keeping it out of shell history)")
	f.StringVar(&mfOpts.only, "only", "clients,sites,dns", "entity subset to import: comma-separated clients,sites,dns")
	f.BoolVar(&mfOpts.dryRun, "dry-run", false, "build and print the plan without writing anything locally")
	f.BoolVar(&mfOpts.insecure, "insecure", false, "disable TLS certificate verification (loudly echoed in the report)")
	f.BoolVar(&mfOpts.mapAllToLocal, "map-all-to-local-server", false, "explicitly confirm mapping a multi-server legacy panel onto the single local server")
	f.BoolVar(&mfOpts.resetPasswords, "reset-passwords", false, "after apply, generate one-time password-reset tokens for every recreated panel user")
	f.BoolVar(&mfOpts.orphansToAdmin, "assign-orphan-zones-to-admin", false, "assign DNS zones whose owning client is absent to admin instead of conflicting")
	_ = migrateFromCmd.MarkFlagRequired("url")
	_ = migrateFromCmd.MarkFlagRequired("user")
	rootCmd.AddCommand(migrateFromCmd)
}

// fprintf is fmt.Fprintf with the CLI-output error ignored.
func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

// fprintln is fmt.Fprintln with the CLI-output error ignored.
func fprintln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

// promptPassword reads the legacy password from the terminal without echo.
func promptPassword(errw io.Writer) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no --password given and stdin is not a terminal; pass --password or run interactively")
	}
	fprintf(errw, "Legacy remote_user password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fprintln(errw)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	if len(pw) == 0 {
		return "", fmt.Errorf("empty password")
	}
	return string(pw), nil
}

// parseSelection parses the --only flag.
func parseSelection(only string) (importer.Selection, error) {
	var sel importer.Selection
	for _, part := range strings.Split(only, ",") {
		switch strings.TrimSpace(part) {
		case "clients":
			sel.Clients = true
		case "sites":
			sel.Sites = true
		case "dns":
			sel.DNS = true
		case "":
		default:
			return sel, fmt.Errorf("invalid --only entry %q (valid: clients, sites, dns)", part)
		}
	}
	if !sel.Clients && !sel.Sites && !sel.DNS {
		return sel, fmt.Errorf("--only selects nothing (valid: clients, sites, dns)")
	}
	return sel, nil
}

// runMigrateFrom is the migrate-from flow: connect → grant preflight →
// inventory → plan → (apply → report). openDB is called only after the
// legacy side checks pass, so login/preflight/guard failures never need a
// local database. Any returned error makes the process exit non-zero.
func runMigrateFrom(ctx context.Context, opts migrateFromOpts, openDB func() (*gorm.DB, error), out, errw io.Writer) error {
	sel, err := parseSelection(opts.only)
	if err != nil {
		return err
	}

	lc, err := legacyclient.New(legacyclient.Options{
		URL:      opts.url,
		Username: opts.user,
		Password: opts.password,
		Insecure: opts.insecure,
	})
	if err != nil {
		return err
	}
	if lc.Insecure() {
		fprintln(errw, "WARNING: TLS certificate verification is DISABLED (--insecure).")
	}
	if lc.PlainHTTP() {
		fprintln(errw, "WARNING: plain http:// URL — credentials travel unencrypted.")
	}

	if err := lc.Login(ctx); err != nil {
		return fmt.Errorf("legacy login failed: %w", err)
	}
	defer lc.Close() //nolint:errcheck // best-effort logout
	if err := lc.Preflight(ctx); err != nil {
		return fmt.Errorf("grant preflight failed: %w", err)
	}
	fprintf(out, "Connected to %s as %s; all required remote functions granted.\n", opts.url, opts.user)

	snap, err := importer.FetchSnapshot(ctx, lc, sel)
	if err != nil {
		return err
	}
	inv := snap.Inventory()
	printInventory(out, inv)

	if inv.MultiServer && !opts.mapAllToLocal {
		names := make([]string, 0, len(inv.Servers))
		for _, s := range inv.Servers {
			names = append(names, fmt.Sprintf("%s (id %s)", s["server_name"], s["server_id"]))
		}
		return fmt.Errorf("the legacy panel reports %d servers (%s); multi-server topologies are not supported — pass --map-all-to-local-server to explicitly map everything onto the single local server",
			len(inv.Servers), strings.Join(names, ", "))
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	var localServer model.Server
	if err := db.Order("server_id").First(&localServer).Error; err != nil {
		return fmt.Errorf("no local server row found (run go-ispconfig migrate first): %w", err)
	}

	plan, err := importer.BuildPlan(ctx, db, snap, importer.Options{
		Selection:                sel,
		TargetServerID:           localServer.ServerID,
		AssignOrphanZonesToAdmin: opts.orphansToAdmin,
	})
	if err != nil {
		return err
	}
	printPlan(out, plan)

	if opts.dryRun {
		if n := len(plan.Conflicts()); n > 0 {
			return fmt.Errorf("dry-run found %d conflict(s); resolve them and re-run", n)
		}
		fprintln(out, "Dry-run: no conflicts; nothing was written.")
		return nil
	}

	fprintln(out, "Applying...")
	lastLine := map[string]int{}
	counts, err := importer.Apply(ctx, db, plan, func(p importer.Progress) {
		// One line per 500 items and one at phase completion.
		if p.Done == p.Total || p.Done-lastLine[p.Entity] >= 500 {
			lastLine[p.Entity] = p.Done
			fprintf(out, "  %s: %d/%d\n", p.Entity, p.Done, p.Total)
		}
	})
	if err != nil {
		return err
	}

	report := importer.BuildReport(plan, counts, importer.ReportInput{
		LegacyHost:  importer.LegacyHost(opts.url),
		Insecure:    lc.Insecure(),
		PlainHTTP:   lc.PlainHTTP(),
		MultiServer: inv.MultiServer,
	})
	printReport(out, report)

	if opts.resetPasswords {
		if err := printResetTokens(ctx, db, report.ResetRequired, out); err != nil {
			return err
		}
	} else if len(report.ResetRequired) > 0 {
		fprintln(out, "Run the same command again with --reset-passwords (the import is idempotent) to generate one-time reset tokens for these users.")
	}
	return nil
}

// printInventory renders the per-entity legacy counts.
func printInventory(out io.Writer, inv *importer.Inventory) {
	fprintln(out, "\nLegacy inventory:")
	for _, row := range []struct {
		name  string
		count int
	}{
		{"clients", inv.Clients},
		{"web domains", inv.WebDomains},
		{"web folders", inv.WebFolders},
		{"web folder users", inv.WebFolderUsers},
		{"dns zones", inv.DNSZones},
		{"dns records", inv.DNSRecords},
		{"dns slave zones", inv.DNSSlaveZones},
		{"dns templates", inv.DNSTemplates},
	} {
		fprintf(out, "  %-18s %6d\n", row.name, row.count)
	}
	for _, s := range inv.Servers {
		fprintf(out, "  legacy server: %s (id %s)\n", s["server_name"], s["server_id"])
	}
}

// printPlan renders the plan summary and every conflict with its reason.
func printPlan(out io.Writer, plan *importer.Plan) {
	fprintln(out, "\nPlan:")
	counts := plan.Counts()
	tables := make([]string, 0, len(counts))
	for table := range counts {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	fprintf(out, "  %-18s %7s %7s %7s %9s\n", "entity", "create", "update", "skip", "conflict")
	for _, table := range tables {
		c := counts[table]
		fprintf(out, "  %-18s %7d %7d %7d %9d\n", table, c.Created, c.Updated, c.Skipped, c.Conflicts)
	}
	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		fprintln(out, "\nConflicts (skipped by apply):")
		for _, it := range conflicts {
			fprintf(out, "  - %s %s: %s\n", it.Table, it.Key, it.Reason)
		}
	}
}

// printReport renders the final report.
func printReport(out io.Writer, r *importer.Report) {
	fprintln(out, "\nImport report:")
	tables := make([]string, 0, len(r.Counts))
	for table := range r.Counts {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		c := r.Counts[table]
		fprintf(out, "  %-18s created %d, updated %d, skipped %d, conflicts %d\n",
			table, c.Created, c.Updated, c.Skipped, c.Conflicts)
	}
	if len(r.Warnings) > 0 {
		fprintln(out, "\nWarnings:")
		for _, w := range r.Warnings {
			fprintf(out, "  ! %s\n", w)
		}
	}
	if len(r.ResetRequired) > 0 {
		fprintln(out, "\nPassword reset REQUIRED for these panel users (no password was imported):")
		for _, u := range r.ResetRequired {
			fprintf(out, "  - %s\n", u)
		}
	}
	if len(r.RsyncSuggestions) > 0 {
		fprintln(out, "\nSite file transfer (run per site, uid/gid remapped):")
		for _, s := range r.RsyncSuggestions {
			fprintf(out, "  %s\n", s)
		}
	}
	fprintln(out, "\nOperational order:")
	for _, step := range r.OperationalOrder {
		fprintf(out, "  %s\n", step)
	}
}

// printResetTokens generates and prints the one-time reset tokens.
func printResetTokens(ctx context.Context, db *gorm.DB, users []string, out io.Writer) error {
	if len(users) == 0 {
		fprintln(out, "No panel users require a password reset.")
		return nil
	}
	tokens, err := importer.GenerateResetTokens(ctx, db, users)
	if err != nil {
		return err
	}
	fprintln(out, "\nOne-time password reset tokens (shown once, store them now):")
	for _, tok := range tokens {
		fprintf(out, "  %-24s %s\n", tok.Username, tok.Token)
	}
	return nil
}
