package xpc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAppleTimeRoundTrip(t *testing.T) {
	want := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)
	data, err := json.Marshal(NewAppleTime(want))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "721692800" {
		t.Fatalf("marshal AppleTime = %s", got)
	}
	var decoded AppleTime
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(want) {
		t.Fatalf("decoded AppleTime = %s, want %s", decoded.Time, want)
	}
}

func TestProcessUserCodableShapes(t *testing.T) {
	idUser := IDProcessUser(501, 20)
	data, err := json.Marshal(idUser)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"id":{"gid":20,"uid":501}}`; got != want {
		t.Fatalf("id user JSON = %s, want %s", got, want)
	}

	var raw ProcessUser
	if err := json.Unmarshal([]byte(`{"raw":{"userString":"daemon:wheel"}}`), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.RawUserString == nil || *raw.RawUserString != "daemon:wheel" {
		t.Fatalf("raw user = %#v", raw)
	}
}

func TestFilesystemTypeCodableShapes(t *testing.T) {
	fsType := FilesystemType{
		Kind:   FilesystemTypeBlock,
		Format: "ext4",
		Cache:  CacheModeOn,
		Sync:   SyncModeFsync,
	}
	data, err := json.Marshal(fsType)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"block":{"cache":"on","format":"ext4","sync":"fsync"}}`; got != want {
		t.Fatalf("filesystem type JSON = %s, want %s", got, want)
	}

	var decoded FilesystemType
	if err := json.Unmarshal([]byte(`{"volume":{"name":"vol","format":"ext4","cache":{"on":{}},"sync":{"fsync":{}}}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != FilesystemTypeVolume || decoded.Name != "vol" || decoded.Cache != CacheModeOn || decoded.Sync != SyncModeFsync {
		t.Fatalf("decoded filesystem type = %#v", decoded)
	}

	if err := json.Unmarshal([]byte(`{"tmpfs":{}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != FilesystemTypeTmpfs {
		t.Fatalf("tmpfs kind = %q", decoded.Kind)
	}
}

func TestPublishSocketCompatibility(t *testing.T) {
	var socket PublishSocket
	if err := json.Unmarshal([]byte(`{"containerPath":"file:///tmp/a%20b.sock","hostPath":"file://localhost/tmp/host.sock","permissions":432}`), &socket); err != nil {
		t.Fatal(err)
	}
	if socket.ContainerPath != "/tmp/a b.sock" || socket.HostPath != "/tmp/host.sock" {
		t.Fatalf("decoded socket = %#v", socket)
	}
	if socket.Permissions == nil || *socket.Permissions != 432 {
		t.Fatalf("permissions = %#v", socket.Permissions)
	}
	data, err := json.Marshal(socket)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "file://") {
		t.Fatalf("socket encoded legacy file URL: %s", data)
	}

	for _, input := range []string{
		`{"containerPath":"relative.sock","hostPath":"/host.sock"}`,
		`{"containerPath":"file://example.com/tmp/sock","hostPath":"/host.sock"}`,
		`{"containerPath":"","hostPath":"/host.sock"}`,
	} {
		if err := json.Unmarshal([]byte(input), &socket); err == nil {
			t.Fatalf("expected socket decode error for %s", input)
		}
	}
}

func TestNetworkCompatibility(t *testing.T) {
	var attachment Attachment
	if err := json.Unmarshal([]byte(`{"network":"net","hostname":"ctr","address":"192.168.64.2/24","gateway":"192.168.64.1"}`), &attachment); err != nil {
		t.Fatal(err)
	}
	if attachment.IPv4Address != "192.168.64.2/24" || attachment.IPv4Gateway != "192.168.64.1" {
		t.Fatalf("legacy attachment = %#v", attachment)
	}

	var status NetworkStatus
	if err := json.Unmarshal([]byte(`{"address":"192.168.64.0/24","gateway":"192.168.64.1"}`), &status); err != nil {
		t.Fatal(err)
	}
	if status.IPv4Subnet != "192.168.64.0/24" || status.IPv4Gateway != "192.168.64.1" {
		t.Fatalf("legacy status = %#v", status)
	}

	var config NetworkConfiguration
	if err := json.Unmarshal([]byte(`{"id":"default","mode":"nat","subnet":"192.168.64.0/24","pluginInfo":{"plugin":"container-network-vmnet","variant":"isolated"}}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.Name != "default" || config.IPv4Subnet == nil || *config.IPv4Subnet != "192.168.64.0/24" || config.Options["variant"] != "isolated" {
		t.Fatalf("legacy config = %#v", config)
	}
}

func TestVolumeCompatibility(t *testing.T) {
	var volume VolumeConfiguration
	if err := json.Unmarshal([]byte(`{"name":"v","driver":"local","format":"ext4","source":"/volumes/v.img","createdAt":722995200}`), &volume); err != nil {
		t.Fatal(err)
	}
	if volume.CreationDate.Unix() != 1701302400 {
		t.Fatalf("creation date unix = %d", volume.CreationDate.Unix())
	}
	if volume.Labels == nil || volume.Options == nil {
		t.Fatalf("maps should default to empty maps: %#v", volume)
	}
	data, err := json.Marshal(volume)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "createdAt") || !strings.Contains(string(data), "creationDate") {
		t.Fatalf("volume encoded wrong date key: %s", data)
	}
}

func TestContainerSnapshotDecodeAndMarshal(t *testing.T) {
	input := []byte(`{
		"configuration": {
			"id": "ctr",
			"image": {
				"reference": "docker.io/library/alpine:latest",
				"descriptor": {"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:abc","size":12}
			},
			"initProcess": {
				"executable": "/bin/sh",
				"arguments": ["-c","true"],
				"environment": ["A=B"],
				"workingDirectory": "/",
				"terminal": false,
				"user": {"id":{"uid":0,"gid":0}},
				"supplementalGroups": [],
				"rlimits": []
			},
			"mounts": [{"type":{"virtiofs":{}},"source":"/tmp","destination":"/mnt","options":["ro"]}],
			"resources": {"cpus":2,"memoryInBytes":268435456},
			"platform": {"os":"linux","architecture":"arm64","variant":"v8"}
		},
		"status": "running",
		"networks": [{"network":"default","hostname":"ctr","ipv4Address":"192.168.64.2/24","ipv4Gateway":"192.168.64.1"}],
		"startedDate": 722995200
	}`)
	var snapshot ContainerSnapshot
	if err := json.Unmarshal(input, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ID() != "ctr" || snapshot.Platform().Architecture != "arm64" {
		t.Fatalf("computed fields = id %q platform %#v", snapshot.ID(), snapshot.Platform())
	}
	if snapshot.Configuration.Resources.CPUOverhead != 1 || snapshot.Configuration.RuntimeHandler != "container-runtime-linux" {
		t.Fatalf("defaults not applied: %#v", snapshot.Configuration)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"id":"ctr"`) && strings.Index(string(data), `"id":"ctr"`) < strings.Index(string(data), `"configuration"`) {
		t.Fatalf("snapshot encoded top-level computed id: %s", data)
	}
	if !strings.Contains(string(data), `"configuration"`) || !strings.Contains(string(data), `"status":"running"`) {
		t.Fatalf("snapshot encoded unexpected JSON: %s", data)
	}
}
