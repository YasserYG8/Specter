#!/usr/bin/env python3
"""
test_server.py - Standalone Python 3 HTTP server for live testing with Specter.

Listens on port 8080:
- Emits structured JSON logs on stdout:
  {"level":"info","msg":"Incoming HTTP request","path":...,"status":200}
- On GET /error, emits a multi-line traceback to stderr (fd: 2)
  including unindented exception line and returns HTTP 500.
"""

import http.server
import json
import os
import sys

PORT = int(os.environ.get("PORT", 8080))


class SpecterTestHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        # Suppress default BaseHTTPRequestHandler log formatting on stderr
        # so stdout has pure JSON logs and stderr has explicit error traces.
        pass

    def do_GET(self):
        if self.path.startswith("/error"):
            # Multi-line stack trace to stderr (fd: 2)
            if "node" in self.path:
                error_trace = (
                    "TypeError: Cannot read properties of undefined (reading 'id')\n"
                    "    at getUserById (/app/server.js:42:15)\n"
                    "    at handleRequest (/app/router.js:18:9)\n"
                    "    at Layer.handle [as handle_request] (/app/node_modules/express/lib/router/layer.js:95:5)\n"
                    "    at next (/app/node_modules/express/lib/router/route.js:144:13)\n"
                    "    at Route.dispatch (/app/node_modules/express/lib/router/route.js:114:3)\n"
                )
            else:
                error_trace = (
                    "Traceback (most recent call last):\n"
                    '  File "test_server.py", line 45, in do_GET\n'
                    "    self.handle_request()\n"
                    '  File "test_server.py", line 65, in handle_request\n'
                    "    raise TypeError(\"Cannot read properties of undefined (reading 'id')\")\n"
                    "TypeError: Cannot read properties of undefined (reading 'id')\n"
                )
            sys.stderr.write(error_trace)
            sys.stderr.flush()

            # Structured JSON log on stdout (fd: 1)
            error_log = {
                "level": "error",
                "msg": "Incoming HTTP request",
                "path": self.path,
                "status": 500,
            }
            sys.stdout.write(json.dumps(error_log, separators=(",", ":")) + "\n")
            sys.stdout.flush()

            # Send HTTP 500
            response_body = b'{"error":"Internal Server Error","status":500}\n'
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response_body)))
            self.end_headers()
            self.wfile.write(response_body)
            return

        # Normal request - emits structured JSON log on stdout (fd: 1)
        log_entry = {
            "level": "info",
            "msg": "Incoming HTTP request",
            "path": self.path,
            "status": 200,
        }
        sys.stdout.write(json.dumps(log_entry, separators=(",", ":")) + "\n")
        sys.stdout.flush()

        response_body = b'{"status":200,"message":"OK"}\n'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response_body)))
        self.end_headers()
        self.wfile.write(response_body)


def run_server(port=PORT):
    server_address = ("0.0.0.0", port)
    httpd = http.server.HTTPServer(server_address, SpecterTestHandler)

    startup_log = {
        "level": "info",
        "msg": f"Specter test server listening on port {port}",
        "port": port,
    }
    sys.stdout.write(json.dumps(startup_log, separators=(",", ":")) + "\n")
    sys.stdout.flush()

    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        shutdown_log = {
            "level": "info",
            "msg": "Server shutting down",
        }
        sys.stdout.write(json.dumps(shutdown_log, separators=(",", ":")) + "\n")
        sys.stdout.flush()
        httpd.server_close()


if __name__ == "__main__":
    run_server()
