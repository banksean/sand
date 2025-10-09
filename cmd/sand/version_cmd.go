package main

import (
	"fmt"
	"runtime/debug"

	"github.com/banksean/apple-container/version"
)

type VersionCmd struct{}

func (c *VersionCmd) Run(cctx *Context) error {
	versionInfo := version.Get()
	fmt.Printf("Git Repository: %s\n", versionInfo.GitRepo)
	fmt.Printf("Git Branch: %s\n", versionInfo.GitBranch)
	fmt.Printf("Git Commit: %s\n", versionInfo.GitCommit)
	fmt.Printf("Build Time: %s\n", versionInfo.BuildTime)
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Build info not available")
		return nil
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" && versionInfo.GitCommit == "" {
			fmt.Printf("Git Commit: %s\n", setting.Value)
		}
		if setting.Key == "vcs.time" && versionInfo.BuildTime == "" {
			fmt.Printf("Commit Time: %s\n", setting.Value)
		}
		if setting.Key == "vcs.modified" {
			fmt.Printf("Modified: %s\n", setting.Value)
		}
	}
	return nil
}
