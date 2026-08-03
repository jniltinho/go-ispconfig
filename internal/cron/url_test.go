package cron

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLExecutorSuccess(t *testing.T) {
	var gotUA, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("ok-body"))
	}))
	t.Cleanup(srv.Close)

	ex := &URLExecutor{Client: srv.Client(), Timeout: 5 * time.Second}
	// Build command with {DOMAIN} pointing at the test server host.
	host := strings.TrimPrefix(srv.URL, "http://")
	cmd := "http://{DOMAIN}/cron.php"
	res := ex.Execute(context.Background(), cmd, host)

	require.Equal(t, StatusOK, res.Status, "err=%v output=%s", res.Err, res.Output)
	assert.Equal(t, 200, res.ExitCode)
	assert.Equal(t, "ok-body", res.Output)
	assert.Equal(t, DefaultURLUserAgent, gotUA)
	assert.Equal(t, "/cron.php", gotPath)
	assert.False(t, res.Start.IsZero())
	assert.False(t, res.End.IsZero())
}

func TestURLExecutorInsecureCommandSkipped(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	ex := &URLExecutor{Client: srv.Client(), Timeout: time.Second}

	for _, cmd := range []string{
		"https://example.com/cron\n.php",
		"https://example.com/cron\\x",
		"https://example.com/cron\x00",
	} {
		res := ex.Execute(context.Background(), cmd, "example.com")
		assert.Equal(t, StatusError, res.Status, cmd)
		assert.Error(t, res.Err, cmd)
	}
	assert.False(t, called, "insecure commands must not hit the network")
}

func TestURLExecutorHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	ex := &URLExecutor{Client: srv.Client(), Timeout: time.Second}
	res := ex.Execute(context.Background(), "http://{DOMAIN}/x", host)
	assert.Equal(t, StatusExit, res.Status)
	assert.Equal(t, 500, res.ExitCode)
	assert.Error(t, res.Err)
}

func TestURLExecutorTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	ex := &URLExecutor{Client: srv.Client(), Timeout: 20 * time.Millisecond}
	res := ex.Execute(context.Background(), "http://{DOMAIN}/slow", host)
	assert.Equal(t, StatusTimeout, res.Status)
	assert.Error(t, res.Err)
}

func TestURLExecutorInvalidScheme(t *testing.T) {
	ex := &URLExecutor{Timeout: time.Second}
	res := ex.Execute(context.Background(), "ftp://example.com/x", "example.com")
	assert.Equal(t, StatusError, res.Status)
	assert.Error(t, res.Err)
}

func TestURLExecutorEmptyHost(t *testing.T) {
	ex := &URLExecutor{Timeout: time.Second}
	res := ex.Execute(context.Background(), "http:///no-host", "")
	assert.Equal(t, StatusError, res.Status)
	assert.Error(t, res.Err)
}
