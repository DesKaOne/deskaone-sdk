package database

import (
	"context"
	"fmt"
)

func (e *DatabaseEngine) Migrate(ctx context.Context, migrations []string) error {
	if len(migrations) == 0 {
		return nil
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for i, migration := range migrations {
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i, err)
		}
	}

	return tx.Commit()
}
