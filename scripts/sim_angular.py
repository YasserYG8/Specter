import sys
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

class AngularDevHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/":
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(b"<html><head><title>Cars Frontend</title></head><body><app-root></app-root></body></html>")
            sys.stdout.write('"GET /" 200 - 14ms\n')
            sys.stdout.write('"GET /styles.css" 200 - 2ms\n')
            sys.stdout.write('"GET /main.js" 200 - 5ms\n')
            sys.stdout.flush()
        elif self.path == "/api/missing":
            self.send_response(404)
            self.end_headers()
            sys.stdout.write('"GET /api/missing" 404 - 1ms\n')
            sys.stdout.flush()
        elif self.path == "/crash":
            self.send_response(500)
            self.end_headers()
            sys.stderr.write("TypeError: Cannot read properties of undefined (reading 'subscribe')\n")
            sys.stderr.write("    at CarsComponent.ngOnInit (src/app/cars/cars.component.ts:24:15)\n")
            sys.stderr.write("    at executeInitHooks (node_modules/@angular/core/fesm2022/core.mjs:11520:30)\n")
            sys.stderr.write("    at refreshView (node_modules/@angular/core/fesm2022/core.mjs:13470:21)\n")
            sys.stderr.write("    at detectChangesInView (node_modules/@angular/core/fesm2022/core.mjs:13634:9)\n")
            sys.stderr.flush()
        else:
            self.send_response(200)
            self.end_headers()

    def log_message(self, format, *args):
        pass

def run():
    print("Initial Chunk Files | Names         |  Raw Size")
    print("polyfills.js        | polyfills     |  91.24 kB |")
    print("main.js             | main          | 240.15 kB |")
    print("styles.css          | styles        |  15.60 kB |")
    print("\nApplication bundle generation complete. [1.234 seconds]")
    print("Watch mode enabled. Watching for changes...")
    print("➜  Local:   http://localhost:4200/")
    print("➜  Network: use --host to expose")
    print("➜  press h + enter to show help\n")
    sys.stdout.flush()

    server = HTTPServer(("0.0.0.0", 4200), AngularDevHandler)
    server.serve_forever()

if __name__ == "__main__":
    run()
