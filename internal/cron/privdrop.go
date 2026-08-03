package cron

import (
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// UserLookup resolves a system username to uid and primary gid.
// Tests inject stubs; production uses os/user.
type UserLookup func(name string) (uid, gid uint32, err error)

// GroupLookup resolves a system group name to gid.
type GroupLookup func(name string) (gid uint32, err error)

// defaultUserLookup uses os/user.Lookup.
func defaultUserLookup(name string) (uint32, uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing gid %q: %w", u.Gid, err)
	}
	return uint32(uid), uint32(gid), nil
}

// defaultGroupLookup uses os/user.LookupGroup.
func defaultGroupLookup(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing gid %q: %w", g.Gid, err)
	}
	return uint32(gid), nil
}

// SiteCredentials is the non-root uid/gid pair for a full/chrooted job.
type SiteCredentials struct {
	UID uint32
	GID uint32
}

// ResolveSiteCredentials maps system_user / system_group to uid/gid and
// refuses root (design D5 / PHP is_allowed_user parity). lookupUser and
// lookupGroup may be nil (os/user defaults).
func ResolveSiteCredentials(site SiteContext, lookupUser UserLookup, lookupGroup GroupLookup) (SiteCredentials, error) {
	if site.SystemUser == "" {
		return SiteCredentials{}, fmt.Errorf("cron: site system_user is empty")
	}
	if site.SystemGroup == "" {
		return SiteCredentials{}, fmt.Errorf("cron: site system_group is empty")
	}
	if lookupUser == nil {
		lookupUser = defaultUserLookup
	}
	if lookupGroup == nil {
		lookupGroup = defaultGroupLookup
	}

	uid, _, err := lookupUser(site.SystemUser)
	if err != nil {
		return SiteCredentials{}, fmt.Errorf("cron: resolving system_user %q: %w", site.SystemUser, err)
	}
	if uid == 0 {
		return SiteCredentials{}, fmt.Errorf("cron: refusing root uid for system_user %q", site.SystemUser)
	}

	gid, err := lookupGroup(site.SystemGroup)
	if err != nil {
		return SiteCredentials{}, fmt.Errorf("cron: resolving system_group %q: %w", site.SystemGroup, err)
	}
	if gid == 0 {
		return SiteCredentials{}, fmt.Errorf("cron: refusing root gid for system_group %q", site.SystemGroup)
	}
	return SiteCredentials{UID: uid, GID: gid}, nil
}

// ApplySiteCredentials sets SysProcAttr.Credential and Setpgid so a timeout
// can kill the whole process group (design D5). Root uid/gid and lookup
// failures abort before Start — a job is never executed as root.
//
// Note: Go's syscall.SysProcAttr does not expose Linux PR_SET_NO_NEW_PRIVS;
// the fail-closed Credential drop is the enforceable privilege boundary.
func ApplySiteCredentials(cmd *exec.Cmd, site SiteContext, lookupUser UserLookup, lookupGroup GroupLookup) error {
	creds, err := ResolveSiteCredentials(site, lookupUser, lookupGroup)
	if err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid: creds.UID,
		Gid: creds.GID,
	}
	cmd.SysProcAttr.Setpgid = true
	// On context cancel, kill the process group (not only the leader).
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid = process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return nil
}

// ConfigureWithLookups returns a ProcessRunner.Configure func that applies
// site credentials using the given lookups (nil → os/user).
func ConfigureWithLookups(lookupUser UserLookup, lookupGroup GroupLookup) func(*exec.Cmd, SiteContext) error {
	return func(cmd *exec.Cmd, site SiteContext) error {
		return ApplySiteCredentials(cmd, site, lookupUser, lookupGroup)
	}
}
