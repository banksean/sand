package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Printf("Hello World!\nOperating System: %v\n", runtime.GOOS)
	printHostname()
}
