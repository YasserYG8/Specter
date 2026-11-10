// SPDX-License-Identifier: GPL-2.0
//
// The BPF program of the ebpf source. It watches write and writev in the kernel and hands the target's output to
// Specter through a ring buffer, without stopping the target. It grew out of the spike in
// experiments/ebpf-spike/ringbuf, whose proofs (long writes, PIDs in Specter's namespace, forked workers, the file
// behind a descriptor) it keeps.
//
// How a write travels:
//
//   sys_enter_write / sys_enter_writev   decide, from the process and the file behind the descriptor, whether this
//                                        write is output; if so, remember its arguments for this thread
//   sys_exit_write / sys_exit_writev     the kernel has now read the user's buffer, so its pages are in memory; read
//                                        the bytes the call reports as written, up to 64 KB, and send them
//   sched_process_fork                   a child of a followed process is followed too, up to a depth limit
//   sched_process_exit                   forget processes that end; report how the target ended
//
// The rules for which writes are output are the ones of the strace source, with the destinations compared by
// device and inode instead of by path (openspec/specs/ebpf-source/spec.md, "Output Rules Match the Strace Source").
//
// Only the kernel types this file really reads are declared, with preserve_access_index, so that libbpf fixes the
// field offsets at load time from the running kernel's BTF. There is no vmlinux.h.

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

#define CHUNK 16384
#define MAX_CHUNKS 4
#define MAX_CAPTURE (CHUNK * MAX_CHUNKS) /* 65536, like strace -s 65536 */
#define MAX_IOV 16                       /* iovec entries of one writev that are read */
#define MAX_PID_LEVELS 8

/* Indexes of the counters in the stats map. They are kept in every build: a counter costs one map lookup on the
 * paths of followed processes, and they are what tells a failure to capture from a failure to read, so they show
 * where a write was lost when a new kernel behaves differently. */
#define ST_LOST 0           /* events lost because the ring buffer was full */
#define ST_UNREADABLE 1     /* writes whose bytes could not be read */
#define ST_ENTER_FOLLOWED 2 /* writes by a followed process, seen at entry */
#define ST_DROPPED 3        /* ... of which not output of the target, by the rules */
#define ST_CALL_STORED 4    /* ... of which kept for the exit handler */
#define ST_EXIT_MATCHED 5   /* write exits that found their stored call */
#define ST_DATA_SENT 6      /* data events sent to the ring buffer */
#define ST_NS_FAILED 7      /* the PID namespace lookup failed at write entry */
#define ST_FORK_PARENT 8    /* forks by a followed process */
#define ST_FORK_ADDED 9     /* children added to the followed set */
#define ST_FORK_NO_NS 10    /* children whose ID in our namespace was not found */
#define ST_EXIT_REMOVED 11  /* followed processes removed when they exited */
#define ST_FD_UNKNOWN 12    /* writes whose file could not be identified */
#define ST_COUNT 16

#define S_IFMT 0170000
#define S_IFREG 0100000

/* What an event carries. */
#define EV_DATA 0   /* bytes the target wrote */
#define EV_FILE 1   /* a descriptor other than 1 and 2 leads to a regular file; no bytes */
#define EV_MARKER 2 /* arg bytes of the call were not captured */
#define EV_EXIT 3   /* the target ended; arg is the kernel's exit_code of its leader */

#pragma clang attribute push(__attribute__((preserve_access_index)), apply_to = record)
struct ns_common {
	unsigned int inum;
};
struct pid_namespace {
	struct ns_common ns;
};
struct upid {
	int nr;
	struct pid_namespace *ns;
};
struct pid {
	unsigned int level;
	struct upid numbers[];
};
struct super_block {
	__u32 s_dev;
};
struct inode {
	unsigned short i_mode;
	struct super_block *i_sb;
	unsigned long i_ino;
};
struct file {
	struct inode *f_inode;
};
struct fdtable {
	unsigned int max_fds;
	struct file **fd;
};
struct files_struct {
	struct fdtable *fdt;
};
struct task_struct {
	int exit_code;
	int tgid;
	struct pid *thread_pid;
	struct files_struct *files;
};
#pragma clang attribute pop

