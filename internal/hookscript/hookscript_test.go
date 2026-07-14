package hookscript

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type fakeStreamer struct {
	execResults map[string]fakeResult
	calls       []string
	inputs      map[string]string
}

type fakeResult struct {
	out string
	err error
}

func (f *fakeStreamer) Exec(ctx context.Context, cmd string, args ...string) (string, error) {
	key := commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	if res, ok := f.execResults[key]; ok {
		return res.out, res.err
	}
	return "", nil
}

func (f *fakeStreamer) ExecStream(ctx context.Context, stdout, stderr io.Writer, cmd string, args ...string) error {
	key := "stream:" + commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	return nil
}

func (f *fakeStreamer) ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cmd string, args ...string) error {
	key := "stream-input:" + commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		if f.inputs == nil {
			f.inputs = make(map[string]string)
		}
		f.inputs[key] = string(data)
	}
	return nil
}

func TestExecuteUsesContainerCommandsAndConditions(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"which git":           {out: "/usr/bin/git"},
			"which missing":       {err: errors.New("not found")},
			"test -e /tmp/socket": {out: "ok"},
		},
	}

	err := Execute(context.Background(), exec, "test.txt", `
[cmd:git] exec git status
[!cmd:missing] exec echo missing
[exists:/tmp/socket] exec chmod 666 /tmp/socket
`, io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{
		"which git",
		"git status",
		"which missing",
		"echo missing",
		"test -e /tmp/socket",
		"chmod 666 /tmp/socket",
	}
	if !reflect.DeepEqual(exec.calls, want) {
		t.Fatalf("calls = %#v, want %#v", exec.calls, want)
	}
}

func TestExecuteRejectsDefaultHostCommands(t *testing.T) {
	exec := &fakeStreamer{}

	err := Execute(context.Background(), exec, "test.txt", "mkdir host-dir\n", io.Discard)
	if err == nil {
		t.Fatal("Execute() error = nil, want unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Execute() error = %v, want unknown command", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("host command reached streamer: %#v", exec.calls)
	}
}

func TestWriteManagedBazelrcReplacesManagedBlock(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"cat /home/user/.bazelrc": {out: "build --keep=1\n" +
				bazelrcManagedStart + "\n" +
				"build --remote_cache=http://old\n" +
				bazelrcManagedEnd + "\n" +
				"common --keep=2\n"},
		},
	}

	err := Execute(context.Background(), exec, "bazelrc.txt", "write-managed-bazelrc /home/user/.bazelrc http://new-cache:8080\n", io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := exec.inputs["stream-input:tee /home/user/.bazelrc.sand.tmp"]
	for _, want := range []string{
		"build --keep=1\n",
		"common --keep=2\n",
		"build --remote_cache=http://new-cache:8080\n",
		"build --experimental_guard_against_concurrent_changes\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("written bazelrc missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "http://old") {
		t.Fatalf("written bazelrc kept old managed block: %q", got)
	}
}

func TestExecuteErrorIncludesScriptLine(t *testing.T) {
	expected := errors.New("git failed")
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"git status": {err: expected},
		},
	}

	err := Execute(context.Background(), exec, "hooks.txt", "exec git status\n", io.Discard)
	if !errors.Is(err, expected) {
		t.Fatalf("Execute() error = %v, want %v", err, expected)
	}
	if !strings.Contains(err.Error(), "hooks.txt:1:") {
		t.Fatalf("Execute() error = %v, want script line", err)
	}
}

func commandKey(cmd string, args ...string) string {
	return strings.Join(append([]string{cmd}, args...), " ")
}
