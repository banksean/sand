package daemon

import (
	"time"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandtypes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sandboxesToProto(boxes []sandtypes.Box) []*daemonpb.Sandbox {
	out := make([]*daemonpb.Sandbox, 0, len(boxes))
	for i := range boxes {
		out = append(out, sandboxToProto(&boxes[i]))
	}
	return out
}

func sandboxesFromProto(boxes []*daemonpb.Sandbox) []sandtypes.Box {
	out := make([]sandtypes.Box, 0, len(boxes))
	for _, box := range boxes {
		if box != nil {
			out = append(out, *sandboxFromProto(box))
		}
	}
	return out
}

func sandboxToProto(box *sandtypes.Box) *daemonpb.Sandbox {
	if box == nil {
		return nil
	}
	return &daemonpb.Sandbox{
		Id:                    box.ID,
		Name:                  box.Name,
		State:                 box.State,
		AgentType:             box.AgentType,
		ProfileName:           box.ProfileName,
		ContainerId:           box.ContainerID,
		HostOriginDir:         box.HostOriginDir,
		SandboxWorkDir:        box.SandboxWorkDir,
		TrashWorkDir:          box.TrashWorkDir,
		DeletedAt:             timeToProto(box.DeletedAt),
		ImageName:             box.ImageName,
		DnsDomain:             box.DNSDomain,
		EnvFile:               box.EnvFile,
		AllowedDomains:        append([]string(nil), box.AllowedDomains...),
		Mounts:                mountSpecsToProto(box.Mounts),
		MountRequests:         mountRequestsToProto(box.MountRequests),
		SharedCacheMounts:     sharedCacheMountsToProto(box.SharedCacheMounts),
		Cpus:                  int32(box.CPUs),
		MemoryMb:              int32(box.MemoryMB),
		SandboxWorkDirError:   box.SandboxWorkDirError,
		SandboxContainerError: box.SandboxContainerError,
		Username:              box.Username,
		Uid:                   box.Uid,
		OriginalGitDetails:    gitDetailsToProto(box.OriginalGitDetails),
		CurrentGitDetails:     gitDetailsToProto(box.CurrentGitDetails),
		Container:             containerToProto(box.Container),
	}
}

func sandboxFromProto(box *daemonpb.Sandbox) *sandtypes.Box {
	if box == nil {
		return nil
	}
	return &sandtypes.Box{
		ID:                    box.GetId(),
		Name:                  box.GetName(),
		State:                 box.GetState(),
		AgentType:             box.GetAgentType(),
		ProfileName:           box.GetProfileName(),
		ContainerID:           box.GetContainerId(),
		HostOriginDir:         box.GetHostOriginDir(),
		SandboxWorkDir:        box.GetSandboxWorkDir(),
		TrashWorkDir:          box.GetTrashWorkDir(),
		DeletedAt:             timeFromProto(box.GetDeletedAt()),
		ImageName:             box.GetImageName(),
		DNSDomain:             box.GetDnsDomain(),
		EnvFile:               box.GetEnvFile(),
		AllowedDomains:        append([]string(nil), box.GetAllowedDomains()...),
		Mounts:                mountSpecsFromProto(box.GetMounts()),
		MountRequests:         mountRequestsFromProto(box.GetMountRequests()),
		SharedCacheMounts:     sharedCacheMountsFromProto(box.GetSharedCacheMounts()),
		CPUs:                  int(box.GetCpus()),
		MemoryMB:              int(box.GetMemoryMb()),
		SandboxWorkDirError:   box.GetSandboxWorkDirError(),
		SandboxContainerError: box.GetSandboxContainerError(),
		Username:              box.GetUsername(),
		Uid:                   box.GetUid(),
		OriginalGitDetails:    gitDetailsFromProto(box.GetOriginalGitDetails()),
		CurrentGitDetails:     gitDetailsFromProto(box.GetCurrentGitDetails()),
		Container:             containerFromProto(box.GetContainer()),
	}
}

func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func timeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

func mountSpecsToProto(mounts []sandtypes.MountSpec) []*daemonpb.MountSpec {
	out := make([]*daemonpb.MountSpec, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &daemonpb.MountSpec{
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	return out
}

func mountSpecsFromProto(mounts []*daemonpb.MountSpec) []sandtypes.MountSpec {
	out := make([]sandtypes.MountSpec, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, sandtypes.MountSpec{
			Source:   mount.GetSource(),
			Target:   mount.GetTarget(),
			ReadOnly: mount.GetReadOnly(),
		})
	}
	return out
}

func mountRequestsToProto(requests []sandtypes.MountRequest) []*daemonpb.MountRequest {
	out := make([]*daemonpb.MountRequest, 0, len(requests))
	for _, request := range requests {
		out = append(out, &daemonpb.MountRequest{
			Kind:     request.Kind,
			Original: request.Original,
			Source:   request.Source,
			Clone:    request.Clone,
			Target:   request.Target,
			ReadOnly: request.ReadOnly,
			Runtime:  request.Runtime,
		})
	}
	return out
}

func mountRequestsFromProto(requests []*daemonpb.MountRequest) []sandtypes.MountRequest {
	out := make([]sandtypes.MountRequest, 0, len(requests))
	for _, request := range requests {
		if request == nil {
			continue
		}
		out = append(out, sandtypes.MountRequest{
			Kind:     request.GetKind(),
			Original: request.GetOriginal(),
			Source:   request.GetSource(),
			Clone:    request.GetClone(),
			Target:   request.GetTarget(),
			ReadOnly: request.GetReadOnly(),
			Runtime:  request.GetRuntime(),
		})
	}
	return out
}

func sharedCacheMountsToProto(mounts sandtypes.SharedCacheMounts) *daemonpb.SharedCacheMounts {
	if mounts == (sandtypes.SharedCacheMounts{}) {
		return nil
	}
	return &daemonpb.SharedCacheMounts{
		MiseCacheHostDir:    mounts.MiseCacheHostDir,
		ApkCacheHostDir:     mounts.APKCacheHostDir,
		AgentCacheHostDir:   mounts.AgentCacheHostDir,
		BazelRemoteCacheUrl: mounts.BazelRemoteCacheURL,
		HttpProxyUrl:        mounts.HTTPProxyURL,
	}
}

func sharedCacheMountsFromProto(mounts *daemonpb.SharedCacheMounts) sandtypes.SharedCacheMounts {
	if mounts == nil {
		return sandtypes.SharedCacheMounts{}
	}
	return sandtypes.SharedCacheMounts{
		MiseCacheHostDir:    mounts.GetMiseCacheHostDir(),
		APKCacheHostDir:     mounts.GetApkCacheHostDir(),
		AgentCacheHostDir:   mounts.GetAgentCacheHostDir(),
		BazelRemoteCacheURL: mounts.GetBazelRemoteCacheUrl(),
		HTTPProxyURL:        mounts.GetHttpProxyUrl(),
	}
}

func gitDetailsToProto(details *sandtypes.GitDetails) *daemonpb.GitDetails {
	if details == nil {
		return nil
	}
	return &daemonpb.GitDetails{
		RemoteOrigin: details.RemoteOrigin,
		Branch:       details.Branch,
		Commit:       details.Commit,
		IsDirty:      details.IsDirty,
		HasRelative:  details.HasRelative,
		Ahead:        int32(details.Ahead),
		Behind:       int32(details.Behind),
	}
}

func gitDetailsFromProto(details *daemonpb.GitDetails) *sandtypes.GitDetails {
	if details == nil {
		return nil
	}
	return &sandtypes.GitDetails{
		RemoteOrigin: details.GetRemoteOrigin(),
		Branch:       details.GetBranch(),
		Commit:       details.GetCommit(),
		IsDirty:      details.GetIsDirty(),
		HasRelative:  details.GetHasRelative(),
		Ahead:        int(details.GetAhead()),
		Behind:       int(details.GetBehind()),
	}
}

func containerToProto(container *sandtypes.Container) *daemonpb.Container {
	if container == nil {
		return nil
	}
	return &daemonpb.Container{
		Networks:      containerNetworkStatusesToProto(container.Networks),
		Status:        &daemonpb.ContainerStatus{State: container.Status.State},
		Configuration: containerConfigToProto(container.Configuration),
	}
}

func containerFromProto(container *daemonpb.Container) *sandtypes.Container {
	if container == nil {
		return nil
	}
	return &sandtypes.Container{
		Networks:      containerNetworkStatusesFromProto(container.GetNetworks()),
		Status:        sandtypes.ContainerStatus{State: container.GetStatus().GetState()},
		Configuration: containerConfigFromProto(container.GetConfiguration()),
	}
}

func containerNetworkStatusesToProto(networks []sandtypes.ContainerNetworkStatus) []*daemonpb.ContainerNetworkStatus {
	out := make([]*daemonpb.ContainerNetworkStatus, 0, len(networks))
	for _, network := range networks {
		out = append(out, &daemonpb.ContainerNetworkStatus{
			Hostname:    network.Hostname,
			Network:     network.Network,
			Ipv4Address: network.IPv4Address,
			Ipv4Gateway: network.IPv4Gateway,
			Ipv6Address: network.IPv6Address,
			Ipv6Gateway: network.IPv6Gateway,
		})
	}
	return out
}

func containerNetworkStatusesFromProto(networks []*daemonpb.ContainerNetworkStatus) []sandtypes.ContainerNetworkStatus {
	out := make([]sandtypes.ContainerNetworkStatus, 0, len(networks))
	for _, network := range networks {
		if network == nil {
			continue
		}
		out = append(out, sandtypes.ContainerNetworkStatus{
			Hostname:    network.GetHostname(),
			Network:     network.GetNetwork(),
			IPv4Address: network.GetIpv4Address(),
			IPv4Gateway: network.GetIpv4Gateway(),
			IPv6Address: network.GetIpv6Address(),
			IPv6Gateway: network.GetIpv6Gateway(),
		})
	}
	return out
}

func containerConfigToProto(config sandtypes.ContainerConfig) *daemonpb.ContainerConfig {
	return &daemonpb.ContainerConfig{
		Mounts:         mountsToProto(config.Mounts),
		Platform:       platformToProto(config.Platform),
		Virtualization: config.Virtualization,
		InitProcess:    initProcessToProto(config.InitProcess),
		Dns:            dnsToProto(config.DNS),
		Networks:       containerNetworksToProto(config.Networks),
		Id:             config.ID,
		RuntimeHandler: config.RuntimeHandler,
		Ssh:            config.SSH,
		Image:          imageToProto(config.Image),
		Resources:      resourcesToProto(config.Resources),
		Rosetta:        config.Rosetta,
	}
}

func containerConfigFromProto(config *daemonpb.ContainerConfig) sandtypes.ContainerConfig {
	if config == nil {
		return sandtypes.ContainerConfig{}
	}
	return sandtypes.ContainerConfig{
		Mounts:         mountsFromProto(config.GetMounts()),
		Platform:       platformFromProto(config.GetPlatform()),
		Virtualization: config.GetVirtualization(),
		InitProcess:    initProcessFromProto(config.GetInitProcess()),
		DNS:            dnsFromProto(config.GetDns()),
		Networks:       containerNetworksFromProto(config.GetNetworks()),
		ID:             config.GetId(),
		RuntimeHandler: config.GetRuntimeHandler(),
		SSH:            config.GetSsh(),
		Image:          imageFromProto(config.GetImage()),
		Resources:      resourcesFromProto(config.GetResources()),
		Rosetta:        config.GetRosetta(),
	}
}

func mountsToProto(mounts []sandtypes.Mount) []*daemonpb.Mount {
	out := make([]*daemonpb.Mount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &daemonpb.Mount{
			Type:        mountTypeToProto(mount.Type),
			Source:      mount.Source,
			Destination: mount.Destination,
			Options:     append([]string(nil), mount.Options...),
		})
	}
	return out
}

func mountsFromProto(mounts []*daemonpb.Mount) []sandtypes.Mount {
	out := make([]sandtypes.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, sandtypes.Mount{
			Type:        mountTypeFromProto(mount.GetType()),
			Source:      mount.GetSource(),
			Destination: mount.GetDestination(),
			Options:     append([]string(nil), mount.GetOptions()...),
		})
	}
	return out
}

