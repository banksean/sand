package sandtypes

import (
	"context"
	"errors"
	"testing"

	"github.com/banksean/sand/applecontainer/types"
)

func TestMountSpec_String(t *testing.T) {
	tests := []struct {
		name string
		spec MountSpec
		want string
	}{
		{
			name: "readonly mount",
			spec: MountSpec{Source: "/host/path", Target: "/container/path", ReadOnly: true},
			want: "type=bind,source=/host/path,target=/container/path,readonly",
		},
		{
			name: "readwrite mount",
			spec: MountSpec{Source: "/host/rw", Target: "/container/rw", ReadOnly: false},
			want: "type=bind,source=/host/rw,target=/container/rw",
		},
		{
			name: "mount with spaces",
			spec: MountSpec{Source: "/host/path with spaces", Target: "/container/target", ReadOnly: false},
			want: "type=bind,source=/host/path with spaces,target=/container/target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.String(); got != tt.want {
				t.Errorf("MountSpec.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerHook(t *testing.T) {
	t.Run("hook name and execution", func(t *testing.T) {
		called := false
		var capturedBox BoxOperations

		hook := NewContainerStartupHook("test-hook", func(ctx context.Context, b BoxOperations) error {
			called = true
			capturedBox = b
			return nil
		})

		if hook.Name() != "test-hook" {
			t.Errorf("Expected name 'test-hook', got %s", hook.Name())
		}

		ctx := context.Background()
		box := &mockBoxOperations{id: "test"}
		if err := hook.OnStart(ctx, box); err != nil {
			t.Errorf("OnStart() error = %v", err)
		}

		if !called {
			t.Error("Hook function was not called")
		}
		if capturedBox == nil {
			t.Error("Hook received nil box")
		}
		// Type assertion to check if we got the right box instance
		if mockBox, ok := capturedBox.(*mockBoxOperations); !ok || mockBox != box {
			t.Error("Hook received wrong box")
		}
	})

	t.Run("hook returns error", func(t *testing.T) {
		expectedErr := errors.New("hook error")
		hook := NewContainerStartupHook("failing-hook", func(ctx context.Context, b BoxOperations) error {
			return expectedErr
		})

		ctx := context.Background()
		box := &mockBoxOperations{id: "test"}
		err := hook.OnStart(ctx, box)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

// mockBoxOperations is a minimal implementation of BoxOperations for testing
type mockBoxOperations struct {
	id string
}

func (m *mockBoxOperations) Exec(ctx context.Context, cmd string, args ...string) (string, error) {
	return "", nil
}

func (m *mockBoxOperations) GetContainer(ctx context.Context) (*types.Container, error) {
	return &types.Container{}, nil
}
