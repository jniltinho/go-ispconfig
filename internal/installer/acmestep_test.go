package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcmeStepDefaultOff(t *testing.T) {
	st, mock, _ := testState(t)
	err := acmeStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "not requested")
	assert.Empty(t, mock.calls)
}

func TestAcmeStepInstallsAcmeSh(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.InstallAcme = true
	st.Answers.AdminEmail = "admin@example.com"

	require.NoError(t, acmeStep{}.Run(context.Background(), st))
	joined := strings.Join(mock.calls, "|")
	assert.Contains(t, joined, "get.acme.sh")
	assert.Contains(t, joined, "email=admin@example.com")
}

func TestAcmeStepInstallsCertbotWhenChosen(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.InstallAcme = true
	st.Answers.AcmeClient = "certbot"

	require.NoError(t, acmeStep{}.Run(context.Background(), st))
	assert.True(t, mock.called("apt-get"))
	assert.Contains(t, strings.Join(mock.calls, "|"), "certbot")
}

func TestAcmeStepExistingClientNoop(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.InstallAcme = true
	mock.missing["acme.sh"] = false // acme.sh on PATH

	err := acmeStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "already installed")
	assert.False(t, mock.called("sh -c"))
	assert.False(t, mock.called("apt-get"))
}

func TestAcmeStepExistingHomeInstallNoop(t *testing.T) {
	st, mock, _ := testState(t)
	st.Answers.InstallAcme = true
	require.NoError(t, os.MkdirAll(st.AcmeShHome, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(st.AcmeShHome, "acme.sh"), []byte("#!/bin/sh"), 0o755))

	err := acmeStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "already installed")
	assert.False(t, mock.called("sh -c"))
}