func mountTypeToProto(mountType sandtypes.MountType) *daemonpb.MountType {
	return &daemonpb.MountType{
		Tmpfs:    mountType.Tmpfs != nil,
		Virtiofs: mountType.Virtiofs != nil,
	}
}

func mountTypeFromProto(mountType *daemonpb.MountType) sandtypes.MountType {
	if mountType == nil {
		return sandtypes.MountType{}
	}
	var out sandtypes.MountType
	if mountType.GetTmpfs() {
		out.Tmpfs = &struct{}{}
	}
	if mountType.GetVirtiofs() {
		out.Virtiofs = &struct{}{}
	}
	return out
}

func platformToProto(platform sandtypes.Platform) *daemonpb.Platform {
	return &daemonpb.Platform{Os: platform.OS, Architecture: platform.Architecture, Variant: platform.Variant}
}

func platformFromProto(platform *daemonpb.Platform) sandtypes.Platform {
	if platform == nil {
		return sandtypes.Platform{}
	}
	return sandtypes.Platform{OS: platform.GetOs(), Architecture: platform.GetArchitecture(), Variant: platform.GetVariant()}
}

func initProcessToProto(process sandtypes.InitProcess) *daemonpb.InitProcess {
	return &daemonpb.InitProcess{
		Environment:      append([]string(nil), process.Environment...),
		Executable:       process.Executable,
		WorkingDirectory: process.WorkingDirectory,
		Arguments:        append([]string(nil), process.Arguments...),
		Terminal:         process.Terminal,
		User:             userToProto(process.User),
	}
}

