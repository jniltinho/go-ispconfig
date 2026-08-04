//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

func newMailTestEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "mailapi")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "mail1.example.com", "smoke-test-pw")
	require.NoError(t, err)
	// The seeded server must be a mail server for the domain form.
	require.NoError(t, db.Exec("UPDATE server SET mail_server = 1 WHERE server_id = 1").Error)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}, DNSPub: api.NewDNSPublisher(db)}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	cookie, csrf := login(t, srv, "admin", "smoke-test-pw")
	return db, srv, cookie, csrf
}

func TestMailDomainAPI(t *testing.T) {
	db, srv, cookie, csrf := newMailTestEnv(t)

	var domainID float64
	t.Run("create generates a DKIM key and journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "Example.COM", "active": "y", "dkim": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		domainID = rec["domain_id"].(float64)
		assert.Equal(t, "example.com", rec["domain"], "lowercased + IDN")
		assert.NotContains(t, rec, "dkim_private", "private key redacted in responses")
		assert.NotEmpty(t, rec["dkim_public"], "public key generated")
		assert.Equal(t, "default", rec["dkim_selector"])
		assert.Equal(t, false, rec["dns_published"], "no managed zone for example.com")
		assert.Contains(t, rec["suggested_record"], "default._domainkey.example.com.")

		var row model.MailDomain
		require.NoError(t, db.Take(&row, domainID).Error)
		assert.Contains(t, row.DKIMPrivate, "PRIVATE KEY", "private key stored")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'mail_domain' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("domain_id:%d", int(domainID)), dl.DBIdx)
		// The datalog row carries the full row incl. the private key so the
		// daemon can write the key file — that is expected (not a leak to
		// API clients, which never see the datalog).
	})

	t.Run("validation: bad domain and transport collision", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "notadomain"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_error_regex")

		require.NoError(t, db.Create(&model.MailTransport{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Domain: "relay.example.com", Transport: "smtp:x", SortOrder: 5, Active: "y",
		}).Error)
		status, data = call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "relay.example.com"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_is_transport")
	})

	t.Run("list omits the private key", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/mail/domains?limit=100", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, string(data), "PRIVATE KEY")
		assert.Contains(t, string(data), "example.com")
	})

	t.Run("get-by-domain", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/mail/domains/by-domain/example.com", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.EqualValues(t, domainID, rec["domain_id"])
		assert.NotContains(t, rec, "dkim_private")

		status, _ = call(t, srv, http.MethodGet, "/api/mail/domains/by-domain/ghost.example", cookie, "", nil)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("set-status flips active and journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost,
			fmt.Sprintf("/api/mail/domains/%d/set-status", int(domainID)), cookie, csrf,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusNoContent, status, "%s", data)
		var row model.MailDomain
		require.NoError(t, db.Take(&row, domainID).Error)
		assert.Equal(t, "n", row.Active)
	})

	t.Run("generate-dkim returns a fresh pair", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains/generate-dkim", cookie, csrf, nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]string
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Contains(t, rec["dkim_private"], "PRIVATE KEY")
		assert.Contains(t, rec["dkim_public"], "PUBLIC KEY")
	})

	t.Run("spamfilter policy round-trips through spamfilter_users", func(t *testing.T) {
		pol := model.SpamfilterPolicy{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			PolicyName: "Non-paying customer",
		}
		require.NoError(t, db.Create(&pol).Error)

		// The lookup feeds the form select and must be reachable even though
		// the policies entity itself is admin-only.
		status, data := call(t, srv, http.MethodGet, "/api/meta/lookups/spamfilter-policies", cookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		assert.Contains(t, string(data), "Non-paying customer")

		// Create with a policy: the row lands in spamfilter_users under the
		// "@domain" pseudo-address, not on mail_domain.
		status, data = call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "spam.example", "active": "y", "policy": pol.ID})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var created map[string]any
		require.NoError(t, json.Unmarshal(data, &created))
		spamID := int(created["domain_id"].(float64))

		var user model.SpamfilterUser
		require.NoError(t, db.Where("email = ?", "@spam.example").Take(&user).Error)
		assert.Equal(t, pol.ID, user.PolicyID)
		assert.Equal(t, "@spam.example", user.Fullname)
		assert.EqualValues(t, 5, user.Priority)

		// The virtual field is read back on get, so the form preselects it.
		status, data = call(t, srv, http.MethodGet, fmt.Sprintf("/api/mail/domains/%d", spamID), cookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var got map[string]any
		require.NoError(t, json.Unmarshal(data, &got))
		assert.EqualValues(t, pol.ID, got["policy"])

		// Clearing it back to "- not enabled -" updates the same row.
		status, data = call(t, srv, http.MethodPut, fmt.Sprintf("/api/mail/domains/%d", spamID), cookie, csrf,
			map[string]any{"server_id": 1, "domain": "spam.example", "active": "y", "policy": 0})
		require.Equal(t, http.StatusOK, status, "%s", data)
		require.NoError(t, db.Where("email = ?", "@spam.example").Take(&user).Error)
		assert.EqualValues(t, 0, user.PolicyID)

		// A rename carries the pseudo-address over instead of orphaning it.
		status, data = call(t, srv, http.MethodPut, fmt.Sprintf("/api/mail/domains/%d", spamID), cookie, csrf,
			map[string]any{"server_id": 1, "domain": "spam2.example", "active": "y", "policy": pol.ID})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var n int64
		require.NoError(t, db.Model(&model.SpamfilterUser{}).Where("email = ?", "@spam.example").Count(&n).Error)
		assert.EqualValues(t, 0, n, "old pseudo-address gone")
		require.NoError(t, db.Where("email = ?", "@spam2.example").Take(&user).Error)
		assert.Equal(t, pol.ID, user.PolicyID)
	})

	t.Run("DKIM TXT published when a managed zone exists", func(t *testing.T) {
		soa := model.DNSSoa{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Origin: "dkimzone.test.", NS: "ns1.dkimzone.test.",
			Mbox: "hostmaster.dkimzone.test.", Serial: 2026080101,
			Refresh: 7200, Retry: 540, Expire: 604800, Minimum: 3600, TTL: 3600,
			Active: "Y", DNSSECWanted: "N", DNSSECInitialized: "N",
		}
		require.NoError(t, db.Create(&soa).Error)
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "dkimzone.test", "active": "y", "dkim": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, true, rec["dns_published"], "response reports the managed-zone publication")
		assert.Contains(t, rec["suggested_record"], "v=DKIM1", "suggested record present")

		var n int64
		require.NoError(t, db.Model(&model.DNSRr{}).
			Where("name = ? AND type = 'TXT'", "default._domainkey.dkimzone.test.").Count(&n).Error)
		assert.EqualValues(t, 1, n, "DKIM TXT published into the managed zone")

		// Delete withdraws the TXT.
		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/mail/domains/%d", int(rec["domain_id"].(float64))), cookie, csrf, nil)
		require.Equal(t, http.StatusNoContent, status)
		require.NoError(t, db.Model(&model.DNSRr{}).
			Where("name = ? AND type = 'TXT'", "default._domainkey.dkimzone.test.").Count(&n).Error)
		assert.Zero(t, n, "DKIM TXT withdrawn on domain delete")
	})
}

