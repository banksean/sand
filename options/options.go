// package options defines structs for the flagsets passed to various `container` commands.
package options

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type SystemStatus struct {
	// Prefix is the launchd prefix for services (default: com.apple.container.)
	Prefix string `flag:"--prefix"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type SystemStart struct {
	// AppRoot is the path to the root directory for application data
	AppRoot string `flag:"--app-root"`
	// InstallRoot is the path to the root directory for application executables and plugins
	InstallRoot string `flag:"--install-root"`
	// EnableKernelIsntall specifies whether the default kernel should be installed or not (default: prompt user)
	EnableKernelIsntall bool `flag:"--enable-kernel-install"`
	// DisableKernelIsntall specifies whether the default kernel should be installed or not (default: prompt user)
	DisableKernelIsntall bool `flag:"--disable-kernel-install"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type SystemStop struct {
	// Prefix is the launchd prefix for services (default: com.apple.container.)
	Prefix string `flag:"--prefix"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type SystemLogs struct {
	// Follow enables following log output
	Follow bool `flag:"--follow"`
	// Last specifies fetching logs starting from the specified time period (minus the current time); supported formats: m, h, d (default: 5m)
	Last string `flag:"--last"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

// CreateContainer are the options flags for the "container" cli commands dealing with container instances.
type CreateContainer struct {
	ProcessOptions
	ResourceOptions
	ManagementOptions
}

type ContainerLogs struct {
	// Boot displays the boot log for the container instead of stdio
	Boot bool `flag:"--boot"`
	// Follow enables following log output
	Follow bool `flag:"--follow"`
	// N is the number of lines to show from the end of the logs. If not provided this will print all of the logs
	N int `flag:"-n"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type StartContainer struct {
	// Attach enables attaching STDOUT/STDERR
	Attach bool `flag:"--attach"`
	// Interactive enables attaching STDIN
	Interactive bool `flag:"--interactive"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type StopContainer struct {
	// All stops all running containers
	All bool `flag:"--all"`
	// Signal is the signal to send the containers (default: SIGTERM)
	Signal string `flag:"--signal"`
	// Time is the seconds to wait before killing the containers (default: 5)
	Time int `flag:"--time"`
	// Debug enables debug output [environment: CONTAINER_DEBUG]
	Debug bool `flag:"--debug"`
}

type KillContainer struct {
	// All kills all running containers
	All bool `flag:"--all"`
	// Signal is the signal to send the containers (default: SIGTERM)
	Signal string `flag:"--signal"`
}

type DeleteContainer struct {
	// Force forces the removal of one or more running containers
	Force bool `flag:"--force"`
	// All removes all containers
	All bool `flag:"--all"`
}

type RunContainer struct {
	ProcessOptions
	ResourceOptions
	ManagementOptions
	// Scheme is the scheme to use when connecting to the container registry (http, https, auto) (default: auto)
	Scheme string `flag:"--scheme"`
	// DisableProgressUpdates disables progress bar updates
	DisableProgressUpdates bool `flag:"--disable-progress-updates"`
}

// ExecContainer runs a new command in a running container.
type ExecContainer struct {
	ProcessOptions
}

type ManagementOptions struct {
	// Arch sets arch if image can target multiple architectures (default: arm64)
	Arch string `flag:"--arch"`
	// CIDFile writes the container ID to the path provided
	CIDFile string `flag:"--cidfile"`
	// Detach runs the container and detaches from the process
	Detach bool `flag:"--detach"`
	// DNS is the DNS nameserver IP address
	DNS string `flag:"--dns"`
	// DNSDomain is the default DNS domain
	DNSDomain string `flag:"--dns-domain"`
	// DNSOption specifies DNS options
	DNSOption string `flag:"--dns-option"`
	// DNSSearch specifies DNS search domains
	DNSSearch string `flag:"--dns-search"`
	// Entrypoint overrides the entrypoint of the image
	Entrypoint string `flag:"--entrypoint"`
	// Kernel sets a custom kernel path
	Kernel string `flag:"--kernel"`
	// Label adds a key=value label to the container
	Label map[string]string `flag:"--label"`
	// Mount adds a mount to the container (format: type=<>,source=<>,target=<>,readonly)
	Mount []string `flag:"--mount"`
	// Name uses the specified name as the container ID
	Name string `flag:"--name"`
	// Netowrk attaches the container to a network
	Netowrk string `flag:"--network"`
	// NoDNS disables DNS configuration in the container
	NoDNS bool `flag:"--no-dns"`
	// OS sets OS if image can target multiple operating systems (default: linux)
	OS string `flag:"--os"`
	// Publish publishes a port from container to host (format: [host-ip:]host-port:container-port[/protocol])
	Publish string `flag:"--publish"`
	// Platform is the platform for the image if it's multi-platform. This takes precedence over --os and --arch
	Platform string `flag:"--platform"`
	// PublishSocket publishes a socket from container to host (format: host_path:container_path)
	PublishSocket string `flag:"--publish-socket"`
	// Remove removes the container after it stops
	Remove bool `flag:"--remove"`
	// SSH forwards SSH agent socket to container
	SSH bool `flag:"--ssh"`
	// TmpFS adds a tmpfs mount to the container at the given path
	TmpFS string `flag:"--tmpfs"`
	// Volume bind mounts a volume into the container
	Volume string `flag:"--volume"`
	// Virtualization exposes virtualization capabilities to the container (requires host and guest support)
	Virtualization bool `flag:"--virtualization"`
}

type ResourceOptions struct {
	// CPUs is the number of CPUs to allocate to the container
	CPUs int `flag:"--cpus"`
	// Memory is the amount of memory (1MiByte granularity), with optional K, M, G, T, or P suffix
	Memory string `flag:"--memory"`
}

type ProcessOptions struct {
	// Env sets environment variables (format: key=value)
	Env map[string]string `flag:"--env"`
	// EnvFile reads in a file of environment variables (key=value format, ignores # comments and blank lines)
	EnvFile string `flag:"--env-file"`
	// GID sets the group ID for the process
	GID string `flag:"--gid"`
	// Interactive keeps the standard input open even if not attached
	Interactive bool `flag:"--interactive"`
	// TTY opens a TTY with the process
	TTY bool `flag:"--tty"`
	// User sets the user for the process (format: name|uid[:gid])
	User string `flag:"--user"`
	// UID sets the user ID for the process
	UID string `flag:"--uid"`
	// WorkDir sets the initial working directory inside the container
	WorkDir string `flag:"--workdir"`
}

type BuildOptions struct {
	// CPUs is the number of CPUs to allocate to the container (default: 2)
	CPUs int `flag:"--cpus"`
	// Memory is the amount of memory in bytes, kilobytes (K), megabytes (M), or gigabytes (G) for the container, with MB granularity (default: 2048MB)
	Memory string `flag:"--memory"`
	// BuildArg sets build-time variables (format: key=value)
	BuildArg map[string]string `flag:"--build-arg"`
	// File is the path to Dockerfile (default: Dockerfile)
	File string `flag:"--file"`
	// Label sets a label (format: key=value)
	Label map[string]string `flag:"--label"`
	// NoCache disables cache usage
	NoCache bool `flag:"--no-cache"`
	// Output is the output configuration for the build (default: type=oci)
	Output string `flag:"--output"`
	// Platform adds the platform to the build
	Platform string `flag:"--platform"`
	// OS adds the OS type to the build
	OS string `flag:"--os"`
	// Arch adds the architecture type to the build
	Arch string `flag:"--arch"`
	// Progress is the progress type - one of [auto|plain|tty] (default: auto)
	Progress string `flag:"--progress"`
	// VsockPort is the builder-shim vsock port (default: 8088)
	VsockPort int `flag:"--vsock-port"`
	// Tag is the name for the built image
	Tag string `flag:"--tag"`
	// Target sets the target build stage
	Target string `flag:"--target"`
}

// ToArgs creates an array of strings that you can pass to exec.Command(...) as CLI args.
func ToArgs[T any](s *T) []string {
	if s == nil {
		s = new(T)
	}
	var ret []string
	st := reflect.TypeOf(*s)
	sv := reflect.ValueOf(*s)
	if st.Kind() == reflect.Pointer {
		sv = reflect.Indirect(sv)
		st = sv.Type()
	}
	for i := range st.NumField() {
		field := st.Field(i)
		fv := sv.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			fvi := fv.Interface()
			ret = append(ret, ToArgs(&fvi)...)
			continue
		}
		flagTag, ok := field.Tag.Lookup("flag")
		if !ok {
			continue
		}
		flagParts := strings.Split(flagTag, ",")
		flagName := flagParts[0]
		keepZero := false
		if len(flagParts) > 1 {
			if strings.ToLower(flagParts[1]) == "keepZero" {
				keepZero = true
			}
		}
		v := reflect.ValueOf(fv.Interface())

		if !keepZero && v.IsZero() {
			continue
		}
		if ret == nil {
			ret = []string{}
		}
		flagValue := ""
		fieldKind := field.Type.Kind()
		if fieldKind == reflect.Array || fieldKind == reflect.Slice {
			for i := 0; i < fv.Len(); i++ {
				av := fv.Index(i)
				ret = append(ret, flagName)
				ret = append(ret, fmt.Sprintf("%v", av))
			}
			continue
		} else if fieldKind == reflect.Map {
			mapVals := []string{}
			m := v.Interface().(map[string]string)
			keyIter := maps.Keys(m)
			keys := slices.Sorted(keyIter)
			for _, k := range keys {
				v := m[k]
				mapVals = append(mapVals, fmt.Sprintf("%v=%v", k, v))
			}
			flagValue = strings.Join(mapVals, ",")
		} else if fieldKind != reflect.Bool {
			flagValue = fmt.Sprintf("%v", fv.Interface())
		}
		ret = append(ret, flagName)
		if flagValue != "" {
			ret = append(ret, flagValue)
		}
	}
	return ret
}
