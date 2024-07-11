package cmd

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/fatih/color"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
	"github.com/spf13/cobra"
)

// NewAdminCommand creates and returns new command for managing
// admin accounts (create, update, delete).
func NewAdminCommand(app core.App) *cobra.Command {
	defName := "admin"
	defUsage := "Manages admin accounts"

	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command := &cobra.Command{
		Use:       defName,
		Short:     defUsage,
		ValidArgs: []string{"list", "create", "update", "delete"},
	}
	command.AddCommand(adminListCommand(app))
	command.AddCommand(adminCreateCommand(app))
	command.AddCommand(adminUpdateCommand(app))
	command.AddCommand(adminDeleteCommand(app))

	return command
}

func adminCreateCommand(app core.App) *cobra.Command {
	defName := "admin-create"
	defUsage := "Creates a new admin account"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command := &cobra.Command{
		Use:          "create",
		Example:      "admin create test@example.com 1234567890",
		Short:        defUsage,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("missing email and password arguments")
			}

			if args[0] == "" || is.EmailFormat.Validate(args[0]) != nil {
				return errors.New("missing or invalid email address")
			}

			if len(args[1]) < 8 {
				return errors.New("the password must be at least 8 chars long")
			}

			admin := &models.Admin{}
			admin.Email = args[0]
			if err := admin.SetPassword(args[1]); err != nil {
				color.Red("%s", err)
				return nil
			}

			admin.Avatar = rand.IntN(9)

			if !app.Dao().HasTable(admin.TableName()) {
				return errors.New("migration are not initialized yet. Please run 'migrate up' and try again")
			}

			if err := app.Dao().SaveAdmin(admin); err != nil {
				return fmt.Errorf("failed to create new admin account: %v", err)
			}

			color.Green("Successfully created new admin %s!", admin.Email)
			return nil
		},
	}

	return command
}

func adminUpdateCommand(app core.App) *cobra.Command {
	defName := "admin-update"
	defUsage := "Changes the password of a single admin account"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}

	command := &cobra.Command{
		Use:          "update",
		Example:      "admin update test@example.com 1234567890",
		Short:        defUsage,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("missing email and password arguments")
			}

			if args[0] == "" || is.EmailFormat.Validate(args[0]) != nil {
				return errors.New("missing or invalid email address")
			}

			if len(args[1]) < 8 {
				return errors.New("the new password must be at least 8 chars long")
			}

			if !app.Dao().HasTable((&models.Admin{}).TableName()) {
				return errors.New("migration are not initialized yet. Please run 'migrate up' and try again")
			}

			admin, err := app.Dao().FindAdminByEmail(args[0])
			if err != nil {
				return fmt.Errorf("admin with email %s doesn't exist", args[0])
			}

			admin.SetPassword(args[1])

			if err := app.Dao().SaveAdmin(admin); err != nil {
				return fmt.Errorf("failed to change admin %s password: %v", admin.Email, err)
			}

			color.Green("Successfully changed admin %s password!", admin.Email)
			return nil
		},
	}

	return command
}

func adminDeleteCommand(app core.App) *cobra.Command {
	defName := "admin-delete"
	defUsage := "Deletes an existing admin account"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command := &cobra.Command{
		Use:          "delete",
		Example:      "admin delete test@example.com",
		Short:        defUsage,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" || is.EmailFormat.Validate(args[0]) != nil {
				return errors.New("invalid or missing email address")
			}

			if !app.Dao().HasTable((&models.Admin{}).TableName()) {
				return errors.New("migration are not initialized yet. Please run 'migrate up' and try again")
			}
			color.Red(args[0])
			admin, err := app.Dao().FindAdminByEmail(args[0])
			if err != nil {
				color.Yellow("Admin with email %s doesn't exist", args[0])
				return nil
			}

			if err := app.Dao().DeleteAdmin(admin); err != nil {
				return fmt.Errorf("failed to delete admin %s: %v", admin.Email, err)
			}

			color.Green("Successfully deleted admin %s!", admin.Email)
			return nil
		},
	}

	return command
}

func adminListCommand(app core.App) *cobra.Command {
	defName := "admin-create"
	defUsage := "List all existing admin accounts"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Example: "admin list",
		Short:   defUsage,
		// prevents printing the error log twice
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, args []string) error {
			if total, err := app.Dao().TotalAdmins(); err == nil {
				if total == 0 {
					color.Yellow("No administrators found")
					return nil
				}
				admins := []*models.Admin{}
				err = app.Dao().AdminQuery().Select("Email").All(&admins)
				if err != nil {
					color.Red("%s", err)
					return nil
				}
				t := table.NewWriter()

				t.AppendHeader(table.Row{"Email", "Created", "Updated", "Last Reset"})
				t.AppendSeparator()
				var index int = 0
				for _, admin := range admins {
					index = index + 1
					t.AppendRow([]interface{}{admin.Email, admin.Created.Time().Format("02/01/2006 15:04"), admin.Updated.Time().Format("02/01/2006 15:04"), admin.LastResetSentAt.Time().Format("02/01/2006 15:04")})
				}
				t.AppendFooter(table.Row{"Total", total, total, total}, table.RowConfig{
					AutoMerge: true, AutoMergeAlign: text.AlignRight,
				})
				// t.SetStyle(table.StyleColoredBlackOnCyanWhite)
				t.SetAutoIndex(true)
				t.SetStyle(table.StyleColoredBlackOnMagentaWhite)
				fmt.Print(t.Render() + "\n")

			} else {
				color.Red("%s", err)
			}
			return nil
		},
	}

	return command
}
