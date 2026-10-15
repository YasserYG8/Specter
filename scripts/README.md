# Specter Test Simulators

Lightweight, zero-dependency HTTP server scripts to verify Specter's live process attachment, log level parsing, and multi-line stack trace grouping without installing full application runtimes.

---

## 1. Spring Boot Simulator (`sim_spring.py`)

Simulates a Spring Boot 3.3.4 service running Tomcat and Hibernate on port `:8082`.

- **Startup**: Prints authentic ASCII Spring Boot banner and Tomcat initialization logs.
- **`GET /api/all`**: Emits `INFO` level REST access log.
- **`GET /api/warn`**: Emits `WARN` level entity-not-found log.
- **`GET /api/error`**: Emits `ERROR` level log with a full multi-line Java `DataIntegrityViolationException` and `Caused by:` Hibernate stack trace.

### Usage:
```bash
# Terminal 1: Start simulator
python3 scripts/sim_spring.py

# Terminal 2: Attach Specter
./specter 8082
# Or test level filtering:
./specter 8082 --level warn

# Terminal 3: Trigger events
curl http://localhost:8082/api/all
curl http://localhost:8082/api/warn
curl http://localhost:8082/api/error
```

---

## 2. Angular Dev Server Simulator (`sim_angular.py`)

Simulates an Angular 20 / Vite dev server on port `:4200`.

- **Startup**: Emits chunk bundle output and Vite local server banner.
- **`GET /`**: Emits HTTP 200 access logs for `index.html`, `styles.css`, and `main.js`.
- **`GET /api/missing`**: Emits HTTP 404 access log.
- **`GET /crash`**: Emits an unhandled JavaScript `TypeError` with a multi-line V8/Node.js call stack.

### Usage:
```bash
# Terminal 1: Start simulator
python3 scripts/sim_angular.py

# Terminal 2: Attach Specter
./specter 4200

# Terminal 3: Trigger events
curl http://localhost:4200/
curl http://localhost:4200/api/missing
curl http://localhost:4200/crash
```
