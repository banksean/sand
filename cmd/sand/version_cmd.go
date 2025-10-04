package main

import (
	"fmt"
	"runtime/debug"
)

var (
	// These will be set via -ldflags
	GitRepo   string
	GitBranch string
	GitCommit string
	BuildTime string
)

type VersionCmd struct{}

func (v *VersionCmd) Run(cctx *Context) error {
	fmt.Printf("Git Repository: %s\n", GitRepo)
	fmt.Printf("Git Branch: %s\n", GitBranch)
	fmt.Printf("Git Commit: %s\n", GitCommit)
	fmt.Printf("Build Time: %s\n", BuildTime)
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Build info not available")
		return nil
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			fmt.Printf("Git Commit: %s\n", setting.Value)
		}
		if setting.Key == "vcs.time" {
			fmt.Printf("Commit Time: %s\n", setting.Value)
		}
		if setting.Key == "vcs.modified" {
			fmt.Printf("Modified: %s\n", setting.Value)
		}
	}
	return nil
}
