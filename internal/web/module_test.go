package web

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// subscribeAll registers a recorder on every event the web module announces
// and returns the received event names.
func subscribeAll(t *testing.T, r *engine.Registry, events []string) *[]string {
	t.Helper()
	var got []string
	for _, e := range events {
		require.NoError(t, r.RegisterEvent(e, func(_ context.Context, event string, _ engine.Data) error {
			got = append(got, event)
			return nil
		}))
	}
	return &got
}

var allWebEvents = []string{
	"web_domain_insert", "web_domain_update", "web_domain_delete",
	"web_folder_insert", "web_folder_update", "web_folder_delete",
	"web_folder_user_insert", "web_folder_user_update", "web_folder_user_delete",
}

// TestModuleFanOut covers the web-module-events spec: a datalog row for a
// hooked table fans out to the correspondingly named event with the decoded
// payload, for all nine table/action combinations.
func TestModuleFanOut(t *testing.T) {
	r := engine.NewRegistry(nil)
	require.NoError(t, r.Load([]engine.Module{NewModule()}, nil))
	got := subscribeAll(t, r, allWebEvents)

	data := engine.Data{New: map[string]any{"domain": "example.com"}}
	for _, table := range []string{"web_domain", "web_folder", "web_folder_user"} {
		for _, action := range []string{"i", "u", "d"} {
			require.NoError(t, r.RaiseTableHook(context.Background(), table, action, data))
		}
	}

	assert.Equal(t, []string{
		"web_domain_insert", "web_domain_update", "web_domain_delete",
		"web_folder_insert", "web_folder_update", "web_folder_delete",
		"web_folder_user_insert", "web_folder_user_update", "web_folder_user_delete",
	}, *got)
}

// TestModulePayloadReachesSubscriber asserts the decoded {old,new} payload
// arrives untouched at the plugin.
func TestModulePayloadReachesSubscriber(t *testing.T) {
	r := engine.NewRegistry(nil)
	require.NoError(t, r.Load([]engine.Module{NewModule()}, nil))

	var gotData engine.Data
	require.NoError(t, r.RegisterEvent("web_domain_update", func(_ context.Context, _ string, d engine.Data) error {
		gotData = d
		return nil
	}))

	in := engine.Data{
		Old: map[string]any{"domain": "old.example.com"},
		New: map[string]any{"domain": "example.com"},
	}
	require.NoError(t, r.RaiseTableHook(context.Background(), "web_domain", "u", in))
	assert.Equal(t, in, gotData)
}

// TestModuleIgnoresUnhookedTable covers the spec scenario "Unhooked table is
// ignored": ftp_user rows raise nothing and processing continues.
func TestModuleIgnoresUnhookedTable(t *testing.T) {
	r := engine.NewRegistry(nil)
	require.NoError(t, r.Load([]engine.Module{NewModule()}, nil))
	got := subscribeAll(t, r, allWebEvents)

	require.NoError(t, r.RaiseTableHook(context.Background(), "ftp_user", "i", engine.Data{}))
	assert.Empty(t, *got)
}

// TestClientDeleteNotAnnouncedHere pins the ownership move: since
// add-client-module, client_delete is announced (and raised) by the
// client module, not the web module.
func TestClientDeleteNotAnnouncedHere(t *testing.T) {
	r := engine.NewRegistry(nil)
	require.NoError(t, r.Load([]engine.Module{NewModule()}, nil))
	assert.ErrorIs(t, r.RegisterEvent("client_delete", func(context.Context, string, engine.Data) error {
		return nil
	}), engine.ErrEventNotAnnounced)
}
