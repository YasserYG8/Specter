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

	"github.com/YasserYG8/Specter/internal/pipeline"
	"github.com/YasserYG8/Specter/internal/procwatch"
	"github.com/YasserYG8/Specter/internal/resolver"
	"github.com/YasserYG8/Specter/internal/source"
	"github.com/YasserYG8/Specter/internal/source/selector"
	"github.com/YasserYG8/Specter/internal/source/strace"
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
		Use:   "specter [port|container|project]",
		Short: "Live stdout/stderr stream from any server by port, PID, or container",
		Long: `Specter attaches to any active server process by listening port, container name, project
name, or PID, and streams its stdout and stderr directly to your terminal.

A bare number is always a port. To attach to a process by its PID, use --pid.

Running 'specter' without arguments launches an interactive discovery picker.
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
		Args:         cobra.RangeArgs(0, 1),
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
			seenContainers := make(map[string]bool)
			seenProjects := make(map[string]bool)

			for _, p := range ports {
				if p.Port > 0 {
					portStr := strconv.Itoa(p.Port)
					if toComplete == "" || strings.HasPrefix(portStr, toComplete) {
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
					if toComplete == "" || strings.HasPrefix(p.ContainerName, toComplete) {
						desc := "docker container"
						if p.Image != "" {
							desc = fmt.Sprintf("docker container (%s)", p.Image)
						}
						completions = append(completions, fmt.Sprintf("%s\t%s", p.ContainerName, desc))
					}
				}

				if p.ProjectName != "" && !seenProjects[p.ProjectName] {
					seenProjects[p.ProjectName] = true
					if toComplete == "" || strings.HasPrefix(strings.ToLower(p.ProjectName), strings.ToLower(toComplete)) {
						desc := fmt.Sprintf("project on :%d", p.Port)
						completions = append(completions, fmt.Sprintf("%s\t%s", p.ProjectName, desc))
					}
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// strace gets a parent-death signal tied to the thread that starts it, so that thread must live as
			// long as Specter does. This goroutine does, and it is never unlocked.
			runtime.LockOSThread()

			if flagNoColor {
				color.NoColor = true
			}

			// Reject a --level that would be silently ignored, before anything is resolved or attached
			if err := validateLevel(flagLevel); err != nil {
				return err
			}

			// Reject --pid values that cannot be used, before anything is resolved or attached
			if err := validatePIDFlag(flagPIDs, args); err != nil {
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

			// Context handling SIGINT / SIGTERM
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// --duration stops Specter through the same path as Ctrl+C: it cancels this context. The cause tells
			// the closing message which of the two happened.
			ctx, cancelRun := context.WithCancelCause(ctx)
			defer cancelRun(nil)

			res := resolver.NewDefaultResolver(nil, "")
			var target *source.Target

			if len(flagPIDs) == 1 {
				// An explicit PID, whatever its value: never checked as a port
				t, err := resolvePIDTarget(ctx, res, flagPIDs[0])
				if err != nil {
					return err
				}
				target = t
			} else if len(args) == 0 {
				services, err := res.DiscoverServices(ctx)
				if err != nil {
					return fmt.Errorf("failed to discover active services: %w", err)
				}

				selected, err := ui.SelectService(services, ui.PickerConfig{
					In:       os.Stdin,
					Out:      os.Stderr,
					UseColor: !flagNoColor && !color.NoColor,
				})
				if err != nil {
					if errors.Is(err, ui.ErrPickerAborted) {
						return nil
					}
					return err
				}
				if selected == nil {
					return nil
				}

				if selected.Kind == resolver.ServiceKindDocker {
					if selected.Port > 0 {
						t, err := res.ResolvePort(ctx, selected.Port)
						if err == nil {
							target = t
						}
					}
					if target == nil {
						targetName := selected.ContainerName
						if targetName == "" {
							targetName = selected.ContainerID
						}
						t, err := res.ResolveContainer(ctx, targetName)
						if err != nil {
							return err
						}
						target = t
					}
				} else {
					if selected.Port > 0 {
						t, err := res.ResolvePort(ctx, selected.Port)
						if err != nil {
							return err
						}
						target = t
					} else if selected.PID > 0 {
						t, err := res.ResolvePID(ctx, selected.PID)
						if err != nil {
							return err
						}
						target = t
					}
				}
			} else {
				targetArg := args[0]
				val, intErr := strconv.Atoi(targetArg)

				if intErr == nil && val > 0 {
					t, err := resolveNumericTarget(ctx, res, val)
					if err != nil {
						// The empty-port diagnostic lists the active ports, so it is printed instead of the bare error
						var noProcErr *resolver.NoProcessOnPortError
						if errors.As(err, &noProcErr) {
							fmt.Fprint(os.Stderr, noProcErr.FormatDiagnostic(!flagNoColor && !color.NoColor))
							cmd.SilenceErrors = true
						}
						return err
					}
					target = t
				} else {
					t, err := res.ResolveContainer(ctx, targetArg)
					if err != nil {
						// Check if targetArg matches an active project name
						if services, sErr := res.DiscoverServices(ctx); sErr == nil {
							for _, s := range services {
								if strings.EqualFold(s.ProjectName, targetArg) {
									if s.Kind == resolver.ServiceKindDocker {
										if s.Port > 0 {
											if dt, dErr := res.ResolvePort(ctx, s.Port); dErr == nil {
												target = dt
												break
											}
										}
										cName := s.ContainerName
										if cName == "" {
											cName = s.ContainerID
										}
										if dt, dErr := res.ResolveContainer(ctx, cName); dErr == nil {
											target = dt
											break
										}
									} else {
										if s.Port > 0 {
											if ht, hErr := res.ResolvePort(ctx, s.Port); hErr == nil {
												target = ht
												break
											}
										} else if s.PID > 0 {
											if ht, hErr := res.ResolvePID(ctx, s.PID); hErr == nil {
												target = ht
												break
											}
										}
									}
								}
							}
						}
						if target == nil {
							return fmt.Errorf("argument %q is not a valid port, container, or project (to attach by PID, use --pid): %w", targetArg, err)
						}
					} else {
						target = t
					}
				}
			}

			if target == nil {
				return errors.New("failed to resolve target")
			}

			// Discover pre-existing child processes (only for host PIDs)
			if target.PID > 0 {
				inspector := resolver.NewProcessInspector("")
				if flagTree {
					target.Children = inspector.FindChildProcessTree(target.PID, 3)
				} else {
					if kids, err := inspector.FindChildPIDs(target.PID); err == nil {
						target.Children = kids
					}
				}
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
				// Root can attach at ptrace_scope 1 and 2, so it gets no warning there
				if permErr := strace.CheckPtracePermissionsAs("", os.Geteuid()); permErr != nil {
					fmt.Fprintf(os.Stderr, "%s %v\n", color.YellowString("[specter:warning]"), permErr)
				}
			}

			// Header notice
			headerTag := color.CyanString("[specter]")
			cmdSummary := strings.Join(target.Cmdline, " ")
			if len(cmdSummary) > 60 {
				cmdSummary = cmdSummary[:57] + "..."
			}
			if target.Container != "" {
				if target.Port > 0 {
					fmt.Fprintf(os.Stderr, "%s Attaching to container %s on port :%d (%s)\n", headerTag, target.Container, target.Port, cmdSummary)
				} else {
					fmt.Fprintf(os.Stderr, "%s Attaching to container %s (%s)\n", headerTag, target.Container, cmdSummary)
				}
			} else if target.Port > 0 {
				fmt.Fprintf(os.Stderr, "%s Attaching to PID %d on port :%d (%s)\n", headerTag, target.PID, target.Port, cmdSummary)
			} else {
				fmt.Fprintf(os.Stderr, "%s Attaching to PID %d (%s)\n", headerTag, target.PID, cmdSummary)
			}
			if len(target.Children) > 0 {
				childStrList := make([]string, len(target.Children))
				for i, c := range target.Children {
					childStrList[i] = strconv.Itoa(c)
				}
				fmt.Fprintf(os.Stderr, "%s Discovered active child worker(s): PID %s\n", headerTag, strings.Join(childStrList, ", "))
			}
			fmt.Fprintf(os.Stderr, "%s Source: %s (%s)\n", headerTag, color.GreenString(src.Name()), reason)
			if msg := slowdownNoticeFor(src.Name()); msg != "" {
				printNotice(os.Stderr, msg)
			}

			// The source and the pipeline get their own contexts. The process watcher stops the source when the
			// target is gone and gives the pipeline a moment to print what was already read, while the user's
			// interrupt (ctx) stops both at once.
			srcCtx, cancelSrc := context.WithCancelCause(ctx)
			defer cancelSrc(nil)
			pipeCtx, cancelPipe := context.WithCancel(ctx)
			defer cancelPipe()

			wireNotices(src)
			chunks, errs := src.Start(srcCtx, *target)

			// Fallback: If an auto-detected non-strace source fails immediately to start, fall back to strace
			if flagSource == "" && src.Name() != "strace" {
				select {
				case err, ok := <-errs:
					if ok && err != nil {
						fmt.Fprintf(os.Stderr, "%s Auto-detected source '%s' unavailable (%v); falling back to strace\n",
							headerTag, src.Name(), err)
						src = strace.NewSource(strace.Options{FollowTree: flagTree})
						wireNotices(src)
						printNotice(os.Stderr, slowdownNotice) // the first source did not slow the target, this one does
						chunks, errs = src.Start(srcCtx, *target)
					}
				default:
					// Started normally
				}
			}

			// --duration: the time starts now, when the source has started
			if flagDuration > 0 {
				limit := time.AfterFunc(flagDuration, func() { cancelRun(errDurationElapsed) })
				defer limit.Stop()
			}

			// Watch the target process, for sources that cannot tell when it ends. strace usually does, with the
			// exit code, but the file source only tails a file and would wait forever.
			var watcherGone atomic.Bool
			if watchesPID(src, target) {
				gone := procwatch.Watch(pipeCtx, target.PID, procwatch.Options{})
				go func() {
					select {
					case <-gone:
						watcherGone.Store(true)
						cancelSrc(errTargetGone)
						select {
						case <-time.After(drainGrace):
							cancelPipe()
						case <-pipeCtx.Done():
						}
					case <-pipeCtx.Done():
					}
				}()
			}

			// 1. Line assembler
			assembler := pipeline.NewLineAssembler(pipeline.DefaultIdleTimeout)
			lines := assembler.Assemble(pipeCtx, chunks)

			// 2. Multi-line stack trace grouper
			grouper := pipeline.NewStackTraceGrouper(pipeline.DefaultTraceIdleTimeout)
			entries := grouper.Group(pipeCtx, lines)

			// 3. Filter configuration
			minSeverity := pipeline.ParseSeverity(flagLevel)
			filter := pipeline.NewFilter(pipeline.FilterOptions{
				MinLevel:   minSeverity,
				Grep:       flagGrep,
				OnlyStderr: flagStderr,
			})

			if err := filter.GrepError(); err != nil {
				printNotice(os.Stderr, fmt.Sprintf("--grep %q is not a valid regular expression (%v); matching it as literal text.", flagGrep, err))
			}

			// With --grep, hold each stack trace until it ends, so a match on any of its lines shows the whole
			// trace. Without --grep nothing is held and output stays immediate.
			if flagGrep != "" {
				entries = pipeline.NewTraceHolder(filter.MatchText, pipeline.DefaultTraceIdleTimeout, pipeline.MaxHeldTraceLines).Hold(pipeCtx, entries)
			}

			// 4. Renderer configuration
			renderer := pipeline.NewRenderer(pipeline.RendererOptions{
				NoColor:   flagNoColor,
				Raw:       flagRaw,
				ForceJSON: flagJSON,
			})

			// 5. Stream styled entries to terminal
			sawEntry, stopNoOutputNotice := startNoOutputNotice(pipeCtx, noOutputNoticeDelay, os.Stderr)
			defer stopNoOutputNotice()
			for entry := range entries {
				sawEntry() // counted before filters: output hidden by --grep or --level is not silence
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
			if ok && srcErr != nil && ctx.Err() == nil && !watcherGone.Load() {
				return srcErr
			}

			// Say how the run ended. "Detached cleanly" is only for the user's interrupt; when the target ended on
			// its own, Specter says how and exits with status 1.
			report := endReport{
				Interrupted: ctx.Err() != nil,
				PID:         target.PID,
				Container:   target.Container,
				WatcherGone: watcherGone.Load(),
			}
			if errors.Is(context.Cause(ctx), errDurationElapsed) {
				report.After = flagDuration
			}
			if es, ok := src.(interface {
				ExitStatus() (strace.ExitStatus, bool)
			}); ok {
				if status, known := es.ExitStatus(); known {
					report.Exit = &status
				}
			}
			if src.Name() == "docker" && !report.Interrupted {
				// docker logs -f ends when the container stops; ask once for the exit code
				report.DockerEnded = true
				if ce, ok := src.(interface {
					ContainerExit(context.Context) (int, bool)
				}); ok {
					if code, known := ce.ContainerExit(ctx); known {
						report.ContainerCode = &code
					}
				}
			}
			if reportEnd(os.Stderr, headerTag, report) != 0 {
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
	flags.StringVarP(&flagSource, "source", "s", "", "force source selection (docker, file, journal, strace)")
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

// noOutputNoticeDelay is how long Specter waits for the first entry before saying nothing was captured.
var noOutputNoticeDelay = 10 * time.Second

// printNotice writes a status message to w, which is stderr while attached so piped log output stays clean.
func printNotice(w io.Writer, msg string) {
	fmt.Fprintf(w, "%s %s\n", color.YellowString("[specter:notice]"), msg)
}

// wireNotices lets sources that can explain missing output print their notices to stderr.
func wireNotices(src source.Source) {
	if n, ok := src.(interface{ SetNotifier(func(string)) }); ok {
		n.SetNotifier(func(msg string) { printNotice(os.Stderr, msg) })
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
