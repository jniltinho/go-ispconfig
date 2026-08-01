package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"gorm.io/gorm/utils/tests"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// dryHandlers builds server_ip entity handlers on a connection-less DryRun
// GORM handle; DB-touching paths (UNIQUE, CRUD) run in the integration
// suite.
func dryHandlers(t *testing.T) *entityHandlers[model.ServerIP] {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	s, err := schema.Parse(&model.ServerIP{}, entitySchemaCache, db.NamingStrategy)
	require.NoError(t, err)
	ent := serverIPEntity()
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	return &entityHandlers[model.ServerIP]{deps: &Deps{DB: db}, ent: ent, schema: s}
}

func TestEntityDefaultsAndBody(t *testing.T) {
	h := dryHandlers(t)
	ctx := context.Background()
	rec := &model.ServerIP{}

	body := map[string]any{
		"server_id":  float64(1), // JSON numbers bind as float64
		"ip_address": "10.0.0.1",
		"sys_userid": float64(99), // undeclared: must be ignored
	}
	require.NoError(t, h.applyDefaults(ctx, rec, body))
	require.NoError(t, h.applyBody(ctx, rec, body))

	require.Equal(t, uint32(1), rec.ServerID)
	require.Equal(t, "10.0.0.1", rec.IPAddress)
	require.Equal(t, "IPv4", rec.IPType, "default applied")
	require.Equal(t, "y", rec.Virtualhost, "default applied")
	require.Equal(t, "80,443", rec.VirtualhostPort, "default applied")
	require.Zero(t, rec.SysUserID, "undeclared sys_ column must not be settable from the body")
}

func TestEntityValidate(t *testing.T) {
	h := dryHandlers(t)
	ctx := context.Background()

	t.Run("valid record passes", func(t *testing.T) {
		rec := &model.ServerIP{ServerID: 1, IPType: "IPv4", IPAddress: "10.0.0.1", VirtualhostPort: "80,443"}
		require.NoError(t, h.validate(ctx, rec, nil))
	})

	t.Run("invalid ip and port collect field keys", func(t *testing.T) {
		rec := &model.ServerIP{ServerID: 1, IPType: "IPv4", IPAddress: "999.1.1.1", VirtualhostPort: "80;x"}
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Equal(t, []string{"ip_error_wrong"}, valErr.Fields["ip_address"])
		require.Equal(t, []string{"error_port_syntax"}, valErr.Fields["virtualhost_port"])
	})
}

func TestLimitHookVeto(t *testing.T) {
	orig := limitHook
	t.Cleanup(func() { limitHook = orig })

	RegisterLimitHook(func(_ context.Context, entity string, _ *repository.Identity) error {
		return &LimitError{Key: "error.limit_" + entity}
	})
	err := limitHook(context.Background(), "server_ip", nil)
	var limErr *LimitError
	require.ErrorAs(t, err, &limErr)
	require.Equal(t, "error.limit_server_ip", limErr.Key)
}

func TestEntityToMapUsesColumnNames(t *testing.T) {
	h := dryHandlers(t)
	m := h.toMap(context.Background(), &model.ServerIP{ServerIPID: 7, IPAddress: "10.0.0.1"})
	require.Equal(t, uint32(7), m["server_ip_id"])
	require.Equal(t, "10.0.0.1", m["ip_address"])
	require.Contains(t, m, "sys_perm_user")
}
