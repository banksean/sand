package xpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The types in this file correspond to the Swift Codable types defined under
// https://github.com/apple/container/tree/main/Sources/ContainerResource and
// their transitive ContainerizationOCI value types. They model the JSON payloads
// carried in XPC_TYPE_DATA responses and requests.

var appleReferenceDate = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// AppleTime is Foundation.Date as encoded by Swift JSONEncoder's default
// strategy: seconds since 2001-01-01T00:00:00Z.
type AppleTime struct {
	time.Time
}

func NewAppleTime(t time.Time) AppleTime {
	return AppleTime{Time: t}
}

func (t AppleTime) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(t.Sub(appleReferenceDate).Seconds(), 'f', -1, 64)), nil
}

func (t *AppleTime) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*t = AppleTime{}
		return nil
	}
	var seconds float64
	if err := json.Unmarshal(data, &seconds); err == nil {
		t.Time = appleReferenceDate.Add(time.Duration(seconds * float64(time.Second))).UTC()
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return err
	}
	t.Time = parsed.UTC()
	return nil
}

type RuntimeStatus string

const (
	RuntimeStatusUnknown  RuntimeStatus = "unknown"
	RuntimeStatusStopped  RuntimeStatus = "stopped"
	RuntimeStatusRunning  RuntimeStatus = "running"
	RuntimeStatusStopping RuntimeStatus = "stopping"
)

type NetworkMode string

const (
	NetworkModeNAT      NetworkMode = "nat"
	NetworkModeHostOnly NetworkMode = "hostOnly"
)

type PublishProtocol string

const (
	PublishProtocolTCP PublishProtocol = "tcp"
	PublishProtocolUDP PublishProtocol = "udp"
)

type (
	IPAddress       string
	IPv4Address     string
	CIDRv4          string
	CIDRv6          string
	MACAddress      string
	FilePath        string
	FilePermissions uint32
)

// ContainerSnapshot is a snapshot of a container along with its configuration
// and runtime state information. The Swift type's id and platform fields are
// computed and are not part of its synthesized Codable payload.
type ContainerSnapshot struct {
	Configuration ContainerConfiguration `json:"configuration"`
	Status        RuntimeStatus          `json:"status"`
	Networks      []Attachment           `json:"networks"`
	StartedDate   *AppleTime             `json:"startedDate,omitempty"`
}

func (s ContainerSnapshot) ID() string {
	return s.Configuration.ID
}

func (s ContainerSnapshot) Platform() Platform {
	return s.Configuration.Platform
}

type ContainerStatus struct {
	State       RuntimeStatus `json:"state"`
	Networks    []Attachment  `json:"networks"`
	StartedDate *AppleTime    `json:"startedDate,omitempty"`
}

type ContainerStats struct {
	ID               string  `json:"id"`
	MemoryUsageBytes *uint64 `json:"memoryUsageBytes,omitempty"`
	MemoryLimitBytes *uint64 `json:"memoryLimitBytes,omitempty"`
	CPUUsageUsec     *uint64 `json:"cpuUsageUsec,omitempty"`
	NetworkRxBytes   *uint64 `json:"networkRxBytes,omitempty"`
	NetworkTxBytes   *uint64 `json:"networkTxBytes,omitempty"`
	BlockReadBytes   *uint64 `json:"blockReadBytes,omitempty"`
	BlockWriteBytes  *uint64 `json:"blockWriteBytes,omitempty"`
	NumProcesses     *uint64 `json:"numProcesses,omitempty"`
}

type ContainerConfiguration struct {
	ID               string                    `json:"id"`
	Image            ImageDescription          `json:"image"`
	Mounts           []Filesystem              `json:"mounts"`
	PublishedPorts   []PublishPort             `json:"publishedPorts"`
	PublishedSockets []PublishSocket           `json:"publishedSockets"`
	Labels           map[string]string         `json:"labels"`
	Sysctls          map[string]string         `json:"sysctls"`
	Networks         []AttachmentConfiguration `json:"networks"`
	DNS              *DNSConfiguration         `json:"dns,omitempty"`
	Rosetta          bool                      `json:"rosetta"`
	InitProcess      ProcessConfiguration      `json:"initProcess"`
	Platform         Platform                  `json:"platform"`
	Resources        Resources                 `json:"resources"`
	RuntimeHandler   string                    `json:"runtimeHandler"`
	Virtualization   bool                      `json:"virtualization"`
	SSH              bool                      `json:"ssh"`
	ReadOnly         bool                      `json:"readOnly"`
	UseInit          bool                      `json:"useInit"`
	CapAdd           []string                  `json:"capAdd"`
	CapDrop          []string                  `json:"capDrop"`
	ShmSize          *uint64                   `json:"shmSize,omitempty"`
	StopSignal       *string                   `json:"stopSignal,omitempty"`
	CreationDate     AppleTime                 `json:"creationDate"`
}

