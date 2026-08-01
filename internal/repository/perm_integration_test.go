//go:build integration

// Package repository integration suite: runs the riud permission scope,
// reseller graph, brute-force lockout and session store against a real
// MariaDB (MySQL LIKE/IN semantics, NOW() clock), per task 3.4. This is the
// cross-client isolation suite AGENTS.md requires whenever repository/auth
// change.
package repository_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// Fixture ids: two resellers with one client each plus an independent
// client. Group/client/user ids start high to stay clear of the dump's
// seed rows (admin user 1, admin group 1).
const (
	grpReseller1 = 10
	grpClientA   = 11
	grpReseller2 = 12
	grpClientB   = 13
	grpClientC   = 14

	cltReseller1 = 100
	cltClientA   = 101
	cltReseller2 = 102
	cltClientB   = 103
	cltClientC   = 104

	usrReseller1 = 20
	usrClientA   = 21
	usrReseller2 = 22
	usrClientB   = 23
	usrClientC   = 24
	usrClientA2  = 25 // second panel user in client A's group

	domA   = 200 // owned by client A, riud/riud/-
	domB   = 201 // owned by client B, riud/riud/-
	domC   = 202 // owned by client C, riud/riud/-
	domRO  = 203 // owned by client A, ri/ri/- (no update/delete)
	domPub = 204 // owned by client C, riud/riud/r (world-readable)
)

