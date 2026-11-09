// SPDX-License-Identifier: GPL-2.0
//
// Spike for the ebpf-source change, tasks 1.2 to 1.4. It proves, one at a time:
//
//   1.2  a write of up to 64 KB reaches user space whole and in order, through a ring buffer, in chunks of 16 KB;
//        and a write above 64 KB reports how many bytes were left out;
//   1.3  processes are followed by PID in Specter's own PID namespace (so it works in WSL2 and in containers),
//        and a child started by a followed process is followed too;
//   1.4  the device and inode of the file behind a descriptor can be read in the kernel with CO-RE.
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
#define MAX_PID_LEVELS 8

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
	int tgid;
	struct pid *thread_pid;
	struct files_struct *files;
};
#pragma clang attribute pop

/* Layouts of the syscall tracepoint contexts (see /sys/kernel/tracing/events/syscalls/sys_enter_write/format). */
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

/* What Go tells the program, once, before it is attached. */
struct config {
	__u64 ns_dev;    /* device of Specter's PID namespace file, in the kernel's encoding */
	__u64 ns_ino;    /* inode of that file, which is the namespace's inum */
	__u32 max_depth; /* how deep below the target children are followed */
	__u32 pad;
};

/* A process that is followed, with its depth below the target. */
struct followed_proc {
	__u32 depth;
};

/* The arguments of a write, kept from the enter to the exit of the call, per thread. */
struct call {
	__u64 buf;
	__u64 count;
	__u32 fd;
	__u32 tid;  /* thread ID in Specter's namespace */
	__u32 tgid; /* process ID in Specter's namespace */
	__u32 pad;
};

/* The header of each ring buffer record. The bytes of the chunk follow it. */
struct event {
	__u32 tid;
	__u32 tgid;
	__u32 fd;
	__u32 len;      /* bytes in this chunk */
	__u32 seq;      /* index of this chunk in its call, from 0 */
	__u32 nchunks;  /* chunks in this call */
	__u32 left_out; /* bytes of the write that were not captured; set on the last chunk only */
	__u32 mode;     /* i_mode of the file behind the descriptor, 0 if unknown */
	__u32 dev;      /* s_dev of the file's filesystem, in the kernel's encoding */
	__u32 pad;
	__u64 ino;      /* i_ino of the file */
	__u64 ts_ns;
};

struct scratch {
	struct event h;
	char data[CHUNK];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 8 * 1024 * 1024);
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
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct scratch);
} scratch_map SEC(".maps");

/* stats[0]: writes lost because the ring buffer was full; stats[1]: writes whose bytes could not be read */
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 2);
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

SEC("tracepoint/syscalls/sys_enter_write")
int on_write_enter(struct sys_enter_ctx *ctx)
{
	__u32 zero = 0;
	struct config *cfg = bpf_map_lookup_elem(&config_map, &zero);
	if (!cfg)
		return 0;
	struct bpf_pidns_info ids = {};
	if (ns_ids(cfg, &ids))
		return 0;
	if (!bpf_map_lookup_elem(&followed, &ids.tgid))
		return 0;

	struct call c = {};
	c.fd = (__u32)ctx->args[0];
	c.buf = ctx->args[1];
	c.count = ctx->args[2];
	c.tid = ids.pid;
	c.tgid = ids.tgid;
	__u64 id = bpf_get_current_pid_tgid();
	bpf_map_update_elem(&calls, &id, &c, BPF_ANY);
	return 0;
}

/* Sends the bytes of a finished write: up to MAX_CHUNKS chunks of CHUNK bytes, in order. */
static __always_inline void emit(const struct call *c, long ret)
{
	__u32 zero = 0;
	struct scratch *s = bpf_map_lookup_elem(&scratch_map, &zero);
	if (!s)
		return;

	__u32 total = ret > MAX_CAPTURE ? MAX_CAPTURE : (__u32)ret;
	__u32 left = (__u32)ret - total;
	__u32 nchunks = (total + CHUNK - 1) / CHUNK;

	s->h.tid = c->tid;
	s->h.tgid = c->tgid;
	s->h.fd = c->fd;
	s->h.nchunks = nchunks;
	s->h.mode = 0;
	s->h.dev = 0;
	s->h.ino = 0;
	fd_identity(c->fd, &s->h.dev, &s->h.ino, &s->h.mode);
	s->h.ts_ns = bpf_ktime_get_ns();

#pragma unroll
	for (__u32 i = 0; i < MAX_CHUNKS; i++) {
		__u32 off = i * CHUNK;
		if (off >= total)
			break;
		__u32 n = total - off;
		if (n > CHUNK)
			n = CHUNK;
		if (bpf_probe_read_user(s->data, n, (void *)(c->buf + off))) {
			bump(1);
			break;
		}
		s->h.seq = i;
		s->h.len = n;
		s->h.left_out = (i == nchunks - 1) ? left : 0;
		if (bpf_ringbuf_output(&events, s, sizeof(s->h) + n, 0)) {
			bump(0);
			break;
		}
	}
}

SEC("tracepoint/syscalls/sys_exit_write")
int on_write_exit(struct sys_exit_ctx *ctx)
{
	__u64 id = bpf_get_current_pid_tgid();
	struct call *found = bpf_map_lookup_elem(&calls, &id);
	if (!found)
		return 0;
	struct call c = *found;
	bpf_map_delete_elem(&calls, &id);

	long ret = ctx->ret;
	if (ret <= 0)
		return 0;
	emit(&c, ret);
	return 0;
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
			break;
		}
	}
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
