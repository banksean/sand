package xpc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	xpcRouteKey = "com.apple.container.xpc.route"
	xpcErrorKey = "com.apple.container.xpc.error"
)

type messageKind int

const (
	messageKindString messageKind = iota + 1
	messageKindBool
	messageKindInt64
	messageKindUint64
	messageKindData
	messageKindDate
	messageKindFD
)

type messageValue struct {
	kind messageKind
	s    string
	b    bool
	i    int64
	u    uint64
	data []byte
	t    time.Time
	fd   int
}

// Message is a Go representation of the XPC dictionary protocol used by
// ContainerXPC.XPCMessage.
type Message struct {
	values map[string]messageValue
}

func NewMessage(route XPCRoute) *Message {
	m := &Message{values: map[string]messageValue{}}
	m.SetStringRaw(xpcRouteKey, string(route))
	return m
}

func newEmptyMessage() *Message {
	return &Message{values: map[string]messageValue{}}
}

func (m *Message) Route() XPCRoute {
	v, ok := m.values[xpcRouteKey]
	if !ok || v.kind != messageKindString {
		return ""
	}
	return XPCRoute(v.s)
}

func (m *Message) SetString(key XPCKey, value string) {
	m.SetStringRaw(string(key), value)
}

func (m *Message) SetStringRaw(key string, value string) {
	m.set(key, messageValue{kind: messageKindString, s: value})
}

func (m *Message) String(key XPCKey) (string, bool) {
	return m.StringRaw(string(key))
}

func (m *Message) StringRaw(key string) (string, bool) {
	v, ok := m.values[key]
	if !ok || v.kind != messageKindString {
		return "", false
	}
	return v.s, true
}

func (m *Message) SetBool(key XPCKey, value bool) {
	m.set(string(key), messageValue{kind: messageKindBool, b: value})
}

func (m *Message) Bool(key XPCKey) bool {
	v, ok := m.values[string(key)]
	return ok && v.kind == messageKindBool && v.b
}

func (m *Message) SetInt64(key XPCKey, value int64) {
	m.set(string(key), messageValue{kind: messageKindInt64, i: value})
}

func (m *Message) Int64(key XPCKey) int64 {
	v, ok := m.values[string(key)]
	if !ok || v.kind != messageKindInt64 {
		return 0
	}
	return v.i
}

func (m *Message) SetUint64(key XPCKey, value uint64) {
	m.set(string(key), messageValue{kind: messageKindUint64, u: value})
}

func (m *Message) Uint64(key XPCKey) uint64 {
	v, ok := m.values[string(key)]
	if !ok || v.kind != messageKindUint64 {
		return 0
	}
	return v.u
}

func (m *Message) SetDate(key XPCKey, value time.Time) {
	m.set(string(key), messageValue{kind: messageKindDate, t: value})
}

func (m *Message) Date(key XPCKey) time.Time {
	v, ok := m.values[string(key)]
	if !ok || v.kind != messageKindDate {
		return time.Time{}
	}
	return v.t
}

func (m *Message) SetFile(key XPCKey, file *os.File) {
	if file == nil {
		return
	}
	m.set(string(key), messageValue{kind: messageKindFD, fd: int(file.Fd())})
}

func (m *Message) SetFDRaw(key string, fd int) {
	m.set(key, messageValue{kind: messageKindFD, fd: fd})
}

func (m *Message) FD(key XPCKey) (int, bool) {
	v, ok := m.values[string(key)]
	if !ok || v.kind != messageKindFD {
		return 0, false
	}
	return v.fd, true
}

func (m *Message) SetData(key XPCKey, value []byte) {
	copied := append([]byte(nil), value...)
	m.set(string(key), messageValue{kind: messageKindData, data: copied})
}

func (m *Message) Data(key XPCKey) ([]byte, bool) {
	v, ok := m.values[string(key)]
	if !ok || v.kind != messageKindData {
		return nil, false
	}
	return append([]byte(nil), v.data...), true
}

func (m *Message) SetJSON(key XPCKey, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.SetData(key, data)
	return nil
}

func (m *Message) DecodeJSON(key XPCKey, value any) error {
	data, ok := m.Data(key)
	if !ok {
		return fmt.Errorf("missing XPC data key %q", key)
	}
	return json.Unmarshal(data, value)
}

func (m *Message) checkProtocolError() error {
	data, ok := m.DataRaw(xpcErrorKey)
	if !ok {
		return nil
	}
	var err XPCError
	if unmarshalErr := json.Unmarshal(data, &err); unmarshalErr != nil {
		return fmt.Errorf("decode XPC error: %w", unmarshalErr)
	}
	return err
}

func (m *Message) SetDataRaw(key string, value []byte) {
	copied := append([]byte(nil), value...)
	m.set(key, messageValue{kind: messageKindData, data: copied})
}

func (m *Message) DataRaw(key string) ([]byte, bool) {
	v, ok := m.values[key]
	if !ok || v.kind != messageKindData {
		return nil, false
	}
	return append([]byte(nil), v.data...), true
}

func (m *Message) set(key string, value messageValue) {
	if m.values == nil {
		m.values = map[string]messageValue{}
	}
	m.values[key] = value
}

// XPCError is the structured error object set under
// com.apple.container.xpc.error by ContainerXPC.
type XPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e XPCError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func knownXPCDictionaryKeys() []string {
	return []string{
		xpcRouteKey,
		xpcErrorKey,
		string(XPCKeyContainers),
		string(XPCKeyID),
		string(XPCKeyProcessIdentifier),
		string(XPCKeyContainerConfig),
		string(XPCKeyContainerOptions),
		string(XPCKeyRuntimeData),
		string(XPCKeyPort),
		string(XPCKeyExitCode),
		string(XPCKeyExitedAt),
		string(XPCKeyStdin),
		string(XPCKeyStdout),
		string(XPCKeyStderr),
		string(XPCKeyFD),
		string(XPCKeyLogs),
		string(XPCKeyStopOptions),
		string(XPCKeyForceDelete),
		string(XPCKeyDynamicEnv),
		string(XPCKeyAppRoot),
		string(XPCKeyInstallRoot),
		string(XPCKeyLogRoot),
		string(XPCKeyAPIServerVersion),
		string(XPCKeyAPIServerCommit),
		string(XPCKeyAPIServerBuild),
		string(XPCKeyAPIServerAppName),
		string(XPCKeySignal),
		string(XPCKeyWidth),
		string(XPCKeyHeight),
		string(XPCKeyProcessConfig),
		string(XPCKeyKernel),
		string(XPCKeyInitImage),
		string(XPCKeyArchive),
		string(XPCKeySystemPlatform),
		string(XPCKeyImageDescriptions),
		string(XPCKeyNetworkID),
		string(XPCKeyNetworkConfig),
		string(XPCKeyNetworkResource),
		string(XPCKeyNetworkResources),
		string(XPCKeyVolume),
		string(XPCKeyVolumes),
		string(XPCKeyVolumeName),
		string(XPCKeyVolumeSize),
		string(XPCKeyVolumeDriver),
		string(XPCKeyVolumeDriverOpts),
		string(XPCKeyVolumeLabels),
		string(XPCKeyStatistics),
		string(XPCKeyContainerSize),
		string(XPCKeyListFilters),
		string(XPCKeyDiskUsageStats),
		string(XPCKeySourcePath),
		string(XPCKeyDestinationPath),
		string(XPCKeyFileMode),
		string(XPCKeyCreateParents),
	}
}
