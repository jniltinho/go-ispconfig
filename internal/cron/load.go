package cron

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// JobFactory builds the run function for one active cron row. The plugin
// supplies URL / full / chrooted executors once those land (tasks 3.3–3.5);
// LoadActiveJobs only wires schedule fields into the runner.
type JobFactory func(job model.Cron) JobFunc

// LoadActiveJobs loads every active client job for serverID into the runner
// (design D2 self-healing: daemon start re-arms from DB state without a new
// datalog event). Invalid schedule rows are skipped with an error return so
// operators notice bad data rather than silent partial loads.
func LoadActiveJobs(ctx context.Context, db *gorm.DB, serverID uint32, runner *ClientJobRunner, factory JobFactory) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("cron: LoadActiveJobs requires a database")
	}
	if runner == nil {
		return 0, fmt.Errorf("cron: LoadActiveJobs requires a runner")
	}
	if factory == nil {
		return 0, fmt.Errorf("cron: LoadActiveJobs requires a JobFactory")
	}

	var rows []model.Cron
	err := db.WithContext(ctx).
		Where("active = ? AND server_id = ?", "y", serverID).
		Order("id").
		Find(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("cron: loading active jobs for server %d: %w", serverID, err)
	}

	return registerRows(runner, rows, factory)
}

// registerRows maps cron rows onto the runner. Exported for tests that build
// rows in-memory without a live database.
func registerRows(runner *ClientJobRunner, rows []model.Cron, factory JobFactory) (int, error) {
	var (
		n    int
		errs []error
	)
	for _, row := range rows {
		job := Job{
			ID:       row.ID,
			RunMin:   row.RunMin,
			RunHour:  row.RunHour,
			RunMday:  row.RunMday,
			RunMonth: row.RunMonth,
			RunWday:  row.RunWday,
			Run:      factory(row),
		}
		// One unparseable row must not disarm every later job: collect and
		// keep going, the daemon logs the aggregate.
		if err := runner.Add(job); err != nil {
			errs = append(errs, fmt.Errorf("cron: registering job id=%d: %w", row.ID, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}