/* Layouts of the raw_syscalls tracepoint contexts (see /sys/kernel/tracing/events/raw_syscalls/sys_enter/format).
 * Those tracepoints fire for every system call and carry its number and its arguments, decoded, on both amd64 and
 * arm64. The per-syscall tracepoints (syscalls:sys_enter_write) are not used: on Linux 7.0 they miss the writes of
 * processes that were already running when the program was attached, which is every target Specter attaches to. */
struct sys_enter_ctx {
	__u64 common;
	long id;
	unsigned long args[6];
};
struct sys_exit_ctx {
	__u64 common;
	long id;
	long ret;
};

/* An element of the vector of writev, as it lies in the target's memory (64-bit processes). */
struct iovec_user {
	__u64 base;
	__u64 len;
};

/* What Go tells the program, once, before it is attached. Devices are in the kernel's encoding. */
struct config {
	__u64 ns_dev;       /* Specter's PID namespace file: device ... */
	__u64 ns_ino;       /* ... and inode, which is the namespace's inum */
	__u64 out_dev[2];   /* where the target's fd 1 and fd 2 lead ... */
	__u64 out_ino[2];
	__u64 null_dev;     /* ... and /dev/null */
	__u64 null_ino;
	__u32 target_tgid;  /* the target, as Specter sees its PID */
	__u32 max_depth;    /* how deep below the target children are followed */
	__u32 out_known;    /* bit 0: fd 1 could be read, bit 1: fd 2 could be read */
	__u32 null_known;
};

/* A process that is followed, with its depth below the target. */
struct followed_proc {
	__u32 depth;
};

/* The arguments of a write that is output, kept from the enter to the exit of the call, per thread. */
struct call {
	__u64 buf;   /* write: the buffer; writev: the vector */
	__u64 count; /* write: bytes to write; writev: entries in the vector */
	__u64 dev;   /* the file behind the descriptor, read before the call */
	__u64 ino;
	__u32 fd;
	__u32 tid;   /* thread ID in Specter's namespace */
	__u32 tgid;  /* process ID in Specter's namespace */
	__u32 mode;
	__u32 kind;  /* EV_DATA or EV_FILE */
	__u32 is_vec;
};

/* The header of each ring buffer record. The bytes of the chunk follow it. */
struct event {
	__u32 tid;
	__u32 tgid;
	__u32 fd;
	__u32 len;   /* bytes that follow the header */
	__u32 kind;
	__u32 arg;   /* see EV_MARKER and EV_EXIT */
	__u32 mode;  /* i_mode of the file behind the descriptor, 0 if unknown */
	__u32 dev;   /* s_dev of the file's filesystem, in the kernel's encoding */
	__u64 ino;
	__u64 ts_ns; /* CLOCK_MONOTONIC */
};

struct scratch {
	struct event h;
	char data[CHUNK];
};

struct file_key {
	__u64 dev;
	__u64 ino;
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 8 * 1024 * 1024); /* Go may change it before loading */
} events SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct config);
} config_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 4096);
	__type(key, __u32); /* process ID in Specter's namespace */
	__type(value, struct followed_proc);
} followed SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, __u64); /* pid_tgid as the kernel reports it, which is unique per thread */
	__type(value, struct call);
} calls SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 4096);
	__type(key, struct file_key);
	__type(value, __u8);
} seen_files SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct scratch);
} scratch_map SEC(".maps");

/* Counters, one set per CPU: see the ST_ indexes */
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, ST_COUNT);
	__type(key, __u32);
	__type(value, __u64);
} stats SEC(".maps");

static __always_inline void bump(__u32 which)
{
	__u64 *v = bpf_map_lookup_elem(&stats, &which);
	if (v)
		*v += 1;
}

/* The device and inode of the file behind a descriptor of the current process. Returns 0 on success. */
static __always_inline int fd_identity(__u32 fd, __u32 *dev, __u64 *ino, __u32 *mode)
{
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();
	struct files_struct *files = BPF_CORE_READ(task, files);
	struct fdtable *fdt = BPF_CORE_READ(files, fdt);
	if (!fdt)
		return -1;
	if (fd >= BPF_CORE_READ(fdt, max_fds))
		return -1;
	struct file **table = BPF_CORE_READ(fdt, fd);
	struct file *f = NULL;
	if (bpf_probe_read_kernel(&f, sizeof(f), table + fd) || !f)
		return -1;
	struct inode *inode = BPF_CORE_READ(f, f_inode);
	if (!inode)
		return -1;
	*ino = BPF_CORE_READ(inode, i_ino);
	*mode = BPF_CORE_READ(inode, i_mode);
	*dev = BPF_CORE_READ(inode, i_sb, s_dev);
	return 0;
}

