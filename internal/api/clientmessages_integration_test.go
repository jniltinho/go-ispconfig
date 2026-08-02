//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeMail records outgoing messages instead of using the network.
type fakeMail struct{ to, subject, body string }

type fakeMailer struct{ sent []fakeMail }

func (m *fakeMailer) Send(to, subject, body string) error {
	m.sent = append(m.sent, fakeMail{to, subject, body})
	return nil
}

// TestClientMessagesAPI covers message-template CRUD, send-message with
// a fake transport, the SMTP-not-configured refusal and the
// welcome-on-create hook (task 4.4). No network involved.
func TestClientMessagesAPI(t *testing.T) {
	db, srv, adminCookie, adminCSRF, deps := newClientsTestEnvDeps(t)
	_ = db

	t.Run("send-message refuses without SMTP config", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients/send-message", adminCookie, adminCSRF,
			map[string]any{"subject": "s", "message": "m"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "error.smtp_not_configured")
	})

	mailer := &fakeMailer{}
	deps.Mailer = mailer

	var welcomeTplID float64
	t.Run("message template CRUD", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/client-message-templates", adminCookie, adminCSRF,
			map[string]any{"template_type": "welcome", "template_name": "Welcome"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "subject_error_empty")

		status, data = call(t, srv, http.MethodPost, "/api/client-message-templates", adminCookie, adminCSRF,
			map[string]any{
				"template_type": "welcome", "template_name": "Welcome",
				"subject": "Welcome {username}",
				"message": "Hello {contact_name}, login {username} / {password}.",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		welcomeTplID = rec["client_message_template_id"].(float64)

		status, data = call(t, srv, http.MethodGet, "/api/client-message-templates", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, string(data), "Welcome {username}")
	})

	t.Run("welcome-on-create renders placeholders incl. plaintext password", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Wanda Welcome", "username": "wanda",
				"password": "wanda-pw-secret1", "email": "wanda@example.com",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		require.Len(t, mailer.sent, 1, "one welcome email")
		m := mailer.sent[0]
		require.Equal(t, "wanda@example.com", m.to)
		require.Equal(t, "Welcome wanda", m.subject)
		require.Contains(t, m.body, "Hello Wanda Welcome, login wanda / wanda-pw-secret1.")
		require.NotContains(t, m.body, "$2", "never the hash")
	})

	t.Run("send-message via template to selected clients", func(t *testing.T) {
		// A second client without an email address is skipped.
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "No Mail", "username": "nomail", "password": "nomail-pw-long1",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		mailer.sent = nil
		status, data = call(t, srv, http.MethodPost, "/api/clients/send-message", adminCookie, adminCSRF,
			map[string]any{"template_id": welcomeTplID})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var res map[string]any
		require.NoError(t, json.Unmarshal(data, &res))
		require.EqualValues(t, 1, res["sent"], "wanda")
		require.EqualValues(t, 1, res["skipped"], "nomail has no address")
		require.Len(t, mailer.sent, 1)
		require.True(t, strings.HasPrefix(mailer.sent[0].subject, "Welcome "))
		require.Contains(t, mailer.sent[0].body, "login wanda / .",
			"{password} is empty outside the welcome path")

		status, data = call(t, srv, http.MethodPost, "/api/clients/send-message", adminCookie, adminCSRF,
			map[string]any{"template_id": 424242})
		require.Equal(t, http.StatusNotFound, status, "%s", data)

		status, data = call(t, srv, http.MethodPost, "/api/clients/send-message", adminCookie, adminCSRF,
			map[string]any{"subject": "only subject"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
	})

	t.Run("non-admin cannot manage message templates", func(t *testing.T) {
		cCookie, cCSRF := login(t, srv, "wanda", "wanda-pw-secret1")
		status, _ := call(t, srv, http.MethodPost, "/api/client-message-templates", cCookie, cCSRF,
			map[string]any{"template_type": "other", "template_name": "x", "subject": "s", "message": "m"})
		require.Equal(t, http.StatusForbidden, status)
	})
}
