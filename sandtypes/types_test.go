package sandtypes

import (
	"testing"
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
