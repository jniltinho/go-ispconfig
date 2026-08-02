package installer

import (
	"context"
	"fmt"
	"strings"
)

// aptEnv makes apt fully noninteractive (design risk 1).
var aptEnv = []string{"DEBIAN_FRONTEND=noninteractive"}

// aptOptions: keep existing config files on upgrades and wait up to 10
// minutes for a held dpkg lock (unattended-upgrades) instead of failing.
var aptOptions = []string{
	"-o", "Dpkg::Options::=--force-confold",
	"-o", "DPkg::Lock::Timeout=600",
}

// packagesStep installs the profile package set via apt, skipping packages
// that are already installed (idempotent re-run).
type packagesStep struct{}

// Name identifies the step in the pipeline log.
func (packagesStep) Name() string { return "packages" }

// Run installs the missing packages of the profile set (php-fpm only when
// the php-fpm answer is enabled) with one apt-get invocation.
func (packagesStep) Run(ctx context.Context, st *State) error {
	wanted := append([]string{}, st.Profile.Packages...)
	if st.Answers.InstallPHPFPM {
		wanted = append(wanted, st.Profile.PHPFPMPackage())
	}

	var missing []string
	for _, pkg := range wanted {
		installed, err := dpkgInstalled(ctx, st, pkg)
		if err != nil {
			return err
		}
		if !installed {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return Skip("all packages already installed")
	}

	st.logf("  installing: %s", strings.Join(missing, " "))
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", append(aptOptions, "update", "-q")...); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	args := append(append([]string{}, aptOptions...), "install", "-y", "-q")
	args = append(args, missing...)
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
		return fmt.Errorf("apt-get install: %w", err)
	}
	return nil
}

// dpkgInstalled reports whether a package is in "install ok installed"
// state. dpkg-query exits non-zero for unknown packages: that is "not
// installed", not an error.
func dpkgInstalled(ctx context.Context, st *State, pkg string) (bool, error) {
	out, err := st.Exec.Run(ctx, nil, "dpkg-query", "-W", "-f=${Status}", pkg)
	if err != nil {
		return false, nil //nolint:nilerr // unknown package = not installed
	}
	return strings.Contains(string(out), "install ok installed"), nil
}