func (c *ContainerConfiguration) UnmarshalJSON(data []byte) error {
	type wire ContainerConfiguration
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Mounts == nil {
		v.Mounts = []Filesystem{}
	}
	if v.PublishedPorts == nil {
		v.PublishedPorts = []PublishPort{}
	}
	if v.PublishedSockets == nil {
		v.PublishedSockets = []PublishSocket{}
	}
	if v.Labels == nil {
		v.Labels = map[string]string{}
	}
	if v.Sysctls == nil {
		v.Sysctls = map[string]string{}
	}
	if v.Networks == nil {
		v.Networks = []AttachmentConfiguration{}
	}
	if v.Resources.CPUs == 0 {
		v.Resources.CPUs = 4
	}
	if v.Resources.MemoryInBytes == 0 {
		v.Resources.MemoryInBytes = 1024 * 1024 * 1024
	}
	if v.Resources.CPUOverhead == 0 {
		v.Resources.CPUOverhead = 1
	}
	if v.RuntimeHandler == "" {
		v.RuntimeHandler = "container-runtime-linux"
	}
	if v.CreationDate.IsZero() {
		v.CreationDate = NewAppleTime(time.Unix(0, 0).UTC())
	}
	c.setPlatformDefault(&v.Platform)
	*c = ContainerConfiguration(v)
	return nil
}

func (c ContainerConfiguration) MarshalJSON() ([]byte, error) {
	type wire ContainerConfiguration
	v := wire(c)
	if v.Mounts == nil {
		v.Mounts = []Filesystem{}
	}
	if v.PublishedPorts == nil {
		v.PublishedPorts = []PublishPort{}
	}
	if v.PublishedSockets == nil {
		v.PublishedSockets = []PublishSocket{}
	}
	if v.Labels == nil {
		v.Labels = map[string]string{}
	}
	if v.Sysctls == nil {
		v.Sysctls = map[string]string{}
	}
	if v.Networks == nil {
		v.Networks = []AttachmentConfiguration{}
	}
	if v.RuntimeHandler == "" {
		v.RuntimeHandler = "container-runtime-linux"
	}
	if v.Resources.CPUs == 0 {
		v.Resources.CPUs = 4
	}
	if v.Resources.MemoryInBytes == 0 {
		v.Resources.MemoryInBytes = 1024 * 1024 * 1024
	}
	if v.Resources.CPUOverhead == 0 {
		v.Resources.CPUOverhead = 1
	}
	if v.CreationDate.IsZero() {
		v.CreationDate = NewAppleTime(time.Unix(0, 0).UTC())
	}
	c.setPlatformDefault(&v.Platform)
	return json.Marshal(v)
}

func (c ContainerConfiguration) setPlatformDefault(p *Platform) {
	if p.OS == "" {
		p.OS = "linux"
	}
	if p.Architecture == "" {
		p.Architecture = "arm64"
		p.Variant = "v8"
	}
}

type DNSConfiguration struct {
	Nameservers   []string `json:"nameservers"`
	Domain        *string  `json:"domain,omitempty"`
	SearchDomains []string `json:"searchDomains"`
	Options       []string `json:"options"`
}

type Resources struct {
	CPUs          int     `json:"cpus"`
	MemoryInBytes uint64  `json:"memoryInBytes"`
	Storage       *uint64 `json:"storage,omitempty"`
	CPUOverhead   int     `json:"cpuOverhead"`
}

type ProcessConfiguration struct {
	Executable         string          `json:"executable"`
	Arguments          []string        `json:"arguments"`
	Environment        []string        `json:"environment"`
	WorkingDirectory   string          `json:"workingDirectory"`
	Terminal           bool            `json:"terminal"`
	User               ProcessUser     `json:"user"`
	SupplementalGroups []uint32        `json:"supplementalGroups"`
	Rlimits            []ProcessRlimit `json:"rlimits"`
}

func (p ProcessConfiguration) MarshalJSON() ([]byte, error) {
	type wire ProcessConfiguration
	v := wire(p)
	if v.Arguments == nil {
		v.Arguments = []string{}
	}
	if v.Environment == nil {
		v.Environment = []string{}
	}
	if v.SupplementalGroups == nil {
		v.SupplementalGroups = []uint32{}
	}
	if v.Rlimits == nil {
		v.Rlimits = []ProcessRlimit{}
	}
	return json.Marshal(v)
}

