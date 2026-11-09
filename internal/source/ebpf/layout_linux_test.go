//go:build linux

package ebpf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The decoder reads the ring buffer record by fixed offsets. This checks them against the Go types that bpf2go
// generated from the C structs, so a change to struct event in bpf/specter.c cannot silently break the decoder.
func TestDecoderMatchesTheGeneratedEventType(t *testing.T) {
	if got := binary.Size(specterEvent{}); got != headerSize {
		t.Fatalf("struct event is %d bytes in C, but the decoder expects %d", got, headerSize)
	}

	sent := specterEvent{Tid: 11, Tgid: 22, Fd: 33, Len: 3, Kind: kindFile, Arg: 55, Mode: 0o100644, Dev: 77, Ino: 88, TsNs: 99}
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, sent); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("abc")

	ev, ok := decodeEvent(buf.Bytes())
	if !ok {
		t.Fatal("the record was rejected")
	}
	if ev.Tid != 11 || ev.Tgid != 22 || ev.FD != 33 || ev.Len != 3 || ev.Kind != kindFile || ev.Arg != 55 ||
		ev.Mode != 0o100644 || ev.Dev != 77 || ev.Ino != 88 || ev.TS != 99 || string(ev.Data) != "abc" {
		t.Errorf("decoded %+v from %+v", ev, sent)
	}
}

func TestKindsMatchTheProgram(t *testing.T) {
	// EV_DATA 0, EV_FILE 1, EV_MARKER 2, EV_EXIT 3 in bpf/specter.c
	if kindData != 0 || kindFile != 1 || kindMarker != 2 || kindExit != 3 {
		t.Error("the kinds in event.go no longer match EV_* in bpf/specter.c")
	}
}
