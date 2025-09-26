package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

func main() {
	flag.Parse()
	fmt.Printf("Hello World!\nOperating System: %v\n", runtime.GOOS)
	fmt.Fprintf(os.Stderr, "args: %v\n", flag.Args())
	printHostname()
}