func (p *ProcessConfiguration) UnmarshalJSON(data []byte) error {
	type wire ProcessConfiguration
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Arguments == nil {
		v.Arguments = []string{}
	}
	if v.Environment == nil {
		v.Environment = []string{}
	}
	if v.SupplementalGroups == nil {
		v.SupplementalGroups = []uint32{}
	}
	if v.Rlimits == nil {
		v.Rlimits = []ProcessRlimit{}
	}
	*p = ProcessConfiguration(v)
	return nil
}

type ProcessRlimit struct {
	Limit string `json:"limit"`
	Soft  uint64 `json:"soft"`
	Hard  uint64 `json:"hard"`
}

type ProcessUser struct {
	RawUserString *string
	UID           *uint32
	GID           *uint32
}

func RawProcessUser(user string) ProcessUser {
	return ProcessUser{RawUserString: &user}
}

func IDProcessUser(uid, gid uint32) ProcessUser {
	return ProcessUser{UID: &uid, GID: &gid}
}

func (u ProcessUser) MarshalJSON() ([]byte, error) {
	if u.RawUserString != nil {
		return json.Marshal(map[string]any{"raw": map[string]string{"userString": *u.RawUserString}})
	}
	uid, gid := uint32(0), uint32(0)
	if u.UID != nil {
		uid = *u.UID
	}
	if u.GID != nil {
		gid = *u.GID
	}
	return json.Marshal(map[string]any{"id": map[string]uint32{"uid": uid, "gid": gid}})
}

func (u *ProcessUser) UnmarshalJSON(data []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if rawData, ok := obj["raw"]; ok {
		var raw struct {
			UserString string `json:"userString"`
		}
		if err := json.Unmarshal(rawData, &raw); err != nil {
			return err
		}
		*u = RawProcessUser(raw.UserString)
		return nil
	}
	if idData, ok := obj["id"]; ok {
		var id struct {
			UID uint32 `json:"uid"`
			GID uint32 `json:"gid"`
		}
		if err := json.Unmarshal(idData, &id); err != nil {
			return err
		}
		*u = IDProcessUser(id.UID, id.GID)
		return nil
	}
	return fmt.Errorf("unknown process user case")
}

type Filesystem struct {
	Type        FilesystemType `json:"type"`
	Source      string         `json:"source"`
	Destination string         `json:"destination"`
	Options     []string       `json:"options"`
}

type FilesystemType struct {
	Kind   string
	Format string
	Name   string
	Cache  CacheMode
	Sync   SyncMode
}

const (
	FilesystemTypeBlock    = "block"
	FilesystemTypeVolume   = "volume"
	FilesystemTypeVirtiofs = "virtiofs"
	FilesystemTypeTmpfs    = "tmpfs"
)

type CacheMode string

const (
	CacheModeOn   CacheMode = "on"
	CacheModeOff  CacheMode = "off"
	CacheModeAuto CacheMode = "auto"
)

type SyncMode string

const (
	SyncModeFull   SyncMode = "full"
	SyncModeFsync  SyncMode = "fsync"
	SyncModeNoSync SyncMode = "nosync"
)

func (m CacheMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(m))
}

func (m *CacheMode) UnmarshalJSON(data []byte) error {
	value, err := decodeStringOrEmptyEnum(data)
	if err != nil {
		return err
	}
	*m = CacheMode(value)
	return nil
}

func (m SyncMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(m))
}

func (m *SyncMode) UnmarshalJSON(data []byte) error {
	value, err := decodeStringOrEmptyEnum(data)
	if err != nil {
		return err
	}
	*m = SyncMode(value)
	return nil
}

func (t FilesystemType) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case FilesystemTypeBlock:
		return json.Marshal(map[string]any{"block": map[string]any{
			"format": t.Format,
			"cache":  defaultString(string(t.Cache), string(CacheModeOn)),
			"sync":   defaultString(string(t.Sync), string(SyncModeFsync)),
		}})
	case FilesystemTypeVolume:
		return json.Marshal(map[string]any{"volume": map[string]any{
			"name":   t.Name,
			"format": t.Format,
			"cache":  defaultString(string(t.Cache), string(CacheModeOn)),
			"sync":   defaultString(string(t.Sync), string(SyncModeFsync)),
		}})
	case FilesystemTypeVirtiofs:
		return json.Marshal(map[string]any{"virtiofs": map[string]any{}})
	case FilesystemTypeTmpfs, "":
		return json.Marshal(map[string]any{"tmpfs": map[string]any{}})
	default:
		return nil, fmt.Errorf("unknown filesystem type %q", t.Kind)
	}
}

