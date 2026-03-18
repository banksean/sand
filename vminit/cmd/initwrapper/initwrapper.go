//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func main() {
	kmsg, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	if err == nil {
		kmsg.WriteString("<6>sand-init: === SAND INIT IMAGE RUNNING ===\n")
		defer kmsg.Close()
	}

	cmd := exec.Command("/sbin/dnsproxy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	kmsg.WriteString("<6>starting dns proxy sidecar...")

	if err := cmd.Start(); err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>failed to start dns proxy sidecar: %v", err))
	} else {
		kmsg.WriteString(fmt.Sprintf("<6>sand-init: dns proxy pid is %d\n", cmd.Process.Pid))
	}

	kmsg.WriteString("<6>shelling out to /sbin/vminitd.real...")
	syscall.Exec("/sbin/vminitd.real", os.Args, os.Environ())
}
