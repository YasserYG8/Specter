package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newCompletionCmd creates the "completion" subcommand for generating shell completions.
func newCompletionCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for Specter.

To load completions into your current shell session:

  # Bash
  source <(specter completion bash)

  # Zsh
  source <(specter completion zsh)
  # (or persist to completions directory: specter completion zsh > "${fpath[1]}/_specter")

  # Fish
  specter completion fish | source

  # PowerShell
  specter completion powershell | Out-String | Invoke-Expression
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompletion(rootCmd, cmd.OutOrStdout(), args[0])
		},
	}

	return cmd
}

func runCompletion(rootCmd *cobra.Command, out io.Writer, shell string) error {
	switch shell {
	case "bash":
		return rootCmd.GenBashCompletion(out)
	case "zsh":
		return rootCmd.GenZshCompletion(out)
	case "fish":
		return rootCmd.GenFishCompletion(out, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletionWithDesc(out)
	default:
		return fmt.Errorf("unsupported shell %q, choose one of: bash, zsh, fish, powershell", shell)
	}
}
