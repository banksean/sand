package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

func TestDefaultClientCreateSandbox(t *testing.T) {
	t.Run("streams progress and returns sandbox", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/create-stream" {
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			if err := enc.Encode(CreateSandboxEvent{Type: "progress", Data: "step 1\n"}); err != nil {
				t.Fatal(err)
			}
			if err := enc.Encode(CreateSandboxEvent{Type: "progress", Data: "step 2\n"}); err != nil {
				t.Fatal(err)
			}
			if err := enc.Encode(CreateSandboxEvent{
				Type: "result",
				Box:  &testSandboxBox,
			}); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		client := &defaultClient{
			base:       srv.URL,
			httpClient: srv.Client(),
		}

		var progress bytes.Buffer
		box, err := client.CreateSandbox(context.Background(), CreateSandboxOpts{ID: "test-box"}, &progress)
		if err != nil {
			t.Fatalf("CreateSandbox() error = %v", err)
		}
		if box == nil {
			t.Fatal("CreateSandbox() returned nil box")
		}
		if box.ID != testSandboxBox.ID {
			t.Fatalf("CreateSandbox() box ID = %q, want %q", box.ID, testSandboxBox.ID)
		}
		if got := progress.String(); got != "step 1\nstep 2\n" {
			t.Fatalf("CreateSandbox() progress = %q, want %q", got, "step 1\nstep 2\n")
		}
	})

	t.Run("returns streamed error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			if err := enc.Encode(CreateSandboxEvent{Type: "progress", Data: "starting\n"}); err != nil {
				t.Fatal(err)
			}
			if err := enc.Encode(CreateSandboxEvent{Type: "error", Error: "bootstrap failed"}); err != nil {
				t.Fatal(err)
			}
		}))
		defer srv.Close()

		client := &defaultClient{
			base:       srv.URL,
			httpClient: srv.Client(),
		}

		var progress bytes.Buffer
		box, err := client.CreateSandbox(context.Background(), CreateSandboxOpts{ID: "test-box"}, &progress)
		if err == nil {
			t.Fatal("CreateSandbox() error = nil, want error")
		}
		if box != nil {
			t.Fatalf("CreateSandbox() box = %#v, want nil", box)
		}
		if !strings.Contains(err.Error(), "bootstrap failed") {
			t.Fatalf("CreateSandbox() error = %q, want streamed error", err)
		}
		if got := progress.String(); got != "starting\n" {
			t.Fatalf("CreateSandbox() progress = %q, want %q", got, "starting\n")
		}
	})
}

func TestDefaultClientStartSandbox(t *testing.T) {
	var gotReq StartSandboxRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/start" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(StatusResponse{Status: "ok"})
	}))
	defer srv.Close()

	client := &defaultClient{
		base:       srv.URL,
		httpClient: srv.Client(),
	}

	if err := client.StartSandbox(context.Background(), StartSandboxOpts{
		ID:       "test-box",
		SSHAgent: true,
	}); err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}

	if gotReq.ID != "test-box" {
		t.Fatalf("StartSandbox() request ID = %q, want %q", gotReq.ID, "test-box")
	}
	if !gotReq.SSHAgent {
		t.Fatal("StartSandbox() request SSHAgent = false, want true")
	}
}

func TestDefaultClientResolveAgentLaunchEnv(t *testing.T) {
	var gotReq ResolveAgentLaunchEnvRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/resolve-agent-env" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(ResolveAgentLaunchEnvResponse{
			Env: map[string]string{"OPENAI_API_KEY": "sk-test"},
		})
	}))
	defer srv.Close()

	client := &defaultClient{
		base:       srv.URL,
		httpClient: srv.Client(),
	}

	env, err := client.ResolveAgentLaunchEnv(context.Background(), "codex", "/tmp/test.env")
	if err != nil {
		t.Fatalf("ResolveAgentLaunchEnv() error = %v", err)
	}
	if gotReq.Agent != "codex" {
		t.Fatalf("ResolveAgentLaunchEnv() request agent = %q, want %q", gotReq.Agent, "codex")
	}
	if gotReq.EnvFile != "/tmp/test.env" {
		t.Fatalf("ResolveAgentLaunchEnv() request envFile = %q, want %q", gotReq.EnvFile, "/tmp/test.env")
	}
	if env["OPENAI_API_KEY"] != "sk-test" {
		t.Fatalf("ResolveAgentLaunchEnv() env = %+v, want OPENAI_API_KEY", env)
	}
}

var testSandboxBox = sandBox()

func sandBox() sandtypes.Box {
	return sandtypes.Box{
		ID:          "test-box",
		ContainerID: "ctr-test-box",
		ImageName:   "test-image:latest",
	}
}
