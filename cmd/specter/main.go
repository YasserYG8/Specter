package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/ui"
	"github.com/YasserYG8/Specter/internal/util"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"

	// CLI flags
	flagLevel    string
	flagGrep     string
	flagStderr   bool
	flagJSON     bool
	flagRaw      bool
	flagSource   string
	flagTree     bool
	flagNoColor  bool
	flagPIDs     []int
	flagDuration time.Duration

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
		Use:   "specter [port|container|project]...",
		Short: "Live stdout/stderr stream from any server by port, PID, or container",
		Long: `Specter attaches to any active server process by listening port, container name, project
name, or PID, and streams its stdout and stderr directly to your terminal.

A bare number is always a port. To attach to a process by its PID, use --pid.

Give several targets (up to 8) to follow them together. Every line then starts with the name of its target,
for example ':3000 | GET /api 200'. --pid can be repeated and mixed with other targets.

Running 'specter' without arguments launches an interactive discovery picker.

While Specter streams in a terminal, press Esc to detach and pick another service, or Ctrl+C to quit.
Typed text is not echoed into the logs; Enter adds an empty line. If Specter is killed with SIGKILL, the
terminal can stay without echo: run 'stty sane' to fix it.

Zero configuration, zero wrappers, read-only.`,
		Example: `  # Launch interactive picker to select an active service or container
  specter

  # Attach to an active HTTP/API server by listening port
  specter 3000
  specter 8080

  # Attach directly to a Docker container by name or ID
  specter phpmyadmin
  specter mysql_db

  # Attach directly to a known process ID (PID); a bare number is always a port
  specter --pid 14280
  specter -p 14280

  # Follow several targets at once: every line starts with its target (':3000 | ...', 'web | ...')
  specter 3000 4000
  specter 3000 web --pid 14280
  specter --pid 14280 --pid 14281

  # Filter logs by minimum severity level (debug, info, warn, error)
  specter 3000 --level warn
  specter 3000 --level error

  # Filter logs matching text or regex pattern
  specter 3000 --grep "GET /api"
  specter 3000 --grep "auth|failed|token"

  # Stream only stderr (e.g. unhandled exceptions and crashes)
  specter 3000 --stderr

  # Direct workers (Node cluster, Gunicorn, Celery) are always traced; --tree adds deeper descendants
  specter 8000 --tree

  # Force a specific log source capture engine
  specter 3000 --source strace
  specter 3000 --source file

  # Stream raw JSON lines or disable ANSI color formatting
  specter 3000 --json
  specter 3000 --no-color`,
		Version:      buildVersionString(),
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs, // the limit of 8 targets is checked in RunE, where the message can say so
		ValidArgsFunction: func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// Every position takes a target, so every position gets completions. A target that is already on the
			// line is not offered again.
			given := make(map[string]bool, len(args))
			for _, a := range args {
				given[strings.ToLower(a)] = true
			}

			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			ports, err := portDiscoveryFunc(ctx)
			if err != nil || len(ports) == 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			var completions []string
			seenContainers := make(map[string]bool)
			seenProjects := make(map[string]bool)

			for _, p := range ports {
				if p.Port > 0 {
					portStr := strconv.Itoa(p.Port)
					if !given[portStr] && (toComplete == "" || strings.HasPrefix(portStr, toComplete)) {
						cleanProc := strings.Map(func(r rune) rune {
							if r == '\t' || r == '\n' || r == '\r' {
								return ' '
							}
							return r
						}, p.ProcessName)
						cleanProc = strings.TrimSpace(cleanProc)

						var desc string
						if p.ProjectName != "" && p.ContainerName != "" {
							desc = fmt.Sprintf("docker:%s (%s)", p.ProjectName, p.Image)
						} else if p.ProjectName != "" {
							desc = fmt.Sprintf("%s (%s)", p.ProjectName, cleanProc)
						} else if p.ContainerName != "" {
							if p.Image != "" {
								desc = fmt.Sprintf("docker:%s (%s)", p.ContainerName, p.Image)
							} else {
								desc = fmt.Sprintf("docker:%s", p.ContainerName)
							}
						} else if p.IsProtected {
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
				}

				if p.ContainerName != "" && !seenContainers[p.ContainerName] {
					seenContainers[p.ContainerName] = true
					if !given[strings.ToLower(p.ContainerName)] && (toComplete == "" || strings.HasPrefix(p.ContainerName, toComplete)) {
						desc := "docker container"
						if p.Image != "" {
							desc = fmt.Sprintf("docker container (%s)", p.Image)
						}
						completions = append(completions, fmt.Sprintf("%s\t%s", p.ContainerName, desc))
					}
				}

				if p.ProjectName != "" && !seenProjects[p.ProjectName] {
					seenProjects[p.ProjectName] = true
					if !given[strings.ToLower(p.ProjectName)] && (toComplete == "" || strings.HasPrefix(strings.ToLower(p.ProjectName), strings.ToLower(toComplete))) {
						desc := fmt.Sprintf("project on :%d", p.Port)
						completions = append(completions, fmt.Sprintf("%s\t%s", p.ProjectName, desc))
					}
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// strace gets a parent-death signal tied to the thread that starts it, so that thread must live as
			// long as Specter does. This goroutine does, and it is never unlocked. Every session of every pass is
			// started from it (see Session.Start).
			runtime.LockOSThread()

			if flagNoColor {
				color.NoColor = true
			}

			// Reject a --level that would be silently ignored, before anything is resolved or attached
			if err := validateLevel(flagLevel); err != nil {
				return err
			}

			// Reject --pid values that cannot be used, and too many targets, before anything is resolved or
			// attached
			if err := validatePIDFlag(flagPIDs); err != nil {
				return err
			}
			if err := validateTargetCount(args, flagPIDs); err != nil {
				return err
			}

			// Reject a negative --duration the same way
			if err := validateDuration(flagDuration); err != nil {
				return err
			}

			// Verify Linux platform
			if runtime.GOOS != "linux" {
				targetRemediation := "3000"
				if len(args) > 0 {
					targetRemediation = args[0]
				} else if len(flagPIDs) == 1 {
					targetRemediation = fmt.Sprintf("--pid %d", flagPIDs[0])
				}
				return unsupportedPlatformError(runtime.GOOS, targetRemediation)
			}

			// Context handling SIGINT / SIGTERM. Ctrl+C ends the whole run; Esc and --duration end one pass.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			res := resolver.NewDefaultResolver(nil, "")
			pe := &passEnv{
				cfg: runConfig{
					Source:        flagSource,
					Tree:          flagTree,
					Grep:          flagGrep,
					Duration:      flagDuration,
					NoOutputDelay: noOutputNoticeDelay,
				},
				filterOpts: pipeline.FilterOptions{
					MinLevel:   pipeline.ParseSeverity(flagLevel),
					Grep:       flagGrep,
					OnlyStderr: flagStderr,
				},
				rendererOpts: pipeline.RendererOptions{
					NoColor:   flagNoColor,
					Raw:       flagRaw,
					ForceJSON: flagJSON,
				},
				useColor:    !flagNoColor && !color.NoColor,
				out:         os.Stdout,
				errOut:      os.Stderr,
				stdoutIsTTY: term.IsTerminal(int(os.Stdout.Fd())),
				// The picker draws on stderr. After Esc it gets a clean screen, not the logs of the last stream.
				clearBeforePicker: term.IsTerminal(int(os.Stderr.Fd())),
				newKeys: func() keySource {
					// Keys are read only when Specter has the terminal; otherwise nothing about the terminal changes
					if ui.CanReadKeys(os.Stdin, os.Stderr) {
						return ui.NewKeyReader(os.Stdin)
					}
					return nil
				},
			}

			// The first pass follows the command line, or the picker when no target was given. After Esc, every
			// next pass comes from the picker.
			specs := buildTargetSpecs(args, flagPIDs)
			first := func(ctx context.Context) passResult {
				var resolved []resolvedTarget
				if len(specs) == 0 {
					t, err := pickTarget(ctx, res)
					if err != nil {
						return passResult{err: err}
					}
					if t == nil {
						return passResult{} // the picker was closed without a choice
					}
					resolved = []resolvedTarget{{Label: "target", Target: t}}
				} else {
					var err error
					resolved, err = resolveSpecs(ctx, res, specs)
					if err != nil {
						// The empty-port diagnostic lists the active ports, so it is printed instead of the bare error
						var noProcErr *resolver.NoProcessOnPortError
						if errors.As(err, &noProcErr) {
							fmt.Fprint(os.Stderr, noProcErr.FormatDiagnostic(pe.useColor))
							return passResult{err: err, silenceErr: true}
						}
						return passResult{err: err}
					}
				}
				return runPass(ctx, resolved, pe)
			}
			next := func(ctx context.Context) (passResult, bool) {
				return pickPass(ctx, func(ctx context.Context) (*source.Target, error) { return pickTarget(ctx, res) }, pe)
			}

			result := runLoop(ctx, first, next)
			if result.err != nil {
				if result.silenceErr {
					cmd.SilenceErrors = true
				}
				return result.err
			}

			// Each session said how it ended. The exit status is 1 when a target of the last pass ended on its own.
			if result.anyEnded {
				cmd.SilenceErrors = true
				return errTargetEnded
			}
			return nil
		},
	}

	// cobra's own message for --pid abc ("invalid argument ... strconv.ParseInt ...") does not say what to do
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "--duration") {
			return durationFlagError()
		}
		if msg := err.Error(); strings.Contains(msg, "--pid") || strings.Contains(msg, "'p' in -p") {
			return &util.DiagnosticError{
				Err:         util.ErrInvalidFlag,
				Title:       "Invalid --pid value",
				Explanation: "The PID must be a positive whole number.",
				Remediation: "Find the PID with: ps aux | grep <name>    or: ss -ltnp",
			}
		}
		return err
	})

	flags := cmd.Flags()
	flags.StringVarP(&flagLevel, "level", "l", "", "minimum log level (debug, info, warn, error)")
	flags.StringVarP(&flagGrep, "grep", "g", "", "filter output matching text or regex pattern")
	flags.BoolVar(&flagStderr, "stderr", false, "stream only standard error (fd 2)")
	flags.BoolVar(&flagJSON, "json", false, "force raw JSON lines without pretty-printing")
	flags.BoolVar(&flagRaw, "raw", false, "bypass parsing and ANSI color formatting")
	flags.StringVarP(&flagSource, "source", "s", "", "force source selection (docker, file, journal, ebpf, strace)")
	flags.BoolVarP(&flagTree, "tree", "t", false, "also trace deeper descendants of the target; direct child workers are always traced")
	flags.IntSliceVarP(&flagPIDs, "pid", "p", nil, "attach to the process with this PID, for any PID (a bare number is always a port)")
	flags.DurationVar(&flagDuration, "duration", 0, "detach after this time, for example 30s or 2m (default: no limit)")
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
			"ebpf\tKernel capture with eBPF: no slowdown (needs root and Linux 5.8+)",
			"strace\tDirect syscall snooping via ptrace (slows the target)",
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

