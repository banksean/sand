package xpc

import (
	"encoding/json"
	"fmt"
)

type ContainerListFilters struct {
	IDs    []string          `json:"ids"`
	Status *RuntimeStatus    `json:"status,omitempty"`
	Labels map[string]string `json:"labels"`
}

func (f ContainerListFilters) MarshalJSON() ([]byte, error) {
	type wire ContainerListFilters
	v := wire(f)
	if v.IDs == nil {
		v.IDs = []string{}
	}
	if v.Labels == nil {
		v.Labels = map[string]string{}
	}
	return json.Marshal(v)
}

type ContainerStopOptions struct {
	TimeoutInSeconds int32   `json:"timeoutInSeconds"`
	Signal           *string `json:"signal,omitempty"`
}

func DefaultContainerStopOptions() ContainerStopOptions {
	return ContainerStopOptions{TimeoutInSeconds: 5}
}

func (o ContainerStopOptions) withDefaults() ContainerStopOptions {
	if o.TimeoutInSeconds == 0 {
		o.TimeoutInSeconds = 5
	}
	return o
}

type ContainerCreateOptions struct {
	AutoRemove     bool        `json:"autoRemove"`
	RootFSOverride *Filesystem `json:"rootFsOverride,omitempty"`
}

func decodeJSONData(data []byte, value any) error {
	if err := json.Unmarshal(data, value); err != nil {
		return err
	}
	return nil
}

func stringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}

func decodeSystemHealth(reply *Message) (SystemHealth, error) {
	var health SystemHealth
	var ok bool
	if health.AppRoot, ok = reply.String(XPCKeyAppRoot); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyAppRoot)
	}
	if health.InstallRoot, ok = reply.String(XPCKeyInstallRoot); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyInstallRoot)
	}
	if logRoot, ok := reply.String(XPCKeyLogRoot); ok {
		health.LogRoot = &logRoot
	}
	if health.APIServerVersion, ok = reply.String(XPCKeyAPIServerVersion); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyAPIServerVersion)
	}
	if health.APIServerCommit, ok = reply.String(XPCKeyAPIServerCommit); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyAPIServerCommit)
	}
	if health.APIServerBuild, ok = reply.String(XPCKeyAPIServerBuild); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyAPIServerBuild)
	}
	if health.APIServerAppName, ok = reply.String(XPCKeyAPIServerAppName); !ok {
		return SystemHealth{}, fmt.Errorf("missing %s", XPCKeyAPIServerAppName)
	}
	return health, nil
}