func (t *FilesystemType) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		t.Kind = asString
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	for k, v := range obj {
		t.Kind = k
		switch k {
		case FilesystemTypeBlock:
			var fields struct {
				Format string    `json:"format"`
				Cache  CacheMode `json:"cache"`
				Sync   SyncMode  `json:"sync"`
			}
			if err := json.Unmarshal(v, &fields); err != nil {
				return err
			}
			t.Format, t.Cache, t.Sync = fields.Format, fields.Cache, fields.Sync
		case FilesystemTypeVolume:
			var fields struct {
				Name   string    `json:"name"`
				Format string    `json:"format"`
				Cache  CacheMode `json:"cache"`
				Sync   SyncMode  `json:"sync"`
			}
			if err := json.Unmarshal(v, &fields); err != nil {
				return err
			}
			t.Name, t.Format, t.Cache, t.Sync = fields.Name, fields.Format, fields.Cache, fields.Sync
		case FilesystemTypeVirtiofs, FilesystemTypeTmpfs:
		default:
			return fmt.Errorf("unknown filesystem type %q", k)
		}
		return nil
	}
	return fmt.Errorf("empty filesystem type")
}

type PublishPort struct {
	HostAddress   IPAddress       `json:"hostAddress"`
	HostPort      uint16          `json:"hostPort"`
	ContainerPort uint16          `json:"containerPort"`
	Proto         PublishProtocol `json:"proto"`
	Count         uint16          `json:"count"`
}

func (p *PublishPort) UnmarshalJSON(data []byte) error {
	type wire PublishPort
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Count == 0 {
		v.Count = 1
	}
	if err := validatePortRange(v.HostPort, v.Count); err != nil {
		return fmt.Errorf("hostPort: %w", err)
	}
	if err := validatePortRange(v.ContainerPort, v.Count); err != nil {
		return fmt.Errorf("containerPort: %w", err)
	}
	*p = PublishPort(v)
	return nil
}

func (p PublishPort) MarshalJSON() ([]byte, error) {
	type wire PublishPort
	if p.Count == 0 {
		p.Count = 1
	}
	if err := validatePortRange(p.HostPort, p.Count); err != nil {
		return nil, fmt.Errorf("hostPort: %w", err)
	}
	if err := validatePortRange(p.ContainerPort, p.Count); err != nil {
		return nil, fmt.Errorf("containerPort: %w", err)
	}
	return json.Marshal(wire(p))
}

func validatePortRange(port uint16, count uint16) error {
	if count == 0 || uint32(port)+uint32(count)-1 > uint32(^uint16(0)) {
		return fmt.Errorf("invalid port and count: %d, %d", port, count)
	}
	return nil
}

type PublishSocket struct {
	ContainerPath FilePath         `json:"containerPath"`
	HostPath      FilePath         `json:"hostPath"`
	Permissions   *FilePermissions `json:"permissions,omitempty"`
}