func initProcessFromProto(process *daemonpb.InitProcess) sandtypes.InitProcess {
	if process == nil {
		return sandtypes.InitProcess{}
	}
	return sandtypes.InitProcess{
		Environment:      append([]string(nil), process.GetEnvironment()...),
		Executable:       process.GetExecutable(),
		WorkingDirectory: process.GetWorkingDirectory(),
		Arguments:        append([]string(nil), process.GetArguments()...),
		Terminal:         process.GetTerminal(),
		User:             userFromProto(process.GetUser()),
	}
}

func userToProto(user sandtypes.User) *daemonpb.User {
	return &daemonpb.User{Id: &daemonpb.UserID{Uid: int32(user.ID.UID), Gid: int32(user.ID.GID)}}
}

func userFromProto(user *daemonpb.User) sandtypes.User {
	if user == nil {
		return sandtypes.User{}
	}
	return sandtypes.User{ID: sandtypes.UserID{UID: int(user.GetId().GetUid()), GID: int(user.GetId().GetGid())}}
}

func dnsToProto(dns sandtypes.DNS) *daemonpb.DNS {
	return &daemonpb.DNS{
		Options:       append([]string(nil), dns.Options...),
		Nameservers:   append([]string(nil), dns.Nameservers...),
		SearchDomains: append([]string(nil), dns.SearchDomains...),
	}
}