/* The process and thread IDs of the current task in Specter's namespace. Returns 0 on success. */
static __always_inline int ns_ids(const struct config *cfg, struct bpf_pidns_info *out)
{
	return bpf_get_ns_current_pid_tgid(cfg->ns_dev, cfg->ns_ino, out, sizeof(*out));
}

static __always_inline int same_as_out(const struct config *cfg, int which, __u64 dev, __u64 ino)
{
	return (cfg->out_known & (1u << which)) && cfg->out_dev[which] == dev && cfg->out_ino[which] == ino;
}

/* Sends an event that has no bytes. */
static __always_inline void send_plain(__u32 kind, __u32 arg, __u32 tid, __u32 tgid, __u32 fd, __u32 mode, __u32 dev,
				       __u64 ino)
{
	struct event e = {};
	e.tid = tid;
	e.tgid = tgid;
	e.fd = fd;
	e.kind = kind;
	e.arg = arg;
	e.mode = mode;
	e.dev = dev;
	e.ino = ino;
	e.ts_ns = bpf_ktime_get_ns();
	if (bpf_ringbuf_output(&events, &e, sizeof(e), 0))
		bump(ST_LOST);
}

/* The decision of the enter handlers: is this write output of the target? */
static __always_inline int handle_enter(__u32 fd, __u64 buf, __u64 count, __u32 is_vec)
{
	__u32 zero = 0;
	struct config *cfg = bpf_map_lookup_elem(&config_map, &zero);
	if (!cfg)
		return 0;
	struct bpf_pidns_info ids = {};
	if (ns_ids(cfg, &ids)) {
		bump(ST_NS_FAILED);
		return 0;
	}
	if (!bpf_map_lookup_elem(&followed, &ids.tgid))
		return 0;
	bump(ST_ENTER_FOLLOWED);

	__u32 dev = 0, mode = 0;
	__u64 ino = 0;
	int known = fd_identity(fd, &dev, &ino, &mode) == 0;
	if (!known)
		bump(ST_FD_UNKNOWN);
	int to_out = known && (same_as_out(cfg, 0, dev, ino) || same_as_out(cfg, 1, dev, ino));

	__u32 kind;
	if (fd == 1 || fd == 2) {
		/* The target's own writes always count. Another process counts when its write goes where the target's
		 * stdout or stderr go, or when that cannot be told. */
		if (ids.tgid == cfg->target_tgid || !known || to_out || cfg->out_known != 3)
			kind = EV_DATA;
		else {
			bump(ST_DROPPED);
			return 0;
		}
	} else {
		/* Another descriptor counts when it leads where the target's stdout or stderr lead, which is how Node.js
		 * writes to a terminal. /dev/null never counts. A regular file is the target's own log file. */
		int is_null = cfg->null_known && dev == cfg->null_dev && ino == cfg->null_ino;
		if (to_out && !is_null)
			kind = EV_DATA;
		else if (known && (mode & S_IFMT) == S_IFREG)
			kind = EV_FILE;
		else {
			bump(ST_DROPPED);
			return 0;
		}
	}

	struct call c = {};
	c.fd = fd;
	c.buf = buf;
	c.count = count;
	c.dev = dev;
	c.ino = ino;
	c.mode = mode;
	c.tid = ids.pid;
	c.tgid = ids.tgid;
	c.kind = kind;
	c.is_vec = is_vec;
	__u64 id = bpf_get_current_pid_tgid();
	bpf_map_update_elem(&calls, &id, &c, BPF_ANY);
	bump(ST_CALL_STORED);
	return 0;
}

/* The numbers of the write and writev system calls, which differ by architecture. Go sets them before loading, so
 * the program compares a constant and a call of any other system call costs a few instructions. */