func (s *PublishSocket) UnmarshalJSON(data []byte) error {
	var wire struct {
		ContainerPath string           `json:"containerPath"`
		HostPath      string           `json:"hostPath"`
		Permissions   *FilePermissions `json:"permissions,omitempty"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	containerPath, err := decodeSocketPath(wire.ContainerPath)
	if err != nil {
		return fmt.Errorf("containerPath: %w", err)
	}
	hostPath, err := decodeSocketPath(wire.HostPath)
	if err != nil {
		return fmt.Errorf("hostPath: %w", err)
	}
	*s = PublishSocket{ContainerPath: FilePath(containerPath), HostPath: FilePath(hostPath), Permissions: wire.Permissions}
	return nil
}

func (s PublishSocket) MarshalJSON() ([]byte, error) {
	if err := validateAbsolutePath(string(s.ContainerPath)); err != nil {
		return nil, fmt.Errorf("containerPath: %w", err)
	}
	if err := validateAbsolutePath(string(s.HostPath)); err != nil {
		return nil, fmt.Errorf("hostPath: %w", err)
	}
	var wire struct {
		ContainerPath string           `json:"containerPath"`
		HostPath      string           `json:"hostPath"`
		Permissions   *FilePermissions `json:"permissions,omitempty"`
	}
	wire.ContainerPath = string(s.ContainerPath)
	wire.HostPath = string(s.HostPath)
	wire.Permissions = s.Permissions
	return json.Marshal(wire)
}

func decodeSocketPath(raw string) (string, error) {
	if strings.HasPrefix(raw, "file:") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if u.Scheme != "file" {
			return "", fmt.Errorf("malformed file URL: %s", raw)
		}
		if u.Host != "" && u.Host != "localhost" {
			return "", fmt.Errorf("file URL host must be empty or localhost: %s", raw)
		}
		raw, err = url.PathUnescape(u.Path)
		if err != nil {
			return "", err
		}
	}
	if err := validateAbsolutePath(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func validateAbsolutePath(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	return nil
}

type Attachment struct {
	Network     string      `json:"network"`
	Hostname    string      `json:"hostname"`
	IPv4Address CIDRv4      `json:"ipv4Address"`
	IPv4Gateway IPv4Address `json:"ipv4Gateway"`
	IPv6Address *CIDRv6     `json:"ipv6Address,omitempty"`
	MACAddress  *MACAddress `json:"macAddress,omitempty"`
	MTU         *uint32     `json:"mtu,omitempty"`
	Variant     *string     `json:"variant,omitempty"`
}

func (a *Attachment) UnmarshalJSON(data []byte) error {
	type wire Attachment
	var raw struct {
		wire
		Address *CIDRv4      `json:"address"`
		Gateway *IPv4Address `json:"gateway"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v := Attachment(raw.wire)
	if v.IPv4Address == "" && raw.Address != nil {
		v.IPv4Address = *raw.Address
	}
	if v.IPv4Gateway == "" && raw.Gateway != nil {
		v.IPv4Gateway = *raw.Gateway
	}
	*a = v
	return nil
}

type AttachmentConfiguration struct {
	Network string            `json:"network"`
	Options AttachmentOptions `json:"options"`
}

type AttachmentOptions struct {
	Hostname   string      `json:"hostname"`
	MACAddress *MACAddress `json:"macAddress,omitempty"`
	MTU        *uint32     `json:"mtu,omitempty"`
}

type NetworkConfiguration struct {
	Name         string            `json:"name"`
	Mode         NetworkMode       `json:"mode"`
	CreationDate AppleTime         `json:"creationDate"`
	IPv4Subnet   *CIDRv4           `json:"ipv4Subnet,omitempty"`
	IPv6Subnet   *CIDRv6           `json:"ipv6Subnet,omitempty"`
	Labels       ResourceLabels    `json:"labels"`
	Plugin       string            `json:"plugin"`
	Options      map[string]string `json:"options"`
}

func (n NetworkConfiguration) ID() string {
	return n.Name
}

func (n NetworkConfiguration) MarshalJSON() ([]byte, error) {
	type wire NetworkConfiguration
	v := wire(n)
	if v.CreationDate.IsZero() {
		v.CreationDate = NewAppleTime(time.Unix(0, 0).UTC())
	}
	if v.Labels == nil {
		v.Labels = ResourceLabels{}
	}
	if v.Plugin == "" {
		v.Plugin = "container-network-vmnet"
	}
	if v.Options == nil {
		v.Options = map[string]string{}
	}
	return json.Marshal(v)
}

func (n *NetworkConfiguration) UnmarshalJSON(data []byte) error {
	type wire NetworkConfiguration
	var raw struct {
		wire
		ID         string  `json:"id"`
		Subnet     *CIDRv4 `json:"subnet"`
		PluginInfo *struct {
			Plugin  string  `json:"plugin"`
			Variant *string `json:"variant,omitempty"`
		} `json:"pluginInfo"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v := NetworkConfiguration(raw.wire)
	if v.Name == "" {
		v.Name = raw.ID
	}
	if v.IPv4Subnet == nil {
		v.IPv4Subnet = raw.Subnet
	}
	if v.Labels == nil {
		v.Labels = ResourceLabels{}
	}
	if v.CreationDate.IsZero() {
		v.CreationDate = NewAppleTime(time.Unix(0, 0).UTC())
	}
	if v.Plugin == "" {
		if raw.PluginInfo != nil {
			v.Plugin = raw.PluginInfo.Plugin
			if raw.PluginInfo.Variant != nil {
				v.Options = map[string]string{"variant": *raw.PluginInfo.Variant}
			}
		} else {
			v.Plugin = "container-network-vmnet"
		}
	}
	if v.Options == nil {
		v.Options = map[string]string{}
	}
	*n = v
	return nil
}

type NetworkStatus struct {
	IPv4Subnet  CIDRv4      `json:"ipv4Subnet"`
	IPv4Gateway IPv4Address `json:"ipv4Gateway"`
	IPv6Subnet  *CIDRv6     `json:"ipv6Subnet,omitempty"`
}

func (n *NetworkStatus) UnmarshalJSON(data []byte) error {
	type wire NetworkStatus
	var raw struct {
		wire
		Address *CIDRv4      `json:"address"`
		Gateway *IPv4Address `json:"gateway"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v := NetworkStatus(raw.wire)
	if v.IPv4Subnet == "" && raw.Address != nil {
		v.IPv4Subnet = *raw.Address
	}
	if v.IPv4Gateway == "" && raw.Gateway != nil {
		v.IPv4Gateway = *raw.Gateway
	}
	*n = v
	return nil
}

type NetworkResource struct {
	Configuration NetworkConfiguration `json:"configuration"`
	Status        NetworkStatus        `json:"status"`
}

func (n NetworkResource) ID() string {
	return n.Configuration.Name
}

func (n NetworkResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID            string               `json:"id"`
		Configuration NetworkConfiguration `json:"configuration"`
		Status        NetworkStatus        `json:"status"`
	}{
		ID:            n.ID(),
		Configuration: n.Configuration,
		Status:        n.Status,
	})
}

