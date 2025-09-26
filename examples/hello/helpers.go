package main

import (
	"fmt"
	"os"
)

func printHostname() {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("error getting hostname: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Hostname: %v\n", hostname)
}
