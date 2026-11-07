## Why

Specter shows nothing from a Node.js server started in a terminal. On a Next.js 16 dev server (`pnpm run dev` in WSL), the terminal showed six request lines such as `GET /en/graphs 200 in 628ms`, and Specter attached to `next-server` showed none of them. A trace showed why:

```
1306  write(22</dev/pts/1>, " GET /en/graphs 200 in 628ms (next.js: 255ms, ...)\n", 129) = 129
```

Node's I/O library, libuv, opens the terminal again when stdout or stderr is a TTY, and writes through the new descriptor (here fd 22). Specter only reads fd 1 and fd 2, so it skips every line, even though they reach the same terminal. Node behind a pipe writes to fd 1, which is why the earlier test with `concurrently` worked. `npm run dev` in a terminal is the most common way developers run Node servers (Next.js, Express, Nest, Vite), so a first try with Specter on them shows only the "Nothing captured yet" notice.

## What Changes

- A write to a descriptor other than 1 and 2 is output when strace's `-y` annotation shows that it goes to the same place as the target's own stdout or stderr, the terminal, pipe or socket the target's fd 1 and fd 2 point to.
- `/dev/null` is excluded from this rule: another descriptor pointing to `/dev/null` is usually a library discarding data, not the program's output. The target's own fd 1 and fd 2 writes to `/dev/null` are still output, as today.
- A write to another descriptor without a destination, or with a different one, is ignored as today, and the "writes to its own log file" notice keeps working for those.
- These writes keep their real descriptor number through the pipeline, so line assembly does not mix them with fd 1 and fd 2. They count as stdout: `--stderr` does not show them. When stdout and stderr are the same terminal, Specter cannot know which one a reopened descriptor came from, and the README states this limit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `strace-source`: a new requirement for output written through other descriptors that lead to the target's own stdout or stderr.

## Impact

- `internal/source/strace/parse.go`: for fd 3 and above, the parser asks whether the destination is one of the target's output destinations before skipping the write. If it is, the write is decoded like fd 1 and 2, including unfinished and resumed calls.
- `internal/source/strace/spawn.go`: gives the parser the target's stream references, which are already read at start.
- Tests in `internal/source/strace`, using the real line captured from Next.js 16, and an E2E test where the test server reopens its own stdout and writes through the new descriptor.
- `README.md`: the `--stderr` limit. `CHANGELOG.md`: Fixed.
- The pipeline does not change. It already treats every descriptor other than 2 as stdout (`filter.go`), and assembles lines per process and descriptor.