func (n *NetworkResource) UnmarshalJSON(data []byte) error {
	var wire struct {
		Configuration NetworkConfiguration `json:"configuration"`
		Status        NetworkStatus        `json:"status"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	n.Configuration = wire.Configuration
	n.Status = wire.Status
	return nil
}

type ImageDescription struct {
	Reference  string     `json:"reference"`
	Descriptor Descriptor `json:"descriptor"`
}

type ImageResource struct {
	Configuration    ImageConfiguration `json:"configuration"`
	Variants         []ImageVariant     `json:"variants"`
	DisplayReference string             `json:"-"`
}

func (i ImageResource) ID() string {
	digest := i.Configuration.Descriptor.Digest
	if _, suffix, ok := strings.Cut(digest, ":"); ok {
		return suffix
	}
	return digest
}

func (i ImageResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID            string             `json:"id"`
		Configuration ImageConfiguration `json:"configuration"`
		Variants      []ImageVariant     `json:"variants"`
	}{
		ID:            i.ID(),
		Configuration: i.Configuration,
		Variants:      i.Variants,
	})
}

func (i *ImageResource) UnmarshalJSON(data []byte) error {
	var wire struct {
		Configuration ImageConfiguration `json:"configuration"`
		Variants      []ImageVariant     `json:"variants"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	i.Configuration = wire.Configuration
	i.Variants = wire.Variants
	i.DisplayReference = wire.Configuration.Name
	return nil
}

type ImageConfiguration struct {
	CreationDate AppleTime  `json:"creationDate"`
	Name         string     `json:"name"`
	Descriptor   Descriptor `json:"descriptor"`
}

type ImageVariant struct {
	Platform Platform `json:"platform"`
	Digest   string   `json:"digest"`
	Size     int64    `json:"size"`
	Config   OCIImage `json:"config"`
}

type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	URLs         []string          `json:"urls,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Platform     *Platform         `json:"platform,omitempty"`
	ArtifactType *string           `json:"artifactType,omitempty"`
}

type Platform struct {
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	OSVersion    *string  `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
}

type SystemPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func CurrentSystemPlatform(goarch string) SystemPlatform {
	if goarch == "amd64" {
		return SystemPlatform{OS: "linux", Architecture: "amd64"}
	}
	return SystemPlatform{OS: "linux", Architecture: "arm64"}
}

type Kernel struct {
	Path        string            `json:"path"`
	Platform    SystemPlatform    `json:"platform"`
	CommandLine KernelCommandLine `json:"commandLine"`
}

type KernelCommandLine struct {
	KernelArgs []string `json:"kernelArgs"`
	InitArgs   []string `json:"initArgs"`
}

func NewKernel(path string, platform SystemPlatform) Kernel {
	if !strings.HasPrefix(path, "file:") {
		path = (&url.URL{Scheme: "file", Path: path}).String()
	}
	return Kernel{
		Path:     path,
		Platform: platform,
		CommandLine: KernelCommandLine{
			KernelArgs: []string{"console=hvc0", "tsc=reliable", "panic=0"},
			InitArgs:   []string{},
		},
	}
}

type OCIImage struct {
	Created      *string      `json:"created,omitempty"`
	Author       *string      `json:"author,omitempty"`
	Architecture string       `json:"architecture"`
	OS           string       `json:"os"`
	OSVersion    *string      `json:"os.version,omitempty"`
	OSFeatures   []string     `json:"os.features,omitempty"`
	Variant      *string      `json:"variant,omitempty"`
	Config       *ImageConfig `json:"config,omitempty"`
	Rootfs       Rootfs       `json:"rootfs"`
	History      []History    `json:"history,omitempty"`
}

