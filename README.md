# Specter 👻

> **Live logs for any running server by port. Zero setup, zero code changes, zero restarts.**

`specter` is a developer CLI tool that lets you view the live output (`stdout` and `stderr`) of any local server process just by giving it a port number.

```bash
specter 3000
```

---

## 💡 The Problem

You start a backend server (Node.js, FastAPI, Spring Boot, Go, or a Docker container) in another terminal, background window, or container. When something goes wrong or you need to see what's happening:
- You have to find which terminal window it's running in.
- Or restart it with a logging wrapper.
- Or dig up the container ID and run complex docker commands.

**Specter fixes this.** You just give it the port you are calling (like `3000`), and it immediately attaches and streams the live logs to your current terminal screen.

---

## ✨ Features

- **⚡ Zero Setup**: Works on servers you already started. You don't need to add any libraries, SDKs, or configuration to your project.
- **🔒 Read-Only & Non-Invasive**: Specter never restarts, pauses, or modifies your process. It never opens a network port.
- **🎨 Clean & Readable**:
  - Highlights log levels (`INFO`, `WARN`, `ERROR`).
  - Formats raw JSON log lines into readable single-line logs.
  - Groups multi-line stack traces (Node.js, Python, Java) so errors stay together.
- **🔍 Built-in Filtering**:
  - Filter by log level (`--level error`)
  - Filter by text or regex (`--grep "database"`)
  - Show only standard error (`--stderr`)
- **🐳 Smart Detection**:
  - Automatically detects whether the process is running in **Docker**, as a **systemd service**, writing to a **file**, or in a standard **terminal**.

---

## 🚀 Quick Usage

```bash
# Stream live logs from port 3000
specter 3000

# Stream logs from a specific Process ID (PID)
specter 14250

# Only show warning and error messages
specter 3000 --level warn

# Filter for specific text or regex pattern
specter 3000 --grep "auth"

# View only stderr
specter 3000 --stderr
```

---

## 🛠️ How It Works (In Simple Terms)

1. **Finds the Process**: Looks up what process ID (PID) is listening on the port (using Linux `lsof` or `ss`).
2. **Finds the Output**: Inspects the process to see where its output is sent (terminal screen, pipe, log file, or Docker container).
3. **Captures Output**: Non-invasively listens to `write()` system calls using Linux `strace`.
4. **Cleans & Renders**: Assembles pieces into complete lines, parses JSON and error traces, and prints them with syntax colors.

---

## 💻 System Requirements & Platform Support

- **Linux**: Fully supported natively (Ubuntu, Debian, Arch, Fedora, etc.).
- **Windows**: Supported via **WSL2** (Windows Subsystem for Linux).
- **macOS**: Planned for a future release.
- **Prerequisites**: Node.js 20+, `strace`, `lsof`.

---

## 📄 License

MIT
