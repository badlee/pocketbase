// Package migrations contains the system PocketBase DB migrations.
package migrations

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/models/schema"
	"github.com/pocketbase/pocketbase/models/settings"
	"github.com/pocketbase/pocketbase/tools/migrate"
	"github.com/pocketbase/pocketbase/tools/types"
)

var AppMigrations migrate.MigrationsList

// Register is a short alias for `AppMigrations.Register()`
// that is usually used in external/user defined migrations.
func Register(
	up func(db dbx.Builder) error,
	down func(db dbx.Builder) error,
	optFilename ...string,
) {
	var optFiles []string
	if len(optFilename) > 0 {
		optFiles = optFilename
	} else {
		_, path, _, _ := runtime.Caller(1)
		optFiles = append(optFiles, filepath.Base(path))
	}
	AppMigrations.Register(up, down, optFiles...)
}

func init() {
	AppMigrations.Register(func(db dbx.Builder) error {
		_, tablesErr := db.NewQuery(`
			CREATE TABLE {{_admins}} (
				[[id]]              TEXT PRIMARY KEY NOT NULL,
				[[avatar]]          INTEGER DEFAULT 0 NOT NULL,
				[[name]]          	TEXT DEFAULT "" NOT NULL,
				[[email]]           TEXT UNIQUE NOT NULL,
				[[tokenKey]]        TEXT UNIQUE NOT NULL,
				[[passwordHash]]    TEXT NOT NULL,
				[[lastResetSentAt]] TEXT DEFAULT "" NOT NULL,
				[[created]]         TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
				[[updated]]         TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL
			);

			CREATE TABLE {{_collections}} (
				[[id]]         TEXT PRIMARY KEY NOT NULL,
				[[system]]     BOOLEAN DEFAULT FALSE NOT NULL,
				[[type]]       TEXT DEFAULT "base" NOT NULL,
				[[name]]       TEXT UNIQUE NOT NULL,
				[[label]]      TEXT DEFAULT "" NOT NULL,
				[[schema]]     JSON DEFAULT "[]" NOT NULL,
				[[indexes]]    JSON DEFAULT "[]" NOT NULL,
				[[listRule]]   TEXT DEFAULT NULL,
				[[viewRule]]   TEXT DEFAULT NULL,
				[[createRule]] TEXT DEFAULT NULL,
				[[updateRule]] TEXT DEFAULT NULL,
				[[deleteRule]] TEXT DEFAULT NULL,
				[[options]]    JSON DEFAULT "{}" NOT NULL,
				[[created]]    TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
				[[updated]]    TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL
			);

			CREATE TABLE {{_params}} (
				[[id]]      TEXT PRIMARY KEY NOT NULL,
				[[key]]     TEXT UNIQUE NOT NULL,
				[[value]]   JSON DEFAULT NULL,
				[[created]] TEXT DEFAULT "" NOT NULL,
				[[updated]] TEXT DEFAULT "" NOT NULL
			);

			CREATE TABLE {{_externalAuths}} (
				[[id]]           TEXT PRIMARY KEY NOT NULL,
				[[collectionId]] TEXT NOT NULL,
				[[recordId]]     TEXT NOT NULL,
				[[provider]]     TEXT NOT NULL,
				[[providerId]]   TEXT NOT NULL,
				[[created]]      TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
				[[updated]]      TEXT DEFAULT (strftime('%Y-%m-%d %H:%M:%fZ')) NOT NULL,
				---
				FOREIGN KEY ([[collectionId]]) REFERENCES {{_collections}} ([[id]]) ON UPDATE CASCADE ON DELETE CASCADE
			);

			CREATE UNIQUE INDEX _externalAuths_record_provider_idx on {{_externalAuths}} ([[collectionId]], [[recordId]], [[provider]]);
			CREATE UNIQUE INDEX _externalAuths_collection_provider_idx on {{_externalAuths}} ([[collectionId]], [[provider]], [[providerId]]);
		`).Execute()
		if tablesErr != nil {
			return tablesErr
		}

		dao := daos.New(db)

		userCollectionName := "users"
		getCollectionName := func(name string, auth bool) string {
			prefix := ""
			if auth {
				prefix = "auth_"
			}
			return fmt.Sprintf("_pb_%s_%s", name, prefix)
		}
		newCollection := func(name string, auth bool, fields ...*schema.SchemaField) (*models.Collection, error) {
			// inserts a new system collection
			// -----------------------------------------------------------
			col := &models.Collection{}
			col.MarkAsNew()
			col.System = true
			rules := fmt.Sprintf("@request.auth.id != '' && @request.auth.collectionName = '%s'", userCollectionName)
			colType := models.CollectionTypeBase
			var options any
			options = models.CollectionBaseOptions{}
			if auth {
				colType = models.CollectionTypeAuth
				rules = "id = @request.auth.id"
				options = models.CollectionAuthOptions{
					ManageRule:        nil,
					AllowOAuth2Auth:   true,
					AllowUsernameAuth: true,
					AllowEmailAuth:    true,
					MinPasswordLength: 10,
					RequireEmail:      false,
					OnlyVerified:      false,
				}
			}
			col.Id = getCollectionName(name, auth)
			col.Name = name
			col.Type = colType
			col.ListRule = types.Pointer(rules)
			col.ViewRule = types.Pointer(rules)
			col.CreateRule = types.Pointer("")
			col.UpdateRule = types.Pointer(rules)
			col.DeleteRule = types.Pointer(rules)

			// set auth options
			col.SetOptions(options)

			// set optional default fields
			col.Schema = schema.NewSchema(fields...)
			if err := dao.SaveCollection(col); err != nil {
				return nil, err
			}
			return col, nil
		}

		// inserts the system users collection
		// -----------------------------------------------------------
		droitsCollection, err := newCollection(
			"droits",
			false,
			&schema.SchemaField{
				Id:      "droits_key",
				Type:    schema.FieldTypeText,
				Unique:  true,
				Name:    "key",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "droits_value",
				Type:    schema.FieldTypeText,
				Name:    "value",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "droits_group",
				Type:    schema.FieldTypeText,
				Name:    "group",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "droits_desc",
				Type:    schema.FieldTypeText,
				Name:    "desc",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:   "droits_parent",
				Type: schema.FieldTypeRelation,
				Name: "parent",
				Options: &schema.RelationOptions{
					CollectionId:  getCollectionName("droits", false),
					CascadeDelete: true,
					DisplayFields: []string{"value"},
				},
			},
		)

		if err != nil {
			return err
		}
		organisationCollection, err := newCollection(
			"organisations",
			false,
			&schema.SchemaField{
				Id:       "organisations_name",
				Type:     schema.FieldTypeText,
				Required: true,
				Unique:   true,
				Name:     "name",
				Options:  &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "organisations_address",
				Type:    schema.FieldTypeText,
				Name:    "address",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "organisations_country",
				Type:    schema.FieldTypeText,
				Name:    "country",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:      "organisations_email",
				Type:    schema.FieldTypeEmail,
				Name:    "email",
				Options: &schema.TextOptions{},
			},
		)
		if err != nil {
			return err
		}
		organisationDroitsCollection, err := newCollection(
			"droits_organisations",
			false,
			&schema.SchemaField{
				Id:       "droits_organisations_droit",
				Type:     schema.FieldTypeRelation,
				Name:     "droit",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  droitsCollection.Id,
					CascadeDelete: true,
					DisplayFields: []string{"value"},
				},
			},
			&schema.SchemaField{
				Id:       "droits_organisations_organisation",
				Type:     schema.FieldTypeRelation,
				Name:     "organisation",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  getCollectionName("organisations", false),
					CascadeDelete: true,
					DisplayFields: []string{"name"},
				},
			},

			&schema.SchemaField{
				Id:       "droits_organisations_active",
				Type:     schema.FieldTypeBool,
				Name:     "active",
				Required: true,
				Options:  &schema.BoolOptions{},
			},
		)
		if err != nil {
			return err
		}
		usersCollection, err := newCollection(
			userCollectionName,
			true,
			&schema.SchemaField{
				Id:      "users_name",
				Type:    schema.FieldTypeText,
				Name:    "name",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:   "users_avatar",
				Type: schema.FieldTypeFile,
				Name: "avatar",
				Options: &schema.FileOptions{
					MaxSelect: 1,
					MaxSize:   5242880,
					MimeTypes: []string{
						"image/jpeg",
						"image/png",
						"image/svg+xml",
						"image/gif",
						"image/webp",
					},
				},
			},
			&schema.SchemaField{
				Id:       "users_organisation",
				Type:     schema.FieldTypeRelation,
				Name:     "organisation",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  organisationCollection.Id,
					CascadeDelete: true,
					DisplayFields: []string{"name"},
				},
			},
		)

		if err != nil {
			return err
		}
		_, err = newCollection(
			"droits_users",
			false,
			&schema.SchemaField{
				Id:       "droits_users_droit",
				Type:     schema.FieldTypeRelation,
				Name:     "droit",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  organisationDroitsCollection.Id,
					CascadeDelete: true,
					DisplayFields: []string{"value"},
				},
			},
			&schema.SchemaField{
				Id:       "droits_users_user",
				Type:     schema.FieldTypeRelation,
				Name:     "user",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  usersCollection.Id,
					CascadeDelete: true,
					DisplayFields: []string{"name"},
				},
			},
			&schema.SchemaField{
				Id:       "droits_users_active",
				Type:     schema.FieldTypeBool,
				Name:     "active",
				Required: true,
				Options:  &schema.BoolOptions{},
			},
		)
		if err != nil {
			return err
		}
		// usersDroitsCollection.CreateRule = types.Pointer(fmt.Sprintf("@request.auth.id != '' && @request.auth.collectionName = '%s' && droit.organisation ?= @request.auth.organisation", userCollectionName))
		// if err := dao.SaveCollection(usersDroitsCollection); err != nil {
		// 	return err
		// }
		// CREATE THE DEFAULT LANGUAGE
		record := models.NewRecord(organisationCollection)
		record.Set("name", "Acme")
		if err := dao.SaveRecord(record); err != nil {
			return err
		}
		// inserts default settings
		// -----------------------------------------------------------
		defaultSettings := settings.New(nil)
		defaultSettings.Users.DefaultOrganisation = record.Id
		if err := dao.SaveSettings(defaultSettings); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		tables := []string{
			"users",
			"organisations",
			"droits",
			"droits_organisations",
			"droits_users",
			"_externalAuths",
			"_params",
			"_collections",
			"_admins",
		}

		for _, name := range tables {
			if _, err := db.DropTable(name).Execute(); err != nil {
				return err
			}
		}

		return nil
	})
}