func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsnPrefix, name := database.StartMariaDB(t, "perm")
	database.MariaDBExec(t, name, "CREATE DATABASE perm CHARACTER SET utf8mb4")

	db, err := database.Open(dsnPrefix + "/perm?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	created, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, created)

	for _, stmt := range []string{
		"INSERT INTO sys_group (groupid, name, client_id) VALUES " +
			"(10,'reseller1',100),(11,'clientA',101),(12,'reseller2',102),(13,'clientB',103),(14,'clientC',104)",
		"INSERT INTO client (client_id, sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other, parent_client_id, contact_name) VALUES " +
			"(100,20,10,'riud','riud','',0,'reseller1')," +
			"(101,21,11,'riud','riud','',100,'clientA')," +
			"(102,22,12,'riud','riud','',0,'reseller2')," +
			"(103,23,13,'riud','riud','',102,'clientB')," +
			"(104,24,14,'riud','riud','',0,'clientC')",
		"INSERT INTO sys_user (userid, sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other, username, passwort, typ, active, groups, default_group, client_id) VALUES " +
			"(20,1,10,'riud','riud','','reseller1','x','user',1,'10',10,100)," +
			"(21,20,11,'riud','riud','','clientA','x','user',1,'11',11,101)," +
			"(22,1,12,'riud','riud','','reseller2','x','user',1,'12',12,102)," +
			"(23,22,13,'riud','riud','','clientB','x','user',1,'13',13,103)," +
			"(24,1,14,'riud','riud','','clientC','x','user',1,'14',14,104)," +
			"(25,20,11,'riud','riud','','clientA2','x','user',1,'11',11,101)",
		"INSERT INTO web_domain (domain_id, sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other, server_id, domain, type) VALUES " +
			"(200,21,11,'riud','riud','',1,'a.example','vhost')," +
			"(201,23,13,'riud','riud','',1,'b.example','vhost')," +
			"(202,24,14,'riud','riud','',1,'c.example','vhost')," +
			"(203,21,11,'ri','ri','',1,'ro.example','vhost')," +
			"(204,24,14,'riud','riud','r',1,'pub.example','vhost')",
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

// newDomain builds an insertable web_domain record: enum columns must carry
// valid members (Go zero-value "" is rejected by MariaDB strict mode); the
// CRUD framework of the REST API group populates these from form defaults.
func newDomain(sysUserID, sysGroupID uint32, domain string) *model.WebDomain {
	return &model.WebDomain{
		SysUserID: sysUserID, SysGroupID: sysGroupID,
		SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, Domain: domain, Type: "vhost",
		CGI: "n", SSI: "n", Suexec: "y", Subdomain: "none",
		Ruby: "n", Python: "n", Perl: "n",
		SSL: "n", SSLLetsencrypt: "n", SSLLetsencryptExclude: "n",
		RewriteToHTTPS: "n", PHPFPMUseSocket: "y", EnablePagespeed: "n",
		PHPFPMChroot: "n", PM: "ondemand", BackupEncrypt: "n",
		Active: "y", TrafficQuotaLock: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
	}
}

// identity resolves the Identity of a fixture user through the real
// ResolveIdentity path (CSV groups + reseller graph).
func identity(t *testing.T, db *gorm.DB, userID uint32) *repository.Identity {
	t.Helper()
	var u model.SysUser
	require.NoError(t, db.Where("userid = ?", userID).First(&u).Error)
	id, err := repository.ResolveIdentity(db, &u)
	require.NoError(t, err)
	return id
}

func domains(t *testing.T, db *gorm.DB, id *repository.Identity) []string {
	t.Helper()
	repo, err := repository.New[model.WebDomain](db)
	require.NoError(t, err)
	var recs []model.WebDomain
	require.NoError(t, repo.List(context.Background(), id, &recs))
	names := make([]string, 0, len(recs))
	for _, r := range recs {
		names = append(names, r.Domain)
	}
	return names
}

// TestIntegration shares one MariaDB container across the permission,
// lockout and session sub-suites (container startup dominates runtime).
func TestIntegration(t *testing.T) {
	db := setupDB(t)
	t.Run("Permissions", func(t *testing.T) { testPermissions(t, db) })
	t.Run("LoginLockout", func(t *testing.T) { testLoginLockout(t, db) })
	t.Run("SessionStore", func(t *testing.T) { testSessionStore(t, db) })
	t.Run("SecurityPolicies", func(t *testing.T) { testSecurityPolicies(t, db) })
}

func testSecurityPolicies(t *testing.T, db *gorm.DB) {
	t.Run("defaults apply without seeded rows", func(t *testing.T) {
		v, err := auth.GetPolicy(db, "admin_allow_server_config")
		require.NoError(t, err)
		require.Equal(t, "superadmin", v)
	})

	t.Run("seed stores defaults but keeps operator overrides", func(t *testing.T) {
		require.NoError(t, db.Exec(
			"INSERT INTO sys_config (`group`, `name`, `value`) VALUES ('security', 'remote_api_allowed', 'no')").Error)
		require.NoError(t, auth.SeedPolicyDefaults(db))
		require.NoError(t, auth.SeedPolicyDefaults(db), "seeding must be idempotent")

		v, err := auth.GetPolicy(db, "remote_api_allowed")
		require.NoError(t, err)
		require.Equal(t, "no", v, "operator override must survive seeding")

		var n int64
		require.NoError(t, db.Model(&model.SysConfig{}).Where("`group` = 'security'").Count(&n).Error)
		require.EqualValues(t, 17, n)
	})

	t.Run("stored flag overrides the default", func(t *testing.T) {
		require.NoError(t, db.Exec(
			"UPDATE sys_config SET value = 'yes' WHERE `group` = 'security' AND name = 'admin_allow_langedit'").Error)
		ok, err := auth.CheckPolicy(db, "admin_allow_langedit", 42)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("superadmin flag blocks non-id-1 admin via middleware", func(t *testing.T) {
		superadmin := &auth.SessionData{UserID: 1, Username: "admin", Typ: "admin"}
		otherAdmin := &auth.SessionData{UserID: 2, Username: "admin2", Typ: "admin"}
		store := fakeSessions{"sa": superadmin, "adm2": otherAdmin}

		e := echo.New()
		e.Use(auth.Middleware(store))
		e.GET("/api/server-config", func(c *echo.Context) error { return c.NoContent(http.StatusOK) },
			auth.RequireAuth(), auth.RequirePolicy(db, "admin_allow_server_config"))

		get := func(sid string) int {
			req := httptest.NewRequest(http.MethodGet, "/api/server-config", nil)
			if sid != "" {
				req.Header.Set("Authorization", "Bearer "+sid)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec.Code
		}

		require.Equal(t, http.StatusUnauthorized, get(""))
		require.Equal(t, http.StatusOK, get("sa"))
		require.Equal(t, http.StatusForbidden, get("adm2"),
			"spec scenario: superadmin flag returns 403 for a non-id-1 admin")
	})
}

// fakeSessions avoids creating real DB sessions for the middleware check.
type fakeSessions map[string]*auth.SessionData

// Get implements auth.SessionGetter.
func (f fakeSessions) Get(id string) (*auth.SessionData, error) {
	if d, ok := f[id]; ok {
		return d, nil
	}
	return nil, auth.ErrSessionNotFound
}

func testPermissions(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	repo, err := repository.New[model.WebDomain](db)
	require.NoError(t, err)

	admin := identity(t, db, 1)
	reseller1 := identity(t, db, usrReseller1)
	reseller2 := identity(t, db, usrReseller2)
	clientA := identity(t, db, usrClientA)
	clientA2 := identity(t, db, usrClientA2)
	clientC := identity(t, db, usrClientC)

	t.Run("reseller graph resolves client groups", func(t *testing.T) {
		require.True(t, admin.IsAdmin())
		require.ElementsMatch(t, []uint32{grpReseller1, grpClientA}, reseller1.Groups)
		require.ElementsMatch(t, []uint32{grpReseller2, grpClientB}, reseller2.Groups)
		require.ElementsMatch(t, []uint32{grpClientA}, clientA.Groups)
	})

	t.Run("admin sees everything", func(t *testing.T) {
		require.ElementsMatch(t,
			[]string{"a.example", "b.example", "c.example", "ro.example", "pub.example"},
			domains(t, db, admin))
	})

	t.Run("cross-client isolation", func(t *testing.T) {
		// Client A sees only its own records plus world-readable ones.
		require.ElementsMatch(t, []string{"a.example", "ro.example", "pub.example"},
			domains(t, db, clientA))
		// Get on another client's record is denied.
		var rec model.WebDomain
		require.ErrorIs(t, repo.Get(ctx, clientA, domB, &rec), repository.ErrPermissionDenied)
	})

	t.Run("group access within the same client", func(t *testing.T) {
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, clientA2, domA, &rec), "second user in the group reads via sys_perm_group")
		require.Equal(t, "a.example", rec.Domain)
	})

	t.Run("reseller reaches client group records", func(t *testing.T) {
		require.ElementsMatch(t, []string{"a.example", "ro.example", "pub.example"},
			domains(t, db, reseller1))
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, reseller1, domA, &rec))
	})

	t.Run("cross-reseller isolation", func(t *testing.T) {
		require.ElementsMatch(t, []string{"b.example", "pub.example"}, domains(t, db, reseller2))
		var rec model.WebDomain
		require.ErrorIs(t, repo.Get(ctx, reseller2, domA, &rec), repository.ErrPermissionDenied)
		require.ErrorIs(t, repo.Delete(ctx, reseller2, domA), repository.ErrPermissionDenied)
	})

	t.Run("perm_other grants read but not write", func(t *testing.T) {
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, clientA, domPub, &rec))
		require.ErrorIs(t, repo.Update(ctx, clientA, &rec), repository.ErrPermissionDenied)
	})

	t.Run("update denied without u flag", func(t *testing.T) {
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, clientA, domRO, &rec))
		require.ErrorIs(t, repo.Update(ctx, clientA, &rec), repository.ErrPermissionDenied)
		ok, err := repo.CheckPerm(ctx, clientA, domRO, repository.PermUpdate)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("delete denied without d flag", func(t *testing.T) {
		require.ErrorIs(t, repo.Delete(ctx, clientA, domRO), repository.ErrPermissionDenied)
	})

	t.Run("owner updates and deletes with u and d flags", func(t *testing.T) {
		var rec model.WebDomain
		require.NoError(t, repo.Get(ctx, clientA, domA, &rec))
		rec.Domain = "a-renamed.example"
		require.NoError(t, repo.Update(ctx, clientA, &rec))

		var again model.WebDomain
		require.NoError(t, repo.Get(ctx, clientA, domA, &again))
		require.Equal(t, "a-renamed.example", again.Domain)

		// Reseller may also update the client's record through the group.
		again.Domain = "a.example"
		require.NoError(t, repo.Update(ctx, reseller1, &again))
	})

	t.Run("insert honors the record preset", func(t *testing.T) {
		ownRec := newDomain(usrClientA, grpClientA, "new-a.example")
		require.NoError(t, repo.Insert(ctx, clientA, ownRec))

		foreign := newDomain(usrClientC, grpClientC, "evil.example")
		require.ErrorIs(t, repo.Insert(ctx, clientA, foreign), repository.ErrPermissionDenied)
		require.NoError(t, repo.Insert(ctx, clientC, foreign))
	})
}