volatile const __u32 nr_write = 1;   /* x86-64: 1, arm64: 64 */
volatile const __u32 nr_writev = 20; /* x86-64: 20, arm64: 66 */

SEC("tracepoint/raw_syscalls/sys_enter")
int on_sys_enter(struct sys_enter_ctx *ctx)
{
	if (ctx->id == nr_write)
		return handle_enter((__u32)ctx->args[0], ctx->args[1], ctx->args[2], 0);
	if (ctx->id == nr_writev)
		return handle_enter((__u32)ctx->args[0], ctx->args[1], ctx->args[2], 1);
	return 0;
}

/*
 * Reads up to MAX_CHUNKS * CHUNK bytes at base in the target's memory and sends them in events of up to CHUNK bytes,
 * in order. Returns how many bytes were sent. It stops early when the bytes cannot be read or the ring buffer is
 * full, and the caller then reports the rest as not captured.
 *
 * This is a global function on purpose. The verifier checks a global function once, on its own, instead of again
 * for every path through its caller, which is what keeps the time to load the program short.
 */
__noinline __u32 emit_range(struct scratch *s, __u64 base, __u32 len)
{
	/* The verifier assumes a pointer argument of a global function may be NULL */
	if (!s)
		return 0;

	__u32 sent = 0;

#pragma unroll
	for (int i = 0; i < MAX_CHUNKS; i++) {
		if (sent >= len)
			break;
		__u32 n = len - sent;
		if (n > CHUNK)
			n = CHUNK;
		if (bpf_probe_read_user(s->data, n, (void *)(base + sent))) {
			bump(ST_UNREADABLE);
			break;
		}
		s->h.len = n;
		s->h.ts_ns = bpf_ktime_get_ns();
		if (bpf_ringbuf_output(&events, s, sizeof(s->h) + n, 0)) {
			bump(ST_LOST);
			break;
		}
		bump(ST_DATA_SENT);
		sent += n;
	}
	return sent;
}

/*
 * Sends the bytes of a finished write or writev: up to MAX_CAPTURE bytes, in order. A write is a vector of one
 * entry. Only the bytes the call reports as written count, which are the first bytes of the vector. What is not
 * captured (more than MAX_CAPTURE, or beyond MAX_IOV entries) is reported in one EV_MARKER event after the bytes.
 */
static __always_inline void emit_call(const struct call *c, long ret)
{
	__u32 zero = 0;
	struct scratch *s = bpf_map_lookup_elem(&scratch_map, &zero);
	if (!s)
		return;

	s->h.tid = c->tid;
	s->h.tgid = c->tgid;
	s->h.fd = c->fd;
	s->h.kind = EV_DATA;
	s->h.arg = 0;
	s->h.mode = c->mode;
	s->h.dev = (__u32)c->dev;
	s->h.ino = c->ino;

	__u64 remaining = (__u64)ret; /* written bytes not yet assigned to an entry */
	__u32 budget = MAX_CAPTURE;
	__u32 captured = 0;

#pragma unroll
	for (__u32 i = 0; i < MAX_IOV; i++) {
		if (remaining == 0 || budget == 0)
			break;

		__u64 base, len;
		if (c->is_vec) {
			struct iovec_user v;
			if (i >= c->count)
				break;
			if (bpf_probe_read_user(&v, sizeof(v), (void *)(c->buf + (__u64)i * sizeof(v))))
				break;
			base = v.base;
			len = v.len;
		} else {
			if (i)
				break;
			base = c->buf;
			len = c->count;
		}
		if (len > remaining)
			len = remaining;
		remaining -= len;

		__u32 want = len > budget ? budget : (__u32)len;
		if (want == 0)
			continue;
		__u32 got = emit_range(s, base, want);
		captured += got;
		budget -= got;
		if (got < want)
			break;
	}

	if ((__u64)ret > captured)
		send_plain(EV_MARKER, (__u32)((__u64)ret - captured), c->tid, c->tgid, c->fd, 0, 0, 0);
}

