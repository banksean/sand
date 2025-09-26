package options

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type SystemStatus struct {
	Prefix string `flag:"--prefix"` // Launchd prefix for services (default: com.apple.container.)
	Debug  bool   `flag:"--debug"`  // Enable debug output [environment: CONTAINER_DEBUG]
}

type SystemStart struct {
	AppRoot              string `flag:"--app-root"`               // Path to the root directory for application data
	InstallRoot          string `flag:"--install-root"`           // Path to the root directory for application executables and plugins
	EnableKernelIsntall  bool   `flag:"--enable-kernel-install"`  // Specify whether the default kernel should be installed or not (default: prompt user)
	DisableKernelIsntall bool   `flag:"--disable-kernel-install"` // Specify whether the default kernel should be installed or not (default: prompt user)
	Debug                bool   `flag:"--debug"`                  // Enable debug output [environment: CONTAINER_DEBUG]

}

type SystemStop struct {
	Prefix string `flag:"--prefix"` // Launchd prefix for services (default: com.apple.container.)
	Debug  bool   `flag:"--debug"`  // Enable debug output [environment: CONTAINER_DEBUG]
}

type SystemLogs struct {
	Follow bool   `flag:"--follow"` // Follow log output
	Last   string `flag:"--last"`   // Fetch logs starting from the specified time period (minus the current time); supported formats: m, h, d (default: 5m)
	Debug  bool   `flag:"--debug"`  // Enable debug output [environment: CONTAINER_DEBUG]
}

// CreateContainer are the options flags for the "container" cli commands dealing with container instances.
type CreateContainer struct {
	ProcessOptions
	ResourceOptions
	ManagementOptions
}

type ContainerLogs struct {
	Boot   bool `flag:"--boot"`   // Display the boot log for the container instead of stdio
	Follow bool `flag:"--follow"` // Follow log output
	N      int  `flag:"-n"`       // Number of lines to show from the end of the logs. If not provided this will print all of the
	Debug  bool `flag:"--debug"`  // Enable debug output [environment: CONTAINER_DEBUG]
}

type StartContainer struct {
	Attach      bool `flag:"--attach"`      // Attach STDOUT/STDERR
	Interactive bool `flag:"--interactive"` // Attach STDIN
	Debug       bool `flag:"--debug"`       // Enable debug output [environment: CONTAINER_DEBUG]
}

type StopContainer struct {
	All    bool   `flag:"--all"`    // Stop all running containers
	Signal string `flag:"--signal"` // Signal to send the containers (default: SIGTERM)
	Time   int    `flag:"--time"`   // Seconds to wait before killing the containers (default: 5)
	Debug  bool   `flag:"--debug"`  // Enable debug output [environment: CONTAINER_DEBUG]
}

type ManagementOptions struct {
	Arch           string            `flag:"--arch"`           // Set arch if image can target multiple architectures (default: arm64)
	CIDFile        string            `flag:"--cidfile"`        // Write the container ID to the path provided
	Detach         bool              `flag:"--detach"`         // Run the container and detach from the process
	DNS            string            `flag:"--dns"`            // DNS nameserver IP address
	DNSDomain      string            `flag:"--dns-domain"`     // Default DNS domain
	DNSOption      string            `flag:"--dns-option"`     // DNS options
	DNSSearch      string            `flag:"--dns-search"`     // DNS search domains
	Entrypoint     string            `flag:"--entrypoint"`     // Override the entrypoint of the image
	Kernel         string            `flag:"--kernel"`         // Set a custom kernel path
	Label          map[string]string `flag:"--label"`          // Add a key=value label to the container
	Mount          string            `flag:"--mount"`          // Add a mount to the container (format: type=<>,source=<>,target=<>,readonly)
	Name           string            `flag:"--name"`           // Use the specified name as the container ID
	Netowrk        string            `flag:"--network"`        // Attach the container to a network
	NoDNS          bool              `flag:"--no-dns"`         // Do not configure DNS in the container
	OS             string            `flag:"--os"`             // Set OS if image can target multiple operating systems (default: linux)
	Publish        string            `flag:"--publish"`        // Publish a port from container to host (format: [host-ip:]host-port:container-port[/protocol])
	Platform       string            `flag:"--platform"`       // Platform for the image if it's multi-platform. This takes precedence over --os and --arch
	PublishSocket  string            `flag:"--publish-socket"` // Publish a socket from container to host (format: host_path:container_path)
	Remove         bool              `flag:"--remove"`         // Remove the container after it stops
	SSH            bool              `flag:"--ssh"`            // Forward SSH agent socket to container
	TmpFS          string            `flag:"--tmpfs"`          // Add a tmpfs mount to the container at the given path
	Volume         string            `flag:"--volume"`         // Bind mount a volume into the container
	Virtualization bool              `flag:"--virtualization"` // Expose virtualization capabilities to the container (requires host and guest support)
}

type ResourceOptions struct {
	CPUs   int    `flag:"--cpus"`   // Number of CPUs to allocate to the container
	Memory string `flag:"--memory"` // Amount of memory (1MiByte granularity), with optional K, M, G, T, or P suffix
}

type ProcessOptions struct {
	Env         map[string]string `flag:"--env"`         // Set environment variables (format: key=value)
	EnvFile     string            `flag:"--env-file"`    // Read in a file of environment variables (key=value format, ignores # comments and blank lines)
	GID         string            `flag:"--gid"`         // Set the group ID for the process
	Interactive bool              `flag:"--interactive"` // Keep the standard input open even if not attached
	TTY         bool              `flag:"--tty"`         // Open a TTY with the process
	User        string            `flag:"--user"`        // Set the user for the process (format: name|uid[:gid])
	UID         string            `flag:"--uid"`         // Set the user ID for the process
	WorkDir     string            `flag:"--workdir"`     // Set the initial working directory inside the container
}

// ToArgs creates an array of strings that you can pass to exec.Command(...) as CLI args.
func ToArgs(s any) []string {
	var ret []string
	st := reflect.TypeOf(s)
	for i := range st.NumField() {
		field := st.Field(i)
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
		sv := reflect.ValueOf(s)
		fv := sv.Field(i)
		v := reflect.ValueOf(fv.Interface())
		if !keepZero && v.IsZero() {
			continue
		}
		if ret == nil {
			ret = []string{}
		}
		flagValue := ""
		fieldKind := field.Type.Kind()
		if fieldKind == reflect.Map {
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
