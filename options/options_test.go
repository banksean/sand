package options

import (
	"reflect"
	"testing"
)

func TestToFlags(t *testing.T) {
	tests := map[string]struct {
		s        any
		expected []string
	}{
		"empty": {
			s:        ManagementOptions{},
			expected: nil,
		},
		"arch": {
			s: ManagementOptions{
				Arch: "arm64",
			},
			expected: []string{
				"--arch", "arm64",
			},
		},
		"arch and detach": {
			s: ManagementOptions{
				Arch:   "arm64",
				Detach: true,
			},
			expected: []string{
				"--arch", "arm64",
				"--detach", // bools don't get a value, just include the flag name.
			},
		},
		"logs": {
			s: ContainerLogs{
				Boot: true,
				N:    100,
			},
			expected: []string{
				"--boot",
				"-n", "100",
			},
		},
		"env": {
			s: ProcessOptions{
				Env: map[string]string{
					"a": "1",
					"b": "2",
					"d": "3",
					"c": "4",
				},
			},
			expected: []string{
				"--env", "a=1,b=2,c=4,d=3",
			},
		},
		"container run": {
			s: RunContainer{
				ProcessOptions: ProcessOptions{
					Interactive: true,
				},
				ManagementOptions: ManagementOptions{
					Remove: true,
					Volume: "/foo/bar:/gorunac/dev",
				},
			},
			expected: []string{
				"--interactive",
				"--remove",
				"--volume", "/foo/bar:/gorunac/dev",
			},
		},
		"create container": {
			s: CreateContainer{
				ManagementOptions: ManagementOptions{
					Mount: []string{
						"type=bind,source=/Users/seanmccullough/sandboxen/59edaa35-1cbb-4914-a478-606ae706f324,target=/app",
						"type=bind,source=/Users/seanmccullough/.claude,target=/home/node/.claude,readonly",
					},
				},
			},
			expected: []string{
				"--mount", "type=bind,source=/Users/seanmccullough/sandboxen/59edaa35-1cbb-4914-a478-606ae706f324,target=/app",
				"--mount", "type=bind,source=/Users/seanmccullough/.claude,target=/home/node/.claude,readonly",
			},
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			got := ToArgs(testCase.s)
			if !reflect.DeepEqual(got, testCase.expected) {
				t.Errorf("got %v, want %v", got, testCase.expected)
			}
		})
	}
}
