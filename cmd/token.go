package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"go-ispconfig/internal/apitoken"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// The CLI half of the API token surface (spec add-api-tokens): it exists so
// an unattended install can mint its first automation credential without a
// browser, and so an operator locked out of the panel can still revoke one.

var (
	tokenOwner   string
	tokenScopes  []string
	tokenIPs     string
	tokenExpires string
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage API tokens for automation",
	Long: `Manage the long-lived API tokens that authenticate automation against
the REST API. A token acts as an existing panel user and can only ever do
less than that user, never more.`,
}

var tokenCreateCmd = &cobra.Command{
	Use:          "create <label>",
	Short:        "Mint a token and print it once",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openTokenDB()
		if err != nil {
			return err
		}
		label := strings.TrimSpace(args[0])
		if label == "" {
			return errors.New("the label must not be empty")
		}
		if len(tokenScopes) == 0 {
			return errors.New("at least one --scope is required (e.g. --scope sites:read)")
		}
		if err := apitoken.ValidateScopes(tokenScopes); err != nil {
			return err
		}
		var expires time.Time
		if tokenExpires != "" {
			if expires, err = time.Parse(time.RFC3339, tokenExpires); err != nil {
				return fmt.Errorf("--expires must be RFC3339 (2027-01-01T00:00:00Z): %w", err)
			}
		}
		for entry := range strings.SplitSeq(tokenIPs, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				if err := apitoken.ValidIPEntry(entry); err != nil {
					return fmt.Errorf("--ips entry %q: %w", entry, err)
				}
			}
		}

		var owner model.SysUser
		if err := db.Where("username = ?", tokenOwner).Take(&owner).Error; err != nil {
			return fmt.Errorf("owner %q not found: %w", tokenOwner, err)
		}
		if owner.Active != 1 {
			return fmt.Errorf("owner %q is not active", tokenOwner)
		}

		meta := apitoken.Meta{Scopes: tokenScopes, Expires: expires}
		row := model.RemoteUser{
			SysUserID: owner.UserID, SysGroupID: owner.DefaultGroup,
			SysPermUser: "riud", SysPermGroup: "riud",
			RemoteUsername:  label,
			RemoteAccess:    "y",
			RemoteIPs:       strings.TrimSpace(tokenIPs),
			RemoteFunctions: meta.String(),
		}
		var plaintext string
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			pt, digest, err := apitoken.Mint(row.RemoteUserID)
			if err != nil {
				return err
			}
			plaintext = pt
			return tx.Model(&model.RemoteUser{}).
				Where("remote_userid = ?", row.RemoteUserID).
				Update("remote_password", digest).Error
		})
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Token %d (%s) created for %s\n", row.RemoteUserID, label, owner.Username)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Scopes: %s\n\n", strings.Join(tokenScopes, ", "))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n", plaintext)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "This is the only time the token is shown. Store it now.")
		return nil
	},
}

var tokenListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List API tokens (never prints secrets)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		db, err := openTokenDB()
		if err != nil {
			return err
		}
		var rows []model.RemoteUser
		if err := db.Order("remote_userid").Find(&rows).Error; err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tLABEL\tOWNER\tSCOPES\tIPS\tEXPIRES\tLAST USED\tENABLED")
		for _, r := range rows {
			meta := apitoken.ParseMeta(r.RemoteFunctions)
			var owner model.SysUser
			_ = db.Where("userid = ?", r.SysUserID).Take(&owner).Error
			_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.RemoteUserID, r.RemoteUsername, owner.Username,
				strings.Join(meta.Scopes, ","), orDash(r.RemoteIPs),
				formatTime(meta.Expires), formatTime(meta.LastUsed),
				yesNo(r.RemoteAccess == "y"))
		}
		return w.Flush()
	},
}

var tokenRevokeCmd = &cobra.Command{
	Use:          "revoke <id>",
	Short:        "Disable a token; the next request carrying it is rejected",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil || id == 0 {
			return fmt.Errorf("invalid token id %q", args[0])
		}
		db, err := openTokenDB()
		if err != nil {
			return err
		}
		res := db.Model(&model.RemoteUser{}).Where("remote_userid = ?", id).
			Update("remote_access", "n")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("token %d not found", id)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Token %d revoked.\n", id)
		return nil
	},
}

func init() {
	tokenCreateCmd.Flags().StringVar(&tokenOwner, "owner", "admin", "sys_user login the token acts as")
	tokenCreateCmd.Flags().StringSliceVar(&tokenScopes, "scope", nil,
		"granted scope, repeatable (resource:action, e.g. sites:read, mail:write, *:read)")
	tokenCreateCmd.Flags().StringVar(&tokenIPs, "ips", "",
		"comma-separated IPs/CIDRs the token may be used from (empty = any)")
	tokenCreateCmd.Flags().StringVar(&tokenExpires, "expires", "",
		"RFC3339 expiry, e.g. 2027-01-01T00:00:00Z (empty = never)")

	tokenCmd.AddCommand(tokenCreateCmd, tokenListCmd, tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}

// openTokenDB loads config.toml and opens the panel database, the same way
// every other database-touching command does.
func openTokenDB() (*gorm.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return database.Open(cfg.Database.DSN)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
