package main

// slowdownNotice is the one line printed when the strace source attaches. strace stops the target at every
// system call, so the target runs slower for as long as Specter is attached.
const slowdownNotice = "strace slows the target while Specter is attached. Use it on development and test servers, and use --duration to limit the time."

// slowdownNoticeFor returns the slowdown notice for a source, or "" when that source does not slow the target.
// Only strace does: eBPF, Docker, the journal and file tailing only read what the target already wrote. When strace
// was chosen because the fast engine could not run, note says why and how to get it, and ends the same line.
func slowdownNoticeFor(sourceName, note string) string {
	if sourceName != "strace" {
		return ""
	}
	if note == "" {
		return slowdownNotice
	}
	return slowdownNotice + " " + note
}
