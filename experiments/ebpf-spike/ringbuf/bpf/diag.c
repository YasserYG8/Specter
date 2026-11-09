// SPDX-License-Identifier: GPL-2.0
//
// Diagnostic, not part of Specter. Which hooks see the writes of a process? Each hook counts the writes of the
// processes in the watch map, per hook, so the same writes can be compared across mechanisms on one kernel.

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

struct key {
	__u32 mech;
	__u32 tgid;
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 1024);
	__type(key, struct key);
	__type(value, __u64);
} hits SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 64);
	__type(key, __u32);
	__type(value, __u8);
} watch SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u64);
} nr_write SEC(".maps");

struct nsinfo {
	__u64 dev;
	__u64 ino;
};

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct nsinfo);
} ns_map SEC(".maps");

static __always_inline void count(__u32 mech)
{
	__u32 zero = 0;
	struct nsinfo *ns = bpf_map_lookup_elem(&ns_map, &zero);
	if (!ns)
		return;
	struct bpf_pidns_info ids = {};
	if (bpf_get_ns_current_pid_tgid(ns->dev, ns->ino, &ids, sizeof(ids)))
		return; /* in Specter's PID namespace, like the real program */
	__u32 tgid = ids.tgid;
	if (!bpf_map_lookup_elem(&watch, &tgid))
		return;
	struct key k = {.mech = mech, .tgid = tgid};
	__u64 one = 1;
	__u64 *v = bpf_map_lookup_elem(&hits, &k);
	if (v)
		__sync_fetch_and_add(v, 1);
	else
		bpf_map_update_elem(&hits, &k, &one, BPF_NOEXIST);
}

/* 1: the syscall tracepoint of write, which Specter uses today */
SEC("tracepoint/syscalls/sys_enter_write")
int m1(void *ctx)
{
	count(1);
	return 0;
}

/* 2: the BTF raw tracepoint sys_enter, for the write system call */
SEC("tp_btf/sys_enter")
int BPF_PROG(m2, struct pt_regs *regs, long id)
{
	__u32 zero = 0;
	__u64 *nr = bpf_map_lookup_elem(&nr_write, &zero);
	if (nr && id == (long)*nr)
		count(2);
	return 0;
}

/* 3: a kprobe on vfs_write, which every write(2) of any task goes through */
SEC("kprobe/vfs_write")
int m3(void *ctx)
{
	count(3);
	return 0;
}

/* 4: a kprobe on the system call entry function, as BCC does it. The name depends on the architecture. */
SEC("kprobe/__x64_sys_write")
int m4x(void *ctx)
{
	count(4);
	return 0;
}

SEC("kprobe/__arm64_sys_write")
int m4a(void *ctx)
{
	count(4);
	return 0;
}

/* 5: the raw_syscalls:sys_enter tracepoint, for the write system call */
struct raw_sys_enter_ctx {
	__u64 common;
	long id;
	unsigned long args[6];
};

SEC("tracepoint/raw_syscalls/sys_enter")
int m5(struct raw_sys_enter_ctx *ctx)
{
	__u32 zero = 0;
	__u64 *nr = bpf_map_lookup_elem(&nr_write, &zero);
	if (nr && ctx->id == (long)*nr)
		count(5);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