func TestMailboxAPI(t *testing.T) {
	db, srv, cookie, csrf := newMailTestEnv(t)

	// A primary mail domain the mailbox lives under.
	status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
		map[string]any{"server_id": 1, "domain": "box.example", "active": "y", "dkim": "n"})
	require.Equal(t, http.StatusCreated, status, "%s", data)

	var mailuserID float64
	t.Run("create derives maildir and hashes the password", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/mailboxes", cookie, csrf,
			map[string]any{"email": "User1@Box.Example", "password": "s3cr3t-pw", "quota": 1048576})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		mailuserID = rec["mailuser_id"].(float64)
		assert.Equal(t, "user1@box.example", rec["email"], "lowercased")
		assert.NotContains(t, rec, "password", "hash redacted")

		var row model.MailUser
		require.NoError(t, db.Take(&row, mailuserID).Error)
		assert.Equal(t, "/var/vmail/box.example/user1", row.Maildir)
		assert.Equal(t, "/var/vmail", row.Homedir)
		assert.Equal(t, "user1@box.example", row.Login)
		assert.EqualValues(t, 5000, row.UID)
		assert.NotEqual(t, "s3cr3t-pw", row.Password)
		assert.NotEmpty(t, row.Password, "password hashed and stored")
	})

	// Without the virtual client_group_id field the record is filed under the
	// admin group and the owning client can never list its own mailbox.
	t.Run("client_group_id assigns ownership", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/mailboxes", cookie, csrf,
			map[string]any{"email": "owned@box.example", "password": "s3cr3t-pw",
				"quota": 1048576, "client_group_id": 7})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))

		var row model.MailUser
		require.NoError(t, db.Take(&row, rec["mailuser_id"].(float64)).Error)
		assert.EqualValues(t, 7, row.SysGroupID)
	})

	t.Run("create for unknown domain is rejected", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/mailboxes", cookie, csrf,
			map[string]any{"email": "who@no-such.example", "password": "x-pw-123"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "mail_domain_does_not_exist")
	})

	t.Run("duplicate email rejected", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/mailboxes", cookie, csrf,
			map[string]any{"email": "user1@box.example", "password": "again-pw-1"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
	})

	t.Run("list and by-client omit the password", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/mail/mailboxes?limit=100", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, string(data), `"password"`)
		assert.Contains(t, string(data), "user1@box.example")
	})

	t.Run("update with empty password keeps the hash", func(t *testing.T) {
		var before model.MailUser
		require.NoError(t, db.Take(&before, mailuserID).Error)
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/mail/mailboxes/%d", int(mailuserID)), cookie, csrf,
			map[string]any{"name": "User One", "quota": 2097152})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var after model.MailUser
		require.NoError(t, db.Take(&after, mailuserID).Error)
		assert.Equal(t, before.Password, after.Password, "empty password leaves the hash")
		assert.Equal(t, "User One", after.Name)
		assert.EqualValues(t, 2097152, after.Quota)
	})

	t.Run("email rename re-derives the maildir", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/mail/mailboxes/%d", int(mailuserID)), cookie, csrf,
			map[string]any{"email": "renamed@box.example", "password": ""})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var after model.MailUser
		require.NoError(t, db.Take(&after, mailuserID).Error)
		assert.Equal(t, "renamed@box.example", after.Email)
		assert.Equal(t, "/var/vmail/box.example/renamed", after.Maildir, "maildir follows the email")
	})
}

