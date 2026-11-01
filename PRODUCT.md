# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

- **Core Product (Confirmed):** Go 1.24+ standalone static CLI binary (`cmd/specter`, `internal/`), packaged via GoReleaser (`.goreleaser.yaml`) for Linux (`amd64`, `arm64`), macOS (`arm64`), and Windows (`amd64`).
- **Web Surface Stack (Undecided / Inferred):** No web frontend scaffold exists in the repository yet; stack and deployment target remain open to be chosen or delegated when a web surface (such as a landing page or docs site) is built.

## Users

- **Primary Users (Inferred from repository & `README.md`):** Backend, full-stack, DevOps, and systems engineers running microservices, dev servers, background tasks, or Docker containers across multiple terminal sessions on Linux or Windows (WSL2).
- **Situation & Job:** A server or container is already running on a known port, PID, container name, or project folder, and the developer needs immediate visibility into its live `stdout` and `stderr` without hunting through open terminal tabs, looking up container IDs, adding logging SDKs/wrappers, or restarting the process.

## Product Purpose

- **What It Does:** Specter (`specter [port|pid|container|project]`) attaches non-invasively to any active server process or Docker container—or launches a zero-argument interactive terminal picker (`specter`)—and streams live `stdout` and `stderr` directly to the developer's terminal.
- **Why It Exists:** Eliminates the workflow friction of locating hidden terminal sessions or restarting running processes just to inspect live logs, JSON records, or unhandled exceptions.
- **What Success Means:** A developer runs `specter` or `specter 3000` and within milliseconds sees clean, color-coded, line-reassembled logs and grouped stack traces from an already running target with zero impact on the target process.

## Positioning

- **Meaningfully Different Mechanism:**
  - **Strictly Read-Only, Post-Hoc Attachment:** Attaches to *already running* processes without pre-wrapping commands, modifying code, injecting SDKs, or opening inbound network ports.
  - **Automatic Multi-Engine Source Selection:** Inspects `/proc/<pid>` and the local Docker daemon to automatically pick the right capture mechanism: `strace` (`ptrace` snooping on `write()` syscalls for descriptors 1 & 2), `docker logs`, `journalctl` (`_PID`), or direct `/proc/<pid>/fd` file tailing.
  - **Project-Aware Interactive Discovery:** Running `specter` with zero arguments scans active ports and containers, infers project names from working directories/git roots/container labels, and presents a keyboard-driven terminal selector (`TYPE`, `PORT`, `PROJECT`, `PROCESS / IMAGE`, `PATH / TARGET`).
  - **Multi-Worker Tree Tracing (`--tree`) & Heuristic Log Pipeline:** Automatically discovers forked child worker PIDs (e.g., Gunicorn, Node cluster, Celery) and runs a streaming state machine that reassembles split multi-threaded lines, formats single-line JSON, highlights HTTP status codes, and keeps multi-line stack traces grouped during `--grep` and `--level` filtering.

## Operating Context

- **Environments:** Linux workstations/servers (Ubuntu, Debian, Fedora, Arch, Alpine) and Windows via WSL2; terminal emulators (`bash`, `zsh`, `fish`, `powershell`, `tmux`).
- **Companion Tools & Interfaces:** Linux `/proc` filesystem, `lsof`, `iproute2` (`ss`), `strace` (`cap_sys_ptrace` / Yama `ptrace_scope`), `systemd-journald`, and Docker Engine CLI/socket.
- **Evaluation & Adoption Rituals:** Evaluated by developers via GitHub repository (`github.com/YasserYG8/Specter`), launch demo video (`brag-output/brag.mp4`), CLI help/examples, and quick terminal installation.

## Capabilities and Constraints

- **Confirmed Capabilities:**
  - Zero-argument interactive discovery picker (`specter`) with non-interactive aligned table fallback for pipes/CI.
  - Target resolution by TCP port (`specter 3000`), PID (`specter 18492`), Docker container name/ID (`specter billing-api`), or project name (`specter auth-service`).
  - Flags: `--level` (`-l`: `debug`, `info`, `warn`, `error`), `--grep` (`-g`: text/regex filter preserving stack traces), `--stderr` (fd 2 only), `--tree` (`-t`: child worker tree attachment), `--source` (`-s`: `docker`, `file`, `journal`, `strace`), `--json`, `--raw`, `--no-color`.
  - Dynamic shell autocompletion (`specter completion [bash|zsh|fish|powershell]`) for active ports, containers, and projects.
  - Actionable diagnostic errors for Yama `ptrace_scope` restrictions and non-Linux hosts.
- **Technical Constraints:**
  - Linux-first kernel dependency (`/proc` and `ptrace(2)`); Windows requires WSL2; native macOS is out of scope due to SIP restrictions on `ptrace`.
  - Strictly local and private: zero telemetry, zero external network calls, zero target mutation.
- **Explicitly Undecided Facts:**
  - Exact web surface scope (marketing landing page, documentation site, or web showcase) and hosting target.

## Brand Commitments

- **Name & CLI Identifier:** `Specter` (CLI binary `specter`, terminal prefix badge `[specter]`).
- **Voice:** Direct, technical, systems-engineering precision; honest about kernel/platform constraints; zero generic SaaS fluff.
- **User-Pinned Reference Asset (`@[brag-output]`):** The user explicitly linked `brag-output/` (`brag-output/brag.mp4`, `brag-output/brag.jpg`, `brag-output/brag-plan.md`) as the binding design video and visual reference for the project (dark terminal environment `#090D14` / `#0F1520`, Specter brand cyan `#22D3EE`, host/2xx green `#34D399`, Docker blue `#60A5FA`, warning/port yellow `#FBBF24`, error red `#F87171`, paired geometric sans headlines with developer monospace terminal UI).

## Evidence on Hand

- **Launch Video & Visual Assets (`brag-output/`):**
  - Full 20s launch video (`1920×1080`, `30fps`): `brag-output/brag.mp4`
  - High-resolution poster frame: `brag-output/brag.jpg`
  - Scene stills: `brag-output/work/previews/scene1_settled.png` through `brag-output/work/previews/scene5_poster.png`
  - Storyboard & share copy: `brag-output/brag-plan.md`, `brag-output/share-copy.txt`
- **Documentation & Source Truth:**
  - Product overview, architecture diagram, and CLI reference: `README.md`
  - Docker integration notes: `docs/research/docker-integration.md`
  - CLI & UI implementation: `cmd/specter/main.go`, `cmd/specter/completion.go`, `internal/ui/picker.go`, `internal/pipeline/renderer.go`, `internal/pipeline/json.go`
  - Release & license metadata: `.goreleaser.yaml`, `LICENSE` (MIT)
- **Confirmed Absences (Must Not Be Fabricated):**
  - No customer testimonials, user/download counts, external press quotes, company logos, or comparative latency benchmarks exist on hand.

## Product Principles

1. **Zero-Friction Attachment Over Instrumentation:** Every workflow must respect that the developer's server is already running; never require code edits, SDKs, wrappers, or restarts.
2. **Show Real Terminal Truth:** Demonstrations and interfaces must reflect authentic CLI commands, real `[specter]` output formatting, and accurate system behavior rather than abstract illustrations.
3. **Read-Only Safety & Local Privacy:** Specter observes without interfering—never signaling processes, mutating state, or transmitting data off the machine.
4. **Honest Systems Diagnostics:** When kernel boundaries (`ptrace_scope`, permissions, OS platform) block an action, explain the exact system cause and give the copy-pasteable remediation command.
