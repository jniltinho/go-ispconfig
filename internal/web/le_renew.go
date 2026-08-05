package web

import (
	"context"
	"fmt"
	"log/slog"

	"go-ispconfig/internal/engine"
)

// RenewJobName is the scheduler job identifier for the daily renewal.
const RenewJobName = "letsencrypt_renew"

// RenewJobSpec runs the renewal daily at 02:00 (the client owns the real
// renewal-due logic; this just triggers it).
const RenewJobSpec = "0 2 * * *"

// renewer is the subset of acme.Manager renewal jobs need (testable).
type renewer interface {
	RenewDue() (int, error)
}

// RenewNative renews certificates issued by the in-process ACME client and
// schedules a reload of serviceKey when any lineage was renewed.
func RenewNative(ctx context.Context, mgr renewer, services *engine.Services, log *slog.Logger, serviceKey string) error {
	if log == nil {
		log = slog.Default()
	}
	if mgr == nil {
		log.Info("web: LE renewal skipped, no ACME manager")
		return nil
	}
	_ = ctx
	n, err := mgr.RenewDue()
	if err != nil {
		return fmt.Errorf("web: LE renewal failed: %w", err)
	}
	if n > 0 {
		log.Info("web: LE certificates renewed, scheduling a reload", "count", n, "service", serviceKey)
		if services != nil {
			services.Register(serviceKey)
			services.RestartServiceDelayed(serviceKey, engine.ActionReload)
		}
	}
	return nil
}
