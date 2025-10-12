// package types defines structs for unmashaling the output from various `container` commands.
package types

import "time"

type Container struct {
	Networks []struct {
		Hostname string `json:"hostname"`
		Network  string `json:"network"`
		Address  string `json:"address"`
		Gateway  string `json:"gateway"`
	} `json:"networks"`
	Status        string          `json:"status"`
	Configuration ContainerConfig `json:"configuration"`
}

type ContainerConfig struct {
	PublishedSockets []interface{}          `json:"publishedSockets"`
	Sysctls          map[string]interface{} `json:"sysctls"`
	Mounts           []Mount                `json:"mounts"`
	Labels           map[string]interface{} `json:"labels"`
	Platform         Platform               `json:"platform"`
	Virtualization   bool                   `json:"virtualization"`
	PublishedPorts   []interface{}          `json:"publishedPorts"`
	InitProcess      InitProcess            `json:"initProcess"`
	DNS              DNS                    `json:"dns"`
	Networks         []ContainerNetwork     `json:"networks"`
	ID               string                 `json:"id"`
	RuntimeHandler   string                 `json:"runtimeHandler"`
	SSH              bool                   `json:"ssh"`
	Image            Image                  `json:"image"`
	Resources        Resources              `json:"resources"`
	Rosetta          bool                   `json:"rosetta"`
}

type Mount struct {
	Type        MountType `json:"type"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Options     []string  `json:"options"`
}

type MountType struct {
	Tmpfs    *struct{} `json:"tmpfs,omitempty"`
	Virtiofs *struct{} `json:"virtiofs,omitempty"`
}

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

type InitProcess struct {
	Rlimits            []interface{} `json:"rlimits"`
	Environment        []string      `json:"environment"`
	Executable         string        `json:"executable"`
	WorkingDirectory   string        `json:"workingDirectory"`
	Arguments          []string      `json:"arguments"`
	Terminal           bool          `json:"terminal"`
	SupplementalGroups []interface{} `json:"supplementalGroups"`
	User               User          `json:"user"`
}

type User struct {
	ID UserID `json:"id"`
}

type UserID struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

type DNS struct {
	Options       []string `json:"options"`
	Nameservers   []string `json:"nameservers"`
	SearchDomains []string `json:"searchDomains"`
}

type ContainerNetwork struct {
	Options map[string]string `json:"options"`
	Network string            `json:"network"`
}

type Image struct {
	Reference  string     `json:"reference"`
	Descriptor Descriptor `json:"descriptor"`
}

type Descriptor struct {
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
	MediaType string `json:"mediaType"`
}

type Resources struct {
	CPUs          int   `json:"cpus"`
	MemoryInBytes int64 `json:"memoryInBytes"`
}

type ImageEntry struct {
	Reference  string          `json:"reference"`
	Descriptor ImageDescriptor `json:"descriptor"`
}

type ImageDescriptor struct {
	Size        int               `json:"size"`
	Digest      string            `json:"digest"`
	MediaType   string            `json:"mediaType"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ImageManifest struct {
	Variants []ImageVariant `json:"variants"`
	Name     string         `json:"name"`
	Index    Index          `json:"index"`
}

type ImageVariant struct {
	Size     int                `json:"size"`
	Config   ImageVariantConfig `json:"config"`
	Platform Platform           `json:"platform"`
}

type ImageVariantConfig struct {
	Config       ImageVariantContainerConfig `json:"config"`
	Rootfs       Rootfs                      `json:"rootfs"`
	History      []HistoryEntry              `json:"history"`
	Architecture string                      `json:"architecture"`
	Created      time.Time                   `json:"created"`
	OS           string                      `json:"os"`
}

type ImageVariantContainerConfig struct {
	Cmd        []string          `json:"Cmd,omitempty"`
	WorkingDir string            `json:"WorkingDir,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	Env        []string          `json:"Env,omitempty"`
}

type Rootfs struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type HistoryEntry struct {
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"created_by"`
	Comment    string    `json:"comment,omitempty"`
	EmptyLayer bool      `json:"empty_layer,omitempty"`
}

type Index struct {
	Size        int               `json:"size"`
	Digest      string            `json:"digest"`
	MediaType   string            `json:"mediaType"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type Network struct {
	ID     string        `json:"id"`
	State  string        `json:"state"`
	Status NetworkStatus `json:"status"`
	Config NetworkConfig `json:"config"`
}

type NetworkConfig struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
}

type NetworkStatus struct {
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

type SystemProperty struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Value       any    `json:"value"`
}

func (s *SystemProperty) StringValue() (string, bool) {
	ret, ok := s.Value.(string)
	return ret, ok
}

func (s *SystemProperty) BoolValue() (bool, bool) {
	ret, ok := s.Value.(bool)
	return ret, ok
}