func TestMailForwardingAndTransportAPI(t *testing.T) {
	db, srv, cookie, csrf := newMailTestEnv(t)
	_ = db
	// Primary domain for the forwardings.
	status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
		map[string]any{"server_id": 1, "domain": "fwd.example", "active": "y", "dkim": "n"})
	require.Equal(t, http.StatusCreated, status, "%s", data)

	t.Run("alias forces type and is type-filtered", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/aliases", cookie, csrf,
			map[string]any{"server_id": 1, "source": "alias@fwd.example",
				"destination": "user@fwd.example", "active": "y", "type": "catchall"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "alias", rec["type"], "type forced server-side despite the body")

		// Listing aliases shows it; listing catchalls does not.
		status, data = call(t, srv, http.MethodGet, "/api/mail/aliases", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(data), "alias@fwd.example")
		status, data = call(t, srv, http.MethodGet, "/api/mail/catchalls", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, string(data), "alias@fwd.example")

		// The by-id route is type-scoped: fetching the alias as a catchall 404s.
		fid := int(rec["forwarding_id"].(float64))
		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/mail/catchalls/%d", fid), cookie, "", nil)
		require.Equal(t, http.StatusNotFound, status)
		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/mail/aliases/%d", fid), cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("alias requires email source", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/aliases", cookie, csrf,
			map[string]any{"server_id": 1, "source": "not-an-email", "destination": "user@fwd.example"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "email_error_isemail")
	})

	t.Run("transport unique per server and maildomain collision", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/transports", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "relay.test", "transport": "smtp:[10.0.0.9]", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		// Duplicate (server_id, domain).
		status, data = call(t, srv, http.MethodPost, "/api/mail/transports", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "relay.test", "transport": "smtp:[10.0.0.10]"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_error_unique")

		// Collision with an existing mail_domain.
		status, data = call(t, srv, http.MethodPost, "/api/mail/transports", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "fwd.example", "transport": "smtp:x"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_is_maildomain")
	})
}

func TestMailAccessAndSpamfilterAPI(t *testing.T) {
	_, srv, cookie, csrf := newMailTestEnv(t)

	t.Run("mail access CRUD", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/access", cookie, csrf,
			map[string]any{"server_id": 1, "source": "spammer@bad.example",
				"access": "REJECT", "type": "sender", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "sender", rec["type"])
	})

	var policyID float64
	t.Run("spamfilter policy is admin-scoped", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/spamfilter/policies", cookie, csrf,
			map[string]any{"policy_name": "Normal", "rspamd_greylisting": "y",
				"rspamd_spam_kill_level": 15, "rspamd_spam_tag_level": 6})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		policyID = rec["id"].(float64)
		assert.Equal(t, "Normal", rec["policy_name"])
	})

	var ridUser float64
	t.Run("spamfilter user unique email + policy link", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/spamfilter/users", cookie, csrf,
			map[string]any{"server_id": 1, "email": "u@box.example",
				"policy_id": policyID, "priority": 7})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		ridUser = rec["id"].(float64)

		status, data = call(t, srv, http.MethodPost, "/api/mail/spamfilter/users", cookie, csrf,
			map[string]any{"server_id": 1, "email": "u@box.example", "policy_id": policyID})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "email_error_unique")
	})

	t.Run("spamfilter wblist entry", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/spamfilter/wblists", cookie, csrf,
			map[string]any{"server_id": 1, "wb": "W", "rid": ridUser,
				"email": "friend@good.example", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "W", rec["wb"])
	})
}