type ImageConfig struct {
	User       *string           `json:"User,omitempty"`
	Env        []string          `json:"Env,omitempty"`
	Entrypoint []string          `json:"Entrypoint,omitempty"`
	Cmd        []string          `json:"Cmd,omitempty"`
	WorkingDir *string           `json:"WorkingDir,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	StopSignal *string           `json:"StopSignal,omitempty"`
}

type Rootfs struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type History struct {
	Created    *string `json:"created,omitempty"`
	CreatedBy  *string `json:"created_by,omitempty"`
	Author     *string `json:"author,omitempty"`
	Comment    *string `json:"comment,omitempty"`
	EmptyLayer *bool   `json:"empty_layer,omitempty"`
}

type ResourceLabels map[string]string

type RegistryResource struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Username         string         `json:"username"`
	CreationDate     AppleTime      `json:"creationDate"`
	ModificationDate AppleTime      `json:"modificationDate"`
	Labels           ResourceLabels `json:"labels"`
}

type VolumeConfiguration struct {
	Name         string            `json:"name"`
	Driver       string            `json:"driver"`
	Format       string            `json:"format"`
	Source       string            `json:"source"`
	CreationDate AppleTime         `json:"creationDate"`
	Labels       map[string]string `json:"labels"`
	Options      map[string]string `json:"options"`
	SizeInBytes  *uint64           `json:"sizeInBytes,omitempty"`
}

func (v *VolumeConfiguration) UnmarshalJSON(data []byte) error {
	type wire VolumeConfiguration
	var raw struct {
		wire
		CreatedAt *AppleTime `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := VolumeConfiguration(raw.wire)
	if out.CreationDate.IsZero() && raw.CreatedAt != nil {
		out.CreationDate = *raw.CreatedAt
	}
	if out.CreationDate.IsZero() {
		out.CreationDate = NewAppleTime(time.Unix(0, 0).UTC())
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	if out.Options == nil {
		out.Options = map[string]string{}
	}
	*v = out
	return nil
}

type VolumeResource struct {
	Configuration VolumeConfiguration `json:"configuration"`
}

func (v VolumeResource) ID() string {
	return v.Configuration.Name
}

func (v VolumeResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID            string              `json:"id"`
		Configuration VolumeConfiguration `json:"configuration"`
	}{
		ID:            v.ID(),
		Configuration: v.Configuration,
	})
}

