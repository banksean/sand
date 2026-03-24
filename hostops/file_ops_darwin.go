//go build:darwin

package hostops

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func NewDefaultFileOps() FileOps {
	return &defaultFileOps{}
}

func (f *defaultFileOps) Volume(path string) (*VolumeInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat error: %w", err)
	}
	stat := fi.Sys().(*syscall.Stat_t)
	devID := stat.Dev

	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return nil, fmt.Errorf("statfs error: %w", err)
	}
	mountPoint := int8SliceToString(fs.Mntonname[:])

	// Map Device ID to /dev/node (this isn't supper efficient, but
	// we don't call it frequently and don't expect users to have
	// hugge number of /dev/disk* entries.
	deviceName, _ := mapDeviceIDToName(devID)

	return &VolumeInfo{
		Path:       path,
		MountPoint: mountPoint,
		DeviceID:   devID,
		DeviceName: deviceName,
	}, nil
}

func mapDeviceIDToName(id int32) (string, error) {
	// scan /dev/disk* entries to find a matching Rdev
	devFiles, _ := filepath.Glob("/dev/disk*")
	for _, f := range devFiles {
		dstat, err := os.Stat(f)
		if err != nil {
			continue
		}
		raw := dstat.Sys().(*syscall.Stat_t)
		if raw.Rdev == id {
			return f, nil
		}
	}
	return "", fmt.Errorf("node not found")
}

func int8SliceToString(bs []int8) string {
	b := make([]byte, 0, len(bs))
	for _, v := range bs {
		if v == 0 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)
}
