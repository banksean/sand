// package options defines structs for the flagsets passed to various `container` commands.
package hostops

// CreateContainer are the options flags for the "container" cli commands dealing with container instances.
type CreateContainer struct {
	ProcessOptions
	ResourceOptions
	ManagementOptions
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

type DeleteContainer struct {
	// Force forces the removal of one or more running containers
	Force bool `flag:"--force"`
	// All removes all containers
	All bool `flag:"--all"`
}

// ExecContainer runs a new command in a running container.
type ExecContainer struct {
	ProcessOptions
}

type ExportContainer struct {
	Output string `flag:"--output"`
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
	// InitImage is the OCI image to use as the VM init process (replaces vminitd)
	InitImage string `flag:"--init-image"`
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
	// Volume adds a --volume argument to the container runtime.
	Volume []string `flag:"--volume"`
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
	// Unlimit sets resource limits (format: <type>=<soft>[:<hard>])
	ULimit []string `flag:"--ulimit"`
}
