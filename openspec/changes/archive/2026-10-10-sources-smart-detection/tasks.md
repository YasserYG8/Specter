## 1. Strace Source Tree Tracing Support

- [x] 1.1 Add `FollowTree` option to `strace.Source` and conditionally pass `-f` flag in `internal/source/strace/spawn.go`
- [x] 1.2 Add tests for strace options in `internal/source/strace/spawn_test.go`

## 2. Docker Container Source

- [x] 2.1 Implement `/proc/<pid>/cgroup` parser in `internal/source/docker/cgroup.go` to extract Docker/containerd container IDs
- [x] 2.2 Implement `docker.Source` in `internal/source/docker/source.go` spawning `docker logs -f --tail 0 <container_id>`
- [x] 2.3 Add unit tests for cgroup parsing and docker streaming in `internal/source/docker/cgroup_test.go` and `source_test.go`

## 3. File Tail Source

- [x] 3.1 Implement regular file detector in `internal/source/file/detect.go` inspecting `/proc/<pid>/fd/{1,2}`
- [x] 3.2 Implement `file.Source` in `internal/source/file/source.go` using end-seeking and non-blocking polling loops
- [x] 3.3 Add unit tests for file detection and live tailing in `internal/source/file/source_test.go`

## 4. Systemd Journal Source

- [x] 4.1 Implement systemd unit detector in `internal/source/journal/detect.go` inspecting cgroups and systemd slices
- [x] 4.2 Implement `journal.Source` in `internal/source/journal/source.go` spawning `journalctl -f -n 0 _PID=<pid>`
- [x] 4.3 Add unit tests for journal detection and streaming in `internal/source/journal/source_test.go`

## 5. Source Selector and CLI Integration

- [x] 5.1 Implement `SourceSelector` in `internal/source/selector/selector.go` with priority order and `--source` flag validation
- [x] 5.2 Add unit tests for selection priority in `internal/source/selector/selector_test.go`
- [x] 5.3 Wire `SourceSelector`, `--tree`, and `--source` flags into `cmd/specter/main.go`
- [x] 5.4 Validate end-to-end multi-source selection and cross-compilation build