func testLoginLockout(t *testing.T, db *gorm.DB) {
	const ip = "203.0.113.7"

	locked, err := repository.TooManyLoginAttempts(db, ip)
	require.NoError(t, err)
	require.False(t, locked)

	for i := 0; i < 6; i++ {
		require.NoError(t, repository.RecordFailedLogin(db, ip))
	}
	locked, err = repository.TooManyLoginAttempts(db, ip)
	require.NoError(t, err)
	require.True(t, locked, "6th failure within a minute must lock")

	// A different address stays unaffected.
	locked, err = repository.TooManyLoginAttempts(db, "198.51.100.1")
	require.NoError(t, err)
	require.False(t, locked)

	require.NoError(t, repository.ClearLoginAttempts(db, ip))
	locked, err = repository.TooManyLoginAttempts(db, ip)
	require.NoError(t, err)
	require.False(t, locked)
}

func testSessionStore(t *testing.T, db *gorm.DB) {
	store := auth.NewStore(db, time.Hour)

	id, err := store.Create(&auth.SessionData{UserID: 21, Username: "clientA", Typ: "user", Groups: []uint32{11}})
	require.NoError(t, err)
	require.Len(t, id, 64)

	data, err := store.Get(id)
	require.NoError(t, err)
	require.Equal(t, "clientA", data.Username)
	require.NotEmpty(t, data.CSRFToken)

	t.Run("expired session is rejected and removed", func(t *testing.T) {
		// Backdate with a Go-side timestamp so it round-trips through the
		// same parseTime/loc conversion the store uses.
		require.NoError(t, db.Exec(
			"UPDATE sys_session SET last_updated = ? WHERE session_id = ?",
			time.Now().Add(-2*time.Hour), id).Error)
		_, err := store.Get(id)
		require.ErrorIs(t, err, auth.ErrSessionNotFound)
		var n int64
		require.NoError(t, db.Model(&model.SysSession{}).Where("session_id = ?", id).Count(&n).Error)
		require.Zero(t, n, "expired session row must be deleted")
	})

	t.Run("permanent session never expires", func(t *testing.T) {
		pid, err := store.Create(&auth.SessionData{UserID: 21, Username: "clientA"})
		require.NoError(t, err)
		require.NoError(t, db.Exec(
			"UPDATE sys_session SET permanent = 'y', last_updated = ? WHERE session_id = ?",
			time.Now().Add(-2*time.Hour), pid).Error)
		_, err = store.Get(pid)
		require.NoError(t, err)
	})

	t.Run("delete removes the session", func(t *testing.T) {
		did, err := store.Create(&auth.SessionData{UserID: 21})
		require.NoError(t, err)
		require.NoError(t, store.Delete(did))
		_, err = store.Get(did)
		require.ErrorIs(t, err, auth.ErrSessionNotFound)
	})

	t.Run("leftover PHP-serialized session is not accepted", func(t *testing.T) {
		require.NoError(t, db.Exec(
			"INSERT INTO sys_session (session_id, date_created, last_updated, permanent, session_data) VALUES ('phpsess', NOW(), NOW(), 'n', 'a:1:{s:1:\\\"s\\\";b:1;}')").Error)
		_, err := store.Get("phpsess")
		require.ErrorIs(t, err, auth.ErrSessionNotFound)
	})
}
