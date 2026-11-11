# Docker & Container Integration Architecture Notes

This document captures architectural analysis, real-world edge cases, and design decisions for Specter's container log acquisition engine.

---

## 1. The `docker-proxy` Trap (Bridge Network Port Mapping)

When a container publishes a port to the host (e.g. `docker run -p 3000:3000` or Docker Compose):
- On the host network namespace, `/proc/net/tcp` or `lsof -i :3000` identifies `/usr/bin/docker-proxy` as the listening process.
- `docker-proxy` is purely a user-space TCP proxy between the host network and the container bridge network.
- **Trap**: Attaching `strace` to `docker-proxy` yields zero output because application logs never pass through `docker-proxy` standard descriptors.
- **Solution**:
  1. Detect if the PID resolved on the port has `cmdline` starting with `docker-proxy`.
  2. Query `docker ps --filter "publish=<port>" --format "{{.ID}}"` or extract the container IP and port from `docker-proxy` arguments (`-container-ip <ip> -container-port <port>`).
  3. Stream directly from the resolved container via `docker logs -f --tail 0 <container_id>`.

---

## 2. Host Networking (`--net=host`)

- In host networking mode, the containerized process directly binds the host network interface without `docker-proxy`.
- `lsof -i :<port>` resolves the actual application PID on the host.
- Inspecting `/proc/<pid>/cgroup` reveals the container ID:
  - **cgroups v2**: `0::/system.slice/docker-<64_hex_id>.scope`
  - **cgroups v1**: `1:name=systemd:/docker/<64_hex_id>`
- Specter can then stream via `docker logs` or directly attach `strace` to the host PID.

---

## 3. Custom Logging Drivers (`--log-driver`) & Permission Fallback

- When containers are configured with non-standard log drivers (e.g., `awslogs`, `fluentd`, `syslog`, `none`), running `docker logs` produces an error:
  `Error response from daemon: configured logging driver does not support reading`
- Similarly, if the current user lacks permissions on `/var/run/docker.sock` or the Docker daemon is temporarily paused, `docker logs` fails.
- **Specter's Host Advantage**:
  Because all container processes execute directly on the host Linux kernel, Specter knows the container's host PID.
  When `docker logs` fails, Specter falls back gracefully to `strace` attachment on the container's host PID.

---

## 4. Unpublished Internal Ports (Future Extension)

- In microservice or Docker Compose setups, backend services (e.g., `api:8080`) are often exposed only to the internal bridge network, not published to the host.
- In this scenario, port resolution cannot discover the process by port number from the host network namespace.
- **Future Extension**: Allow targeting by container name or service name (e.g., `specter my-api` or `specter web`) in addition to numeric ports and PIDs.
