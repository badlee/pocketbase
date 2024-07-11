package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/models/schema"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() {
	AppMigrations.Register(func(db dbx.Builder) error {
		dao := daos.New(db)

		// inserts the system _languages collection
		// -----------------------------------------------------------
		codeLength := 2
		language := &models.Collection{}
		language.MarkAsNew()
		language.System = true
		language.Id = "_pb_languages_"
		language.Name = "languages"
		language.Type = models.CollectionTypeBase
		language.ListRule = types.Pointer("")
		language.ViewRule = types.Pointer("")
		language.CreateRule = types.Pointer("")
		language.UpdateRule = types.Pointer("")
		language.DeleteRule = types.Pointer("")

		// set optional default fields
		language.Schema = schema.NewSchema(
			&schema.SchemaField{
				Id:       "languages_code",
				Type:     schema.FieldTypeText,
				Required: true,
				Name:     "code",
				Options: &schema.TextOptions{
					Max:     &codeLength,
					Min:     &codeLength,
					Pattern: "^[a-ZA-Z]{2}$",
				},
			},
			&schema.SchemaField{
				Id:       "languages_country",
				Type:     schema.FieldTypeText,
				Required: true,
				Name:     "country",
				Options: &schema.TextOptions{
					Max:     &codeLength,
					Min:     &codeLength,
					Pattern: "^[a-ZA-Z]{2}$",
				},
			},
			&schema.SchemaField{
				Id:      "languages_name",
				Type:    schema.FieldTypeText,
				Name:    "name",
				Options: &schema.TextOptions{},
			},
		)
		if err := dao.SaveCollection(language); err != nil {
			return err
		}
		// inserts the system _translations collection
		// -----------------------------------------------------------
		translation := &models.Collection{}
		translation.MarkAsNew()
		translation.System = true
		translation.Id = "_pb_translations_"
		translation.Name = "translations"
		translation.Type = models.CollectionTypeBase
		translation.ListRule = types.Pointer("")
		translation.ViewRule = types.Pointer("")
		translation.CreateRule = types.Pointer("")
		translation.UpdateRule = types.Pointer("")
		translation.DeleteRule = types.Pointer("")

		// set optional default fields
		translation.Schema = schema.NewSchema(
			&schema.SchemaField{
				Id:   "translations_key",
				Type: schema.FieldTypeText,
				Name: "key",
				Options: &schema.TextOptions{
					Max: &codeLength,
					Min: &codeLength,
				},
			},
			&schema.SchemaField{
				Id:   "translations_value",
				Type: schema.FieldTypeText,
				Name: "value",

				Options: &schema.TextOptions{
					Max: &codeLength,
					Min: &codeLength,
				},
			},
			&schema.SchemaField{
				Id:       "translations_language",
				Type:     schema.FieldTypeRelation,
				Name:     "language",
				Required: true,
				Options: &schema.RelationOptions{
					CollectionId:  language.Id,
					CascadeDelete: true,
					DisplayFields: []string{"code", "country"},
				},
			},
		)
		if err := dao.SaveCollection(translation); err != nil {
			return err
		}
		// CREATE THE DEFAULT LANGUAGE
		record := models.NewRecord(language)
		record.Set("code", "_DEFAULT")
		record.Set("country", "_DEFAULT")
		record.Set("name", "Default")
		if err := dao.SaveRecord(record); err != nil {
			return err
		}

		// CREATE THE DEFAULT LANGUAGE
		record = models.NewRecord(translation)
		record.Set("language", record)
		record.Set("key", "Default")
		record.Set("value", "Défaut")
		if err := dao.SaveRecord(record); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		tables := []string{
			"languages",
			"translations",
		}

		for _, name := range tables {
			if _, err := db.DropTable(name).Execute(); err != nil {
				return err
			}
		}

		return nil
	})
}
