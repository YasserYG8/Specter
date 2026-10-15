import sys
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

class SpringBootHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        ts = time.strftime("%Y-%m-%dT%H:%M:%S.000+01:00")
        if self.path == "/api/all":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'[{"id":1,"brand":"Porsche","model":"GT3"}]')
            sys.stdout.write(f"{ts}  INFO 14220 --- [cars_proj] [nio-8082-exec-1] c.y.cars.restcontrollers.CarRESTController: GET /api/all - Returned 5 cars\n")
            sys.stdout.flush()
        elif self.path == "/api/warn":
            self.send_response(404)
            self.end_headers()
            sys.stdout.write(f"{ts}  WARN 14220 --- [cars_proj] [nio-8082-exec-2] c.y.cars.restcontrollers.CarRESTController: Car with id 999 not found\n")
            sys.stdout.flush()
        elif self.path == "/api/error":
            self.send_response(500)
            self.end_headers()
            sys.stderr.write(f"{ts} ERROR 14220 --- [cars_proj] [nio-8082-exec-3] o.a.c.c.C.[.[.[/].[dispatcherServlet]    : Servlet.service() for servlet [dispatcherServlet] in context with path [] threw exception [Request processing failed: org.springframework.dao.DataIntegrityViolationException: could not execute statement [Cannot delete or update a parent row: a foreign key constraint fails]] with root cause\n")
            sys.stderr.write("org.springframework.dao.DataIntegrityViolationException: could not execute statement [Cannot delete or update a parent row: a foreign key constraint fails]\n")
            sys.stderr.write("\tat org.springframework.orm.jpa.vendor.HibernateJpaDialect.convertHibernateAccessException(HibernateJpaDialect.java:276)\n")
            sys.stderr.write("\tat org.springframework.orm.jpa.vendor.HibernateJpaDialect.translateExceptionIfPossible(HibernateJpaDialect.java:241)\n")
            sys.stderr.write("\tat org.springframework.orm.jpa.JpaTransactionManager.doCommit(JpaTransactionManager.java:566)\n")
            sys.stderr.write("\tat org.springframework.transaction.support.AbstractPlatformTransactionManager.processCommit(AbstractPlatformTransactionManager.java:795)\n")
            sys.stderr.write("\tat com.yasser.cars.service.CarServiceImpl.deleteCarById(CarServiceImpl.java:45)\n")
            sys.stderr.write("\tat com.yasser.cars.restcontrollers.CarRESTController.deleteCar(CarRESTController.java:78)\n")
            sys.stderr.write("\tCaused by: org.hibernate.exception.ConstraintViolationException: could not execute statement [Cannot delete or update a parent row: a foreign key constraint fails]\n")
            sys.stderr.write("\tat org.hibernate.exception.internal.SQLExceptionTypeDelegate.convert(SQLExceptionTypeDelegate.java:62)\n")
            sys.stderr.write("\tat org.hibernate.exception.internal.StandardSQLExceptionConverter.convert(StandardSQLExceptionConverter.java:58)\n")
            sys.stderr.write("\t... 35 more\n")
            sys.stderr.flush()
        else:
            self.send_response(200)
            self.end_headers()

    def log_message(self, format, *args):
        # Suppress default python http.server logging to emit authentic Spring Boot logs
        pass

def run():
    print("  .   ____          _            __ _ _")
    print(" /\\\\ / ___'_ __ _ _(_)_ __  __ _ \\ \\ \\ \\")
    print("( ( )\\___ | '_ | '_| | '_ \\/ _` | \\ \\ \\ \\")
    print(" \\\\/  ___)| |_)| | | | | || (_| |  ) ) ) )")
    print("  '  |____| .__|_| |_|_| |_\\__, | / / / /")
    print(" =========|_|==============|___/=/_/_/_/")
    print(" :: Spring Boot ::                (v3.3.4)\n")
    ts = time.strftime("%Y-%m-%dT%H:%M:%S.000+01:00")
    print(f"{ts}  INFO 14220 --- [cars_proj] [           main] c.y.cars.CarsProjApplication             : Starting CarsProjApplication using Java 25 with PID 14220")
    print(f"{ts}  INFO 14220 --- [cars_proj] [           main] o.s.b.w.embedded.tomcat.TomcatWebServer  : Tomcat initialized with port 8082 (http)")
    print(f"{ts}  INFO 14220 --- [cars_proj] [           main] o.s.b.w.embedded.tomcat.TomcatWebServer  : Tomcat started on port 8082 (http) with context path '/'")
    print(f"{ts}  INFO 14220 --- [cars_proj] [           main] c.y.cars.CarsProjApplication             : Started CarsProjApplication in 1.45 seconds (process running for 1.82)")
    sys.stdout.flush()

    server = HTTPServer(("0.0.0.0", 8082), SpringBootHandler)
    server.serve_forever()

if __name__ == "__main__":
    run()