func dnsFromProto(dns *daemonpb.DNS) sandtypes.DNS {
	if dns == nil {
		return sandtypes.DNS{}
	}
	return sandtypes.DNS{
		Options:       append([]string(nil), dns.GetOptions()...),
		Nameservers:   append([]string(nil), dns.GetNameservers()...),
		SearchDomains: append([]string(nil), dns.GetSearchDomains()...),
	}
}

func containerNetworksToProto(networks []sandtypes.ContainerNetwork) []*daemonpb.ContainerNetwork {
	out := make([]*daemonpb.ContainerNetwork, 0, len(networks))
	for _, network := range networks {
		out = append(out, &daemonpb.ContainerNetwork{
			Network: network.Network,
			Options: &daemonpb.NetworkOptions{
				Hostname: network.Options.Hostname,
				Mtu:      int32(network.Options.MTU),
			},
		})
	}
	return out
}

func containerNetworksFromProto(networks []*daemonpb.ContainerNetwork) []sandtypes.ContainerNetwork {
	out := make([]sandtypes.ContainerNetwork, 0, len(networks))
	for _, network := range networks {
		if network == nil {
			continue
		}
		out = append(out, sandtypes.ContainerNetwork{
			Network: network.GetNetwork(),
			Options: sandtypes.NetworkOptions{
				Hostname: network.GetOptions().GetHostname(),
				MTU:      int(network.GetOptions().GetMtu()),
			},
		})
	}
	return out
}

