package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// getHealth serves one request against a freshly registered health route.
func getHealth(t *testing.T, h Health, target string) (int, HealthStatus) {
	t.Helper()
	e := echo.New()
	RegisterHealth(e, h)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var st HealthStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
	return rec.Code, st
}

func TestHealthBasic(t *testing.T) {
	code, st := getHealth(t, Health{Version: "1.2.3", GitCommit: "abc"}, "/api/health")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "ok", st.Status)
	require.Equal(t, "1.2.3", st.Version)
	require.Nil(t, st.Checks, "the cheap probe must not run any check")
}

func TestHealthFullQueue(t *testing.T) {
	t.Run("queue up", func(t *testing.T) {
		code, st := getHealth(t, Health{PingQueue: func() error { return nil }}, "/api/health?full=1")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "ok", st.Status)
		require.True(t, st.Checks["queue"].OK)
	})

	t.Run("queue down degrades but stays 200", func(t *testing.T) {
		code, st := getHealth(t, Health{PingQueue: func() error { return errors.New("dial refused") }},
			"/api/health?full=1")
		require.Equal(t, http.StatusOK, code, "a lost queue never takes the panel out of the pool")
		require.Equal(t, "degraded", st.Status)
		require.False(t, st.Checks["queue"].OK)
		require.Equal(t, "dial refused", st.Checks["queue"].Error)
	})
}

// TestHealthFullDatabaseDown covers the only check that returns 503: the
// handle is opened lazily against a dead port so Ping fails on first use.
func TestHealthFullDatabaseDown(t *testing.T) {
	sqlDB, err := sql.Open("mysql", "root:@tcp(127.0.0.1:1)/nope")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true},
	)
	require.NoError(t, err)

	code, st := getHealth(t, Health{DB: db}, "/api/health?full=1")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "fail", st.Status)
	require.False(t, st.Checks["database"].OK)
	require.NotContains(t, st.Checks, "daemon", "the daemon check needs a working database")
}

func TestHealthz(t *testing.T) {
	e := echo.New()
	RegisterHealth(e, Health{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok\n", rec.Body.String())
}

func TestCheckTLSCert(t *testing.T) {
	t.Run("fresh certificate", func(t *testing.T) {
		r := checkTLSCert(writeCert(t, 365*24*time.Hour))
		require.True(t, r.OK, r.Error)
		require.Contains(t, r.Detail, "expires")
	})

	t.Run("expiring inside the warning window", func(t *testing.T) {
		r := checkTLSCert(writeCert(t, 10*24*time.Hour))
		require.False(t, r.OK)
		require.Contains(t, r.Error, "30 days")
	})

	t.Run("missing file", func(t *testing.T) {
		require.False(t, checkTLSCert(filepath.Join(t.TempDir(), "absent.pem")).OK)
	})

	t.Run("not a certificate", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "junk.pem")
		require.NoError(t, os.WriteFile(path, []byte("not pem at all"), 0o600))
		require.False(t, checkTLSCert(path).OK)
	})
}

// writeCert emits a self-signed certificate valid for the given duration
// and returns its path.
func writeCert(t *testing.T, valid time.Duration) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "health-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(valid),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}
