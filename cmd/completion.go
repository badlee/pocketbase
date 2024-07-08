package cmd

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

func NewCompletionCommand(app core.App) *cobra.Command {
	defName := "completion"
	defUsage := "Prints shell completion scripts"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	return &cobra.Command{
		Use:       "completion [shell]",
		Short:     defUsage,
		ValidArgs: []string{"bash", "sh", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				_ = cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				_ = cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				_ = cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				_ = cmd.Root().GenPowerShellCompletion(cmd.OutOrStdout())
			}
			return nil
		},
	}
}
