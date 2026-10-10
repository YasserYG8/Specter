package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/specter-cli/specter/internal/pipeline"
	"github.com/specter-cli/specter/internal/resolver"
	"github.com/specter-cli/specter/internal/source"
	"github.com/specter-cli/specter/internal/source/selector"
	"github.com/specter-cli/specter/internal/source/strace"
	"github.com/specter-cli/specter/internal/util"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"

	// CLI flags
	flagLevel   string
	flagGrep    string
	flagStderr  bool
	flagJSON    bool
	flagRaw     bool
	flagSource  string
	flagTree    bool
	flagNoColor bool

	// portDiscoveryFunc discovers active listening TCP ports for autocompletion.
	portDiscoveryFunc = func(ctx context.Context) ([]resolver.ListeningPort, error) {
		res := resolver.NewPortResolver(nil)
		return res.DiscoverListeningPorts(ctx)
	}
)

// buildVersionString formats complete build and runtime provenance.
func buildVersionString() string {
	return fmt.Sprintf("%s (commit: %s, built at: %s by %s, %s %s/%s)",
		version, commit, date, builtBy, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "specter <port|pid>",
		Short: "Live stdout/stderr stream from any server by port or PID",
		Long: `Specter attaches to any active server process by listening port or PID
and streams its stdout and stderr directly to your terminal.

Zero configuration, zero wrappers, read-only.`,
		Example: `  # Attach to an active HTTP/API server by listening port
  specter 3000
  specter 8080

  # Attach directly to a known process ID (PID)
  specter 14280

  # Filter logs by minimum severity level (debug, info, warn, error)
  specter 3000 --level warn
  specter 3000 --level error

  # Filter logs matching text or regex pattern
  specter 3000 --grep "GET /api"
  specter 3000 --grep "auth|failed|token"

  # Stream only stderr (e.g. unhandled exceptions and crashes)
  specter 3000 --stderr

  # Trace multi-process architectures (e.g. Node cluster, Gunicorn, Celery)
  specter 8000 --tree

  # Force a specific log source capture engine
  specter 3000 --source strace
  specter 3000 --source file

  # Stream raw JSON lines or disable ANSI color formatting
  specter 3000 --json
  specter 3000 --no-color`,
		Version:      buildVersionString(),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		ValidArgsFunction: func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			ports, err := portDiscoveryFunc(ctx)
			if err != nil || len(ports) == 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			var completions []string
			for _, p := range ports {
				portStr := strconv.Itoa(p.Port)
				if toComplete != "" && !strings.HasPrefix(portStr, toComplete) {
					continue
				}

				cleanProc := strings.Map(func(r rune) rune {
					if r == '\t' || r == '\n' || r == '\r' {
						return ' '
					}
					return r
				}, p.ProcessName)
				cleanProc = strings.TrimSpace(cleanProc)

				var desc string
				if p.IsProtected {
					desc = "protected process (run with sudo)"
				} else if cleanProc != "" && p.PID > 0 {
					desc = fmt.Sprintf("%s (PID %d)", cleanProc, p.PID)
				} else if cleanProc != "" {
					desc = cleanProc
				} else if p.PID > 0 {
					desc = fmt.Sprintf("PID %d", p.PID)
				} else {
					desc = "listening port"
				}

				completions = append(completions, fmt.Sprintf("%s\t%s", portStr, desc))
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
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

			// Resolve target (try as port first if <= 65535, then as PID)
			res := resolver.NewDefaultResolver(nil, "")
			var target *source.Target
			var savedPortErr error

			if val <= 65535 {
				t, portErr := res.ResolvePort(ctx, val)
				if portErr == nil {
					target = t
				} else if !strings.Contains(portErr.Error(), util.ErrNoProcessFound.Error()) && !errors.Is(portErr, util.ErrNoProcessFound) {
					// Surface permission or tool errors immediately
					return portErr
				} else {
					savedPortErr = portErr
				}
			}

			if target == nil {
				t, pidErr := res.ResolvePID(ctx, val)
				if pidErr != nil {
					var noProcErr *resolver.NoProcessOnPortError
					if savedPortErr != nil && errors.As(savedPortErr, &noProcErr) {
						fmt.Fprint(os.Stderr, noProcErr.FormatDiagnostic(!flagNoColor && !color.NoColor))
						cmd.SilenceErrors = true
						return noProcErr
					}
					return fmt.Errorf("could not find listening port or active process for: %d (%w)", val, pidErr)
				}
				target = t
			}

			// Notice if stdout and stderr are discarded to /dev/null
			if strings.Contains(target.Stdout, "/dev/null") && strings.Contains(target.Stderr, "/dev/null") {
				fmt.Fprintf(os.Stderr, "%s Target process has stdout/stderr redirected to /dev/null; no console logs are emitted.\n", color.YellowString("[specter:notice]"))
			}

			// Select capture source
			sourceSelector := selector.NewSelector(flagSource, flagTree)
			src, reason, selectErr := sourceSelector.Select(target)
			if selectErr != nil {
				return selectErr
			}
			defer src.Stop()

			// Check ptrace permissions upfront if strace source is selected
			if src.Name() == "strace" {
				if permErr := strace.CheckPtracePermissions(""); permErr != nil {
					fmt.Fprintf(os.Stderr, "%s %v\n", color.YellowString("[specter:warning]"), permErr)
				}
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
			fmt.Fprintf(os.Stderr, "%s Source: %s (%s)\n", headerTag, color.GreenString(src.Name()), reason)

			chunks, errs := src.Start(ctx, *target)

			// Fallback: If an auto-detected non-strace source fails immediately to start, fall back to strace
			if flagSource == "" && src.Name() != "strace" {
				select {
				case err, ok := <-errs:
					if ok && err != nil {
						fmt.Fprintf(os.Stderr, "%s Auto-detected source '%s' unavailable (%v); falling back to strace\n",
							headerTag, src.Name(), err)
						src = strace.NewSource(strace.Options{FollowTree: flagTree})
						chunks, errs = src.Start(ctx, *target)
					}
				default:
					// Started normally
				}
			}

			// 1. Line assembler
			assembler := pipeline.NewLineAssembler(pipeline.DefaultIdleTimeout)
			lines := assembler.Assemble(ctx, chunks)

			// 2. Multi-line stack trace grouper
			grouper := pipeline.NewStackTraceGrouper(pipeline.DefaultTraceIdleTimeout)
			entries := grouper.Group(ctx, lines)

			// 3. Filter configuration
			minSeverity := pipeline.ParseSeverity(flagLevel)
			filter := pipeline.NewFilter(pipeline.FilterOptions{
				MinLevel:   minSeverity,
				Grep:       flagGrep,
				OnlyStderr: flagStderr,
			})

			// 4. Renderer configuration
			renderer := pipeline.NewRenderer(pipeline.RendererOptions{
				NoColor:   flagNoColor,
				Raw:       flagRaw,
				ForceJSON: flagJSON,
			})

			// 5. Stream styled entries to terminal
			for entry := range entries {
				if !filter.Matches(&entry) {
					continue
				}

				rendered := renderer.Render(&entry)
				if entry.IsPartial {
					fmt.Print(rendered)
				} else {
					fmt.Println(rendered)
				}
			}

			// Check for source startup or runtime errors (draining without blocking)
			srcErr, ok := <-errs
			if ok && srcErr != nil && ctx.Err() == nil {
				return srcErr
			}

			// Check if target process terminated while being traced
			if statusProvider, ok := src.(interface{ ProcessStatus() string }); ok {
				if status := statusProvider.ProcessStatus(); status != "" && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "\n%s Target %s\n", headerTag, status)
					return nil
				}
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

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version and build metadata",
		Run: func(c *cobra.Command, args []string) {
			fmt.Printf("specter version %s\n", buildVersionString())
		},
	}
	cmd.AddCommand(versionCmd)
	cmd.AddCommand(newCompletionCmd(cmd))

	_ = cmd.RegisterFlagCompletionFunc("source", func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		sources := []string{
			"strace\tDirect syscall snooping via ptrace",
			"file\tLog file tailing (/proc/<pid>/fd)",
			"docker\tDocker container stdout/stderr logs",
			"journal\tSystemd journald service logs",
		}
		return sources, cobra.ShellCompDirectiveNoFileComp
	})

	_ = cmd.RegisterFlagCompletionFunc("level", func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		levels := []string{
			"debug\tShow all messages including debug traces",
			"info\tShow info, warn, and error messages",
			"warn\tShow warnings and error messages only",
			"error\tShow error messages only",
		}
		return levels, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