static __always_inline int handle_exit(long ret)
{
	__u64 id = bpf_get_current_pid_tgid();
	struct call *found = bpf_map_lookup_elem(&calls, &id);
	if (!found)
		return 0;
	bump(ST_EXIT_MATCHED);
	struct call c = *found;
	bpf_map_delete_elem(&calls, &id);

	if (ret <= 0)
		return 0;

	if (c.kind == EV_FILE) {
		/* Once per file: Go prints the notice about a target that writes to its own log file */
		struct file_key key = {.dev = c.dev, .ino = c.ino};
		__u8 one = 1;
		if (bpf_map_lookup_elem(&seen_files, &key))
			return 0;
		bpf_map_update_elem(&seen_files, &key, &one, BPF_ANY);
		send_plain(EV_FILE, 0, c.tid, c.tgid, c.fd, c.mode, (__u32)c.dev, c.ino);
		return 0;
	}

	emit_call(&c, ret);
	return 0;
}

SEC("tracepoint/raw_syscalls/sys_exit")
int on_sys_exit(struct sys_exit_ctx *ctx)
{
	if (ctx->id != nr_write && ctx->id != nr_writev)
		return 0;
	return handle_exit(ctx->ret);
}

/* A process started by a followed process is followed too, up to the depth limit. Threads are not processes. */
SEC("tp_btf/sched_process_fork")
int BPF_PROG(on_fork, struct task_struct *parent, struct task_struct *child)
{
	__u32 zero = 0;
	struct config *cfg = bpf_map_lookup_elem(&config_map, &zero);
	if (!cfg)
		return 0;

	/* This runs in the parent's context, so the current task is the parent. */
	struct bpf_pidns_info ids = {};
	if (ns_ids(cfg, &ids))
		return 0;
	struct followed_proc *pp = bpf_map_lookup_elem(&followed, &ids.tgid);
	if (!pp)
		return 0;
	bump(ST_FORK_PARENT);
	if (BPF_CORE_READ(child, tgid) == BPF_CORE_READ(parent, tgid))
		return 0; /* a new thread, not a new process */
	if (pp->depth + 1 > cfg->max_depth)
		return 0;

	/* The child's ID in Specter's namespace: the entry of its struct pid that belongs to that namespace. */
	struct pid *tp = BPF_CORE_READ(child, thread_pid);
	if (!tp)
		return 0;
	unsigned int level = BPF_CORE_READ(tp, level);
	__u64 base = (__u64)tp + bpf_core_field_offset(tp->numbers);
	__u64 step = bpf_core_type_size(struct upid);

	struct followed_proc add = {.depth = pp->depth + 1};
#pragma unroll
	for (int i = 0; i < MAX_PID_LEVELS; i++) {
		if (i > level)
			break;
		struct upid *up = (struct upid *)(base + i * step);
		struct pid_namespace *ns = BPF_CORE_READ(up, ns);
		if (!ns)
			continue;
		if (BPF_CORE_READ(ns, ns.inum) == cfg->ns_ino) {
			__u32 nr = BPF_CORE_READ(up, nr);
			bpf_map_update_elem(&followed, &nr, &add, BPF_ANY);
			bump(ST_FORK_ADDED);
			return 0;
		}
	}
	bump(ST_FORK_NO_NS);
	return 0;
}

/*
 * A followed process ends when its thread-group leader does. The target's end is reported with the kernel's exit
 * code, which Go decodes into an exit code or a signal. Workers, helpers and threads are forgotten without a word.
 */
SEC("tp_btf/sched_process_exit")
int BPF_PROG(on_exit, struct task_struct *p)
{
	__u32 zero = 0;
	struct config *cfg = bpf_map_lookup_elem(&config_map, &zero);
	if (!cfg)
		return 0;
	struct bpf_pidns_info ids = {};
	if (ns_ids(cfg, &ids)) /* the exiting task is the current one */
		return 0;
	if (ids.pid != ids.tgid)
		return 0; /* a thread that is not the leader */
	if (!bpf_map_lookup_elem(&followed, &ids.tgid))
		return 0;
	bpf_map_delete_elem(&followed, &ids.tgid);
	bump(ST_EXIT_REMOVED);

	if (ids.tgid == cfg->target_tgid)
		send_plain(EV_EXIT, (__u32)BPF_CORE_READ(p, exit_code), ids.pid, ids.tgid, 0, 0, 0, 0);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
