//go:build integration

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// eventPlugin records raised engine events.
type eventPlugin struct {
	subscribe []string
	got       []string
	last      engine.Data
}

func (*eventPlugin) Name() string { return "capture" }

func (p *eventPlugin) OnLoad(r *engine.Registry) error {
	for _, ev := range p.subscribe {
		if err := r.RegisterEvent(ev, func(_ context.Context, event string, data engine.Data) error {
			p.got = append(p.got, event)
			p.last = data
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// nopExec satisfies engine.Executor without touching the system.
type nopExec struct{}

func (nopExec) Run(context.Context, string, string) error { return nil }

// TestClientEndToEndFlow (task 6.1): API client create → sys_user/
// sys_group rows → datalog → the daemon's client module raises
// client_insert, and delete-everything on a resource-owning client
// journals the child deletes and raises client_delete.
func TestClientEndToEndFlow(t *testing.T) {
	db, srv, adminCookie, adminCSRF := newClientsTestEnv(t)
	ctx := context.Background()

	// Daemon-shaped engine: the client module plus a subscriber plugin.
	reg := engine.NewRegistry(nil)
	plugin := &eventPlugin{subscribe: []string{"client_insert", "client_delete"}}
	require.NoError(t, reg.Load([]engine.Module{clients.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nopExec{}, nil), nil)
	require.NoError(t, err)

	// Drain the seed backlog so assertions see only this test's events.
	require.NoError(t, daemon.RunCycle(ctx))
	plugin.got = nil

	var clientID int
	t.Run("API create provisions identity and journals broadcast datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Flow One", "username": "flowone",
				"password": "flow-pw-long-12", "email": "flow@example.com",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var row model.Client
		require.NoError(t, db.Where("username = 'flowone'").Take(&row).Error)
		clientID = int(row.ClientID)

		var u model.SysUser
		require.NoError(t, db.Where("client_id = ?", clientID).Take(&u).Error)
		var g model.SysGroup
		require.NoError(t, db.Where("client_id = ?", clientID).Take(&g).Error)
		require.Equal(t, g.GroupID, u.DefaultGroup)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'client' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Zero(t, dl.ServerID, "client journal rows broadcast with server_id = 0")
	})

	t.Run("daemon cycle raises client_insert", func(t *testing.T) {
		require.NoError(t, daemon.RunCycle(ctx))
		require.Contains(t, plugin.got, "client_insert")
		require.EqualValues(t, clientID, plugin.last.New["client_id"])
		plugin.got = nil
	})

	t.Run("delete-everything journals child deletes and raises client_delete", func(t *testing.T) {
		// The client owns a DNS zone before deletion.
		cCookie, cCSRF := login(t, srv, "flowone", "flow-pw-long-12")
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", cCookie, cCSRF,
			map[string]any{
				"server_id": 1, "origin": "flow.example.com.", "ns": "ns1.example.com.",
				"mbox": "hostmaster.example.com.", "active": "Y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		status, data = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/clients/%d/everything", clientID), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", data)

		var zoneDel model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_soa' AND action = 'd'").
			Order("datalog_id DESC").First(&zoneDel).Error)
		require.Contains(t, zoneDel.Data, "flow.example.com.", "child resource delete journaled")

		require.NoError(t, daemon.RunCycle(ctx))
		require.Contains(t, plugin.got, "client_delete")
		require.EqualValues(t, clientID, plugin.last.Old["client_id"])
	})
}