func imageToProto(image sandtypes.Image) *daemonpb.Image {
	return &daemonpb.Image{Reference: image.Reference, Descriptor_: descriptorToProto(image.Descriptor)}
}

func imageFromProto(image *daemonpb.Image) sandtypes.Image {
	if image == nil {
		return sandtypes.Image{}
	}
	return sandtypes.Image{Reference: image.GetReference(), Descriptor: descriptorFromProto(image.GetDescriptor_())}
}

func descriptorToProto(desc sandtypes.Descriptor) *daemonpb.Descriptor {
	return &daemonpb.Descriptor{Digest: desc.Digest, Size: int64(desc.Size), MediaType: desc.MediaType}
}

func descriptorFromProto(desc *daemonpb.Descriptor) sandtypes.Descriptor {
	if desc == nil {
		return sandtypes.Descriptor{}
	}
	return sandtypes.Descriptor{Digest: desc.GetDigest(), Size: int(desc.GetSize()), MediaType: desc.GetMediaType()}
}

func resourcesToProto(resources sandtypes.Resources) *daemonpb.Resources {
	return &daemonpb.Resources{Cpus: int32(resources.CPUs), MemoryInBytes: resources.MemoryInBytes}
}

func resourcesFromProto(resources *daemonpb.Resources) sandtypes.Resources {
	if resources == nil {
		return sandtypes.Resources{}
	}
	return sandtypes.Resources{CPUs: int(resources.GetCpus()), MemoryInBytes: resources.GetMemoryInBytes()}
}

func containerStatsToProto(stats []sandtypes.ContainerStats) []*daemonpb.ContainerStats {
	out := make([]*daemonpb.ContainerStats, 0, len(stats))
	for _, stat := range stats {
		out = append(out, &daemonpb.ContainerStats{
			BlockReadBytes:   int64(stat.BlockReadBytes),
			BlockWriteBytes:  int64(stat.BlockWriteBytes),
			CpuUsageUsec:     int64(stat.CPUUsageUsec),
			Id:               stat.ID,
			MemoryLimitBytes: int64(stat.MemoryLimitBytes),
			MemoryUsageBytes: int64(stat.MemoryUsageBytes),
			NetworkRxBytes:   int64(stat.NetworkRxBytes),
			NetworkTxBytes:   int64(stat.NetworkTxBytes),
			NumProcesses:     int64(stat.NumProcesses),
		})
	}
	return out
}

func containerStatsFromProto(stats []*daemonpb.ContainerStats) []sandtypes.ContainerStats {
	out := make([]sandtypes.ContainerStats, 0, len(stats))
	for _, stat := range stats {
		if stat == nil {
			continue
		}
		out = append(out, sandtypes.ContainerStats{
			BlockReadBytes:   int(stat.GetBlockReadBytes()),
			BlockWriteBytes:  int(stat.GetBlockWriteBytes()),
			CPUUsageUsec:     int(stat.GetCpuUsageUsec()),
			ID:               stat.GetId(),
			MemoryLimitBytes: int(stat.GetMemoryLimitBytes()),
			MemoryUsageBytes: int(stat.GetMemoryUsageBytes()),
			NetworkRxBytes:   int(stat.GetNetworkRxBytes()),
			NetworkTxBytes:   int(stat.GetNetworkTxBytes()),
			NumProcesses:     int(stat.GetNumProcesses()),
		})
	}
	return out
}
