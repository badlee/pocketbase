package migrations

import (
	"github.com/pocketbase/dbx"
)

func init() {
	AppMigrations.Register(func(db dbx.Builder) error {
		// add new name column
		if _, err := db.AddColumn("_admins", "name", `TEXT DEFAULT "" NOT NULL`).Execute(); err != nil {
			return err
		}
		return nil
	}, func(db dbx.Builder) error {
		// drop name column
		if _, err := db.DropColumn("_admins", "name").Execute(); err != nil {
			return err
		}

		return nil
	})
}
