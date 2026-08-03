package cron

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSiteCredentialsOK(t *testing.T) {
	creds, err := ResolveSiteCredentials(
		SiteContext{SystemUser: "web1", SystemGroup: "client1"},
		func(name string) (uint32, uint32, error) {
			assert.Equal(t, "web1", name)
			return 1001, 1001, nil
		},
		func(name string) (uint32, error) {
			assert.Equal(t, "client1", name)
			return 1002, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, uint32(1001), creds.UID)
	assert.Equal(t, uint32(1002), creds.GID)
}

func TestResolveSiteCredentialsRefusesRootUID(t *testing.T) {
	_, err := ResolveSiteCredentials(
		SiteContext{SystemUser: "root", SystemGroup: "root"},
		func(string) (uint32, uint32, error) { return 0, 0, nil },
		func(string) (uint32, error) { return 0, nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root uid")
}

func TestResolveSiteCredentialsRefusesRootGID(t *testing.T) {
	_, err := ResolveSiteCredentials(
		SiteContext{SystemUser: "web1", SystemGroup: "root"},
		func(string) (uint32, uint32, error) { return 1001, 1001, nil },
		func(string) (uint32, error) { return 0, nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root gid")
}

func TestResolveSiteCredentialsMissingUser(t *testing.T) {
	_, err := ResolveSiteCredentials(
		SiteContext{SystemUser: "missing", SystemGroup: "client1"},
		func(string) (uint32, uint32, error) { return 0, 0, errors.New("unknown user") },
		func(string) (uint32, error) { return 1002, nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system_user")
}

func TestResolveSiteCredentialsMissingGroup(t *testing.T) {
	_, err := ResolveSiteCredentials(
		SiteContext{SystemUser: "web1", SystemGroup: "missing"},
		func(string) (uint32, uint32, error) { return 1001, 1001, nil },
		func(string) (uint32, error) { return 0, errors.New("unknown group") },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system_group")
}

func TestResolveSiteCredentialsEmptyNames(t *testing.T) {
	_, err := ResolveSiteCredentials(SiteContext{}, nil, nil)
	require.Error(t, err)
	_, err = ResolveSiteCredentials(SiteContext{SystemUser: "web1"}, nil, nil)
	require.Error(t, err)
}

func TestApplySiteCredentialsSetsSysProcAttr(t *testing.T) {
	cmd := exec.Command("/bin/true")
	err := ApplySiteCredentials(
		cmd,
		SiteContext{SystemUser: "web1", SystemGroup: "client1"},
		func(string) (uint32, uint32, error) { return 1001, 1001, nil },
		func(string) (uint32, error) { return 1002, nil },
	)
	require.NoError(t, err)
	require.NotNil(t, cmd.SysProcAttr)
	require.NotNil(t, cmd.SysProcAttr.Credential)
	assert.Equal(t, uint32(1001), cmd.SysProcAttr.Credential.Uid)
	assert.Equal(t, uint32(1002), cmd.SysProcAttr.Credential.Gid)
	assert.True(t, cmd.SysProcAttr.Setpgid)
	assert.NotNil(t, cmd.Cancel)
	// Cancel with no process is a no-op.
	require.NoError(t, cmd.Cancel())
}

func TestProcessRunnerAbortsWhenCredentialFails(t *testing.T) {
	dir := t.TempDir()
	site := SiteContext{
		DocumentRoot: dir,
		SystemUser:   "web1",
		SystemGroup:  "client1",
	}
	p := &ProcessRunner{
		Configure: ConfigureWithLookups(
			func(string) (uint32, uint32, error) { return 0, 0, nil }, // root uid
			func(string) (uint32, error) { return 1002, nil },
		),
	}
	spec, err := BuildExecSpec("/bin/true", "full", site)
	require.NoError(t, err)
	res := p.Run(t.Context(), spec, site)
	assert.Equal(t, StatusError, res.Status)
	assert.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "root uid")
}