func (v *VolumeResource) UnmarshalJSON(data []byte) error {
	var wire struct {
		Configuration VolumeConfiguration `json:"configuration"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	v.Configuration = wire.Configuration
	return nil
}

type DiskUsageStats struct {
	Images     ResourceUsage `json:"images"`
	Containers ResourceUsage `json:"containers"`
	Volumes    ResourceUsage `json:"volumes"`
}

type ResourceUsage struct {
	Total       int    `json:"total"`
	Active      int    `json:"active"`
	SizeInBytes uint64 `json:"sizeInBytes"`
	Reclaimable uint64 `json:"reclaimable"`
}

type SystemHealth struct {
	AppRoot          string  `json:"appRoot"`
	InstallRoot      string  `json:"installRoot"`
	LogRoot          *string `json:"logRoot,omitempty"`
	APIServerVersion string  `json:"apiServerVersion"`
	APIServerCommit  string  `json:"apiServerCommit"`
	APIServerBuild   string  `json:"apiServerBuild"`
	APIServerAppName string  `json:"apiServerAppName"`
}

type XPCKey string

const (
	XPCKeyRoute             XPCKey = "route"
	XPCKeyContainers        XPCKey = "containers"
	XPCKeyID                XPCKey = "id"
	XPCKeyProcessIdentifier XPCKey = "processIdentifier"
	XPCKeyContainerConfig   XPCKey = "containerConfig"
	XPCKeyContainerOptions  XPCKey = "containerOptions"
	XPCKeyRuntimeData       XPCKey = "runtimeData"
	XPCKeyPort              XPCKey = "port"
	XPCKeyExitCode          XPCKey = "exitCode"
	XPCKeyExitedAt          XPCKey = "exitedAt"
	XPCKeyStdin             XPCKey = "stdin"
	XPCKeyStdout            XPCKey = "stdout"
	XPCKeyStderr            XPCKey = "stderr"
	XPCKeyFD                XPCKey = "fd"
	XPCKeyLogs              XPCKey = "logs"
	XPCKeyStopOptions       XPCKey = "stopOptions"
	XPCKeyForceDelete       XPCKey = "forceDelete"
	XPCKeyDynamicEnv        XPCKey = "dynamicEnv"
	XPCKeyAppRoot           XPCKey = "appRoot"
	XPCKeyInstallRoot       XPCKey = "installRoot"
	XPCKeyLogRoot           XPCKey = "logRoot"
	XPCKeyAPIServerVersion  XPCKey = "apiServerVersion"
	XPCKeyAPIServerCommit   XPCKey = "apiServerCommit"
	XPCKeyAPIServerBuild    XPCKey = "apiServerBuild"
	XPCKeyAPIServerAppName  XPCKey = "apiServerAppName"
	XPCKeySignal            XPCKey = "signal"
	XPCKeyWidth             XPCKey = "width"
	XPCKeyHeight            XPCKey = "height"
	XPCKeyProcessConfig     XPCKey = "processConfig"
	XPCKeyKernel            XPCKey = "kernel"
	XPCKeyInitImage         XPCKey = "initImage"
	XPCKeyArchive           XPCKey = "archive"
	XPCKeySystemPlatform    XPCKey = "systemPlatform"
	XPCKeyImageDescriptions XPCKey = "imageDescriptions"
	XPCKeyNetworkID         XPCKey = "networkId"
	XPCKeyNetworkConfig     XPCKey = "networkConfig"
	XPCKeyNetworkResource   XPCKey = "networkResource"
	XPCKeyNetworkResources  XPCKey = "networkResources"
	XPCKeyVolume            XPCKey = "volume"
	XPCKeyVolumes           XPCKey = "volumes"
	XPCKeyVolumeName        XPCKey = "volumeName"
	XPCKeyVolumeSize        XPCKey = "volumeSize"
	XPCKeyVolumeDriver      XPCKey = "volumeDriver"
	XPCKeyVolumeDriverOpts  XPCKey = "volumeDriverOpts"
	XPCKeyVolumeLabels      XPCKey = "volumeLabels"
	XPCKeyStatistics        XPCKey = "statistics"
	XPCKeyContainerSize     XPCKey = "containerSize"
	XPCKeyListFilters       XPCKey = "listFilters"
	XPCKeyDiskUsageStats    XPCKey = "diskUsageStats"
	XPCKeySourcePath        XPCKey = "sourcePath"
	XPCKeyDestinationPath   XPCKey = "destinationPath"
	XPCKeyFileMode          XPCKey = "fileMode"
	XPCKeyCreateParents     XPCKey = "createParents"
)

type XPCRoute string

const (
	XPCRouteContainerList          XPCRoute = "containerList"
	XPCRouteContainerCreate        XPCRoute = "containerCreate"
	XPCRouteContainerBootstrap     XPCRoute = "containerBootstrap"
	XPCRouteContainerCreateProcess XPCRoute = "containerCreateProcess"
	XPCRouteContainerStartProcess  XPCRoute = "containerStartProcess"
	XPCRouteContainerWait          XPCRoute = "containerWait"
	XPCRouteContainerDelete        XPCRoute = "containerDelete"
	XPCRouteContainerStop          XPCRoute = "containerStop"
	XPCRouteContainerDial          XPCRoute = "containerDial"
	XPCRouteContainerResize        XPCRoute = "containerResize"
	XPCRouteContainerKill          XPCRoute = "containerKill"
	XPCRouteContainerLogs          XPCRoute = "containerLogs"
	XPCRouteContainerStats         XPCRoute = "containerStats"
	XPCRouteContainerDiskUsage     XPCRoute = "containerDiskUsage"
	XPCRouteContainerCopyIn        XPCRoute = "containerCopyIn"
	XPCRouteContainerCopyOut       XPCRoute = "containerCopyOut"
	XPCRouteContainerExport        XPCRoute = "containerExport"
	XPCRouteImageList              XPCRoute = "imageList"
	XPCRouteNetworkCreate          XPCRoute = "networkCreate"
	XPCRouteNetworkDelete          XPCRoute = "networkDelete"
	XPCRouteNetworkList            XPCRoute = "networkList"
	XPCRouteVolumeCreate           XPCRoute = "volumeCreate"
	XPCRouteVolumeDelete           XPCRoute = "volumeDelete"
	XPCRouteVolumeList             XPCRoute = "volumeList"
	XPCRouteVolumeInspect          XPCRoute = "volumeInspect"
	XPCRouteVolumeDiskUsage        XPCRoute = "volumeDiskUsage"
	XPCRouteSystemDiskUsage        XPCRoute = "systemDiskUsage"
	XPCRoutePing                   XPCRoute = "ping"
	XPCRouteGetDefaultKernel       XPCRoute = "getDefaultKernel"
)

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func decodeStringOrEmptyEnum(data []byte) (string, error) {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		return asString, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", err
	}
	for key := range obj {
		return key, nil
	}
	return "", fmt.Errorf("empty enum value")
}
