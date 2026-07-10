//go:build darwin && cgo

package hostops

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/xpc"
)

func TestNullStdioUsesDevNull(t *testing.T) {
	stdio, cleanup, err := nullStdio()
	if err != nil {
		t.Fatalf("nullStdio() error = %v", err)
	}
	defer cleanup()

	for i, file := range stdio {
		if file == nil {
			t.Fatalf("stdio[%d] is nil", i)
		}
		if file.Name() != os.DevNull {
			t.Fatalf("stdio[%d].Name() = %q, want %q", i, file.Name(), os.DevNull)
		}
	}
}

func TestProcessFilesNonTerminalDoesNotUseCallerFilesForOutputPipes(t *testing.T) {
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()

	stdio, cleanup, err := processFiles(stdin, nil, nil, false)
	if err != nil {
		t.Fatalf("processFiles() error = %v", err)
	}
	defer cleanup()

	if stdio[0] != stdin {
		t.Fatalf("stdin file was not passed through for non-terminal exec")
	}
	if stdio[1] == nil {
		t.Fatalf("stdout pipe is nil")
	}
	if stdio[2] == nil {
		t.Fatalf("stderr pipe is nil")
	}
}

func TestProcessFilesNilStdinUsesDevNull(t *testing.T) {
	stdio, cleanup, err := processFiles(nil, nil, nil, false)
	if err != nil {
		t.Fatalf("processFiles() error = %v", err)
	}
	defer cleanup()

	if stdio[0] == nil {
		t.Fatal("stdin is nil")
	}
	if stdio[0].Name() != os.DevNull {
		t.Fatalf("stdin.Name() = %q, want %q", stdio[0].Name(), os.DevNull)
	}
}

func TestSameWriterDetectsSharedComparableWriter(t *testing.T) {
	var buf bytes.Buffer
	if !sameWriter(&buf, &buf) {
		t.Fatal("sameWriter() = false, want true for shared buffer")
	}
	var other bytes.Buffer
	if sameWriter(&buf, &other) {
		t.Fatal("sameWriter() = true, want false for separate buffers")
	}
}

func TestApplyExecOptionsWrapsCommandWithShell(t *testing.T) {
	cfg := xpc.ProcessConfiguration{
		Executable: "/bin/zsh",
		Arguments:  []string{"-l"},
	}

	if err := applyExecOptions(&cfg, ProcessOptions{WorkDir: "/app"}, "missing-command", []string{"--flag"}); err != nil {
		t.Fatalf("applyExecOptions() error = %v", err)
	}

	if cfg.Executable != "/bin/sh" {
		t.Fatalf("Executable = %q, want /bin/sh", cfg.Executable)
	}
	wantArgs := []string{"-c", `exec "$0" "$@"`, "missing-command", "--flag"}
	if !reflect.DeepEqual(cfg.Arguments, wantArgs) {
		t.Fatalf("Arguments = %#v, want %#v", cfg.Arguments, wantArgs)
	}
	if cfg.WorkingDirectory != "/app" {
		t.Fatalf("WorkingDirectory = %q, want /app", cfg.WorkingDirectory)
	}
}
