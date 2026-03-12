//go:build linux

package main

import (
	"log"
	"os"
	"syscall"
)

func main() {
	// Write a message to kernel log
	kmsg, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	if err == nil {
		kmsg.WriteString("<6>sand-init: === SAND INIT IMAGE RUNNING ===\n")
		kmsg.Close()
	}

	if os.Getenv("SKIP_EXEC") == "" {
		// Execute the real vminitd
		syscall.Exec("/sbin/vminitd.real", os.Args, os.Environ())
	}
	log.Println("skipping exec, staying alive for inspection")
	select {} // block so we can inspect /sys/fs/bpf/ etc
}