// noOutputNoticeDelay is how long Specter waits for the first entry before saying nothing was captured.
var noOutputNoticeDelay = 10 * time.Second

// printNotice writes a status message to w, which is stderr while attached so piped log output stays clean.
func printNotice(w io.Writer, msg string) {
	fmt.Fprintf(w, "%s %s\n", color.YellowString("[specter:notice]"), msg)
}

// wireNotices lets sources that can explain missing output print their notices to w (stderr, with the target label
// in front when several targets run).
func wireNotices(src source.Source, w io.Writer) {
	if n, ok := src.(interface{ SetNotifier(func(string)) }); ok {
		n.SetNotifier(func(msg string) { printNotice(w, msg) })
	}
}

// startNoOutputNotice prints one notice to w if sawEntry has not been called after delay. The returned stop
// function cancels a notice that has not fired yet.
func startNoOutputNotice(ctx context.Context, delay time.Duration, w io.Writer) (sawEntry func(), stop func()) {
	var seen atomic.Bool
	timer := time.AfterFunc(delay, func() {
		if !seen.Load() && ctx.Err() == nil {
			printNotice(w, "Nothing captured yet. The target may be idle, buffering its output, or logging somewhere else.")
		}
	})
	return func() { seen.Store(true) }, func() { timer.Stop() }
}

// validateLevel rejects --level values that ParseRankedSeverity would silently treat as "no filter".
func validateLevel(raw string) error {
	if strings.TrimSpace(raw) == "" || pipeline.ParseRankedSeverity(raw) != pipeline.SeverityNone {
		return nil
	}
	return &util.DiagnosticError{
		Err:         util.ErrInvalidFlag,
		Title:       fmt.Sprintf("Unknown --level %q", raw),
		Explanation: "Specter would show every line instead of filtering, so it refuses the value.",
		Remediation: "Use one of: debug, info, warn, error (aliases such as warning, err, fatal and trace also work)",
	}
}
