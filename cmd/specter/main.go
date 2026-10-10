package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/specter-cli/specter/internal/pipeline"
	"github.com/specter-cli/specter/internal/resolver"
	"github.com/specter-cli/specter/internal/source"
	"github.com/specter-cli/specter/internal/source/strace"
	"github.com/specter-cli/specter/internal/util"
)

var (
	version = "0.1.0-dev"

	// CLI flags
	flagLevel   string
	flagGrep    string
	flagStderr  bool
	flagJSON    bool
	flagRaw     bool
	flagSource  string
	flagTree    bool
	flagNoColor bool
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "specter <port|pid>",
		Short: "Live stdout/stderr stream from any server by port or PID",
		Long: `Specter attaches to any active server process by listening port or PID
and streams its stdout and stderr directly to your terminal.

Zero configuration, zero wrappers, read-only.`,
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagNoColor {
				color.NoColor = true
			}

			targetArg := args[0]
			val, err := strconv.Atoi(targetArg)
			if err != nil || val <= 0 {
				return fmt.Errorf("argument must be a positive port or PID, got: %q", targetArg)
			}

			// Verify Linux platform
			if runtime.GOOS != "linux" {
				return &util.DiagnosticError{
					Err:         util.ErrUnsupportedPlatform,
					Title:       fmt.Sprintf("Specter requires Linux kernel interfaces (running on %s)", runtime.GOOS),
					Explanation: "Low-level /proc inspection and ptrace syscall snooping require a Linux environment.",
					Remediation: fmt.Sprintf("On Windows, run inside WSL2: wsl specter %d (or within a Linux container)", val),
				}
			}

			// Context handling SIGINT / SIGTERM
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Check ptrace permissions upfront
			if permErr := strace.CheckPtracePermissions(""); permErr != nil {
				fmt.Fprintf(os.Stderr, "%s %v\n", color.YellowString("[specter:warning]"), permErr)
			}

			// Resolve target (try as port first if <= 65535, then as PID)
			res := resolver.NewDefaultResolver(nil, "")
			var target *source.Target

			if val <= 65535 {
				t, portErr := res.ResolvePort(ctx, val)
				if portErr == nil {
					target = t
				} else if !strings.Contains(portErr.Error(), util.ErrNoProcessFound.Error()) {
					// Surface permission or tool errors immediately
					return portErr
				}
			}

			if target == nil {
				t, pidErr := res.ResolvePID(ctx, val)
				if pidErr != nil {
					return fmt.Errorf("could not find listening port or active process for: %d (%w)", val, pidErr)
				}
				target = t
			}

			// Notice if stdout and stderr are discarded to /dev/null
			if strings.Contains(target.Stdout, "/dev/null") && strings.Contains(target.Stderr, "/dev/null") {
				fmt.Fprintf(os.Stderr, "%s Target process has stdout/stderr redirected to /dev/null; no console logs are emitted.\n", color.YellowString("[specter:notice]"))
			}

			// Header notice
			headerTag := color.CyanString("[specter]")
			cmdSummary := strings.Join(target.Cmdline, " ")
			if len(cmdSummary) > 60 {
				cmdSummary = cmdSummary[:57] + "..."
			}
			if target.Port > 0 {
				fmt.Fprintf(os.Stderr, "%s Attaching to PID %d on port :%d (%s)\n", headerTag, target.PID, target.Port, cmdSummary)
			} else {
				fmt.Fprintf(os.Stderr, "%s Attaching to PID %d (%s)\n", headerTag, target.PID, cmdSummary)
			}

			// Start strace source
			straceSource := strace.NewSource()
			defer straceSource.Stop()

			chunks, errs := straceSource.Start(ctx, *target)

			// Line assembler
			assembler := pipeline.NewLineAssembler(pipeline.DefaultIdleTimeout)
			lines := assembler.Assemble(ctx, chunks)

			// Stream output to terminal
			for line := range lines {
				// Apply --stderr filter
				if flagStderr && line.FD != 2 {
					continue
				}

				// Apply --grep filter
				if flagGrep != "" && !strings.Contains(line.Text, flagGrep) {
					continue
				}

				// Output line
				if line.IsPartial {
					fmt.Print(line.Text)
				} else {
					fmt.Println(line.Text)
				}
			}

			// Check for source startup or runtime errors (draining without blocking)
			srcErr, ok := <-errs
			if ok && srcErr != nil && ctx.Err() == nil {
				return srcErr
			}

			// Check if target process terminated while being traced
			if status := straceSource.ProcessStatus(); status != "" && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "\n%s Target %s\n", headerTag, status)
				return nil
			}

			fmt.Fprintf(os.Stderr, "\n%s Detached from PID %d cleanly.\n", headerTag, target.PID)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&flagLevel, "level", "", "minimum log level (debug, info, warn, error)")
	flags.StringVar(&flagGrep, "grep", "", "filter output matching text or regex pattern")
	flags.BoolVar(&flagStderr, "stderr", false, "stream only standard error (fd 2)")
	flags.BoolVar(&flagJSON, "json", false, "force raw JSON lines without pretty-printing")
	flags.BoolVar(&flagRaw, "raw", false, "bypass parsing and ANSI color formatting")
	flags.StringVar(&flagSource, "source", "", "force source selection (docker, file, journal, strace)")
	flags.BoolVar(&flagTree, "tree", false, "follow and attach to child processes")
	flags.BoolVar(&flagNoColor, "no-color", false, "disable ANSI color output")

	return cmd
}

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
