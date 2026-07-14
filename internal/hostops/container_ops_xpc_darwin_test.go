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

func TestParsePublishPort(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		hostAddress   xpc.IPAddress
		hostPort      uint16
		containerPort uint16
		proto         xpc.PublishProtocol
	}{
		{
			name:          "host and container port",
			spec:          "3000:3000/tcp",
			hostPort:      3000,
			containerPort: 3000,
			proto:         xpc.PublishProtocolTCP,
		},
		{
			name:          "host address",
			spec:          "127.0.0.1:3128:3128/tcp",
			hostAddress:   "127.0.0.1",
			hostPort:      3128,
			containerPort: 3128,
			proto:         xpc.PublishProtocolTCP,
		},
		{
			name:          "udp",
			spec:          "5353:5353/udp",
			hostPort:      5353,
			containerPort: 5353,
			proto:         xpc.PublishProtocolUDP,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePublishPort(tt.spec)
			if err != nil {
				t.Fatalf("parsePublishPort: %v", err)
			}
			if got.HostAddress != tt.hostAddress || got.HostPort != tt.hostPort || got.ContainerPort != tt.containerPort || got.Proto != tt.proto || got.Count != 1 {
				t.Fatalf("parsePublishPort(%q) = %+v", tt.spec, got)
			}
		})
	}
}

func TestXPCSnapshotToContainerPreservesLabelsAndImage(t *testing.T) {
	got := xpcSnapshotToContainer(xpc.ContainerSnapshot{
		Status: xpc.RuntimeStatus("running"),
		Configuration: xpc.ContainerConfiguration{
			ID: "sand-http-cache",
			Labels: map[string]string{
				"sand.service":         "http-proxy",
				"sand.service.version": "1",
			},
			Image: xpc.ImageDescription{
				Reference: "ubuntu/squid:6.6-24.04_beta",
				Descriptor: xpc.Descriptor{
					Digest:    "sha256:test",
					Size:      42,
					MediaType: "application/vnd.oci.image.manifest.v1+json",
				},
			},
		},
	})

	if got.Configuration.Labels["sand.service"] != "http-proxy" {
		t.Fatalf("labels = %#v", got.Configuration.Labels)
	}
	if got.Configuration.Image.Reference != "ubuntu/squid:6.6-24.04_beta" {
		t.Fatalf("image reference = %q", got.Configuration.Image.Reference)
	}
	if got.Configuration.Image.Descriptor.Digest != "sha256:test" {
		t.Fatalf("image descriptor = %+v", got.Configuration.Image.Descriptor)
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
