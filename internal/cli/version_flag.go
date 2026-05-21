package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand/internal/version"
)

type VersionFlag string

func (v VersionFlag) Decode(ctx *kong.DecodeContext) error { return nil }
func (v VersionFlag) IsBool() bool                         { return true }
func (v VersionFlag) BeforeApply(app *kong.Kong, vars kong.Vars) error {
	versionInfo := version.Get()
	if !versionInfo.DevBuild {
		fmt.Printf("%s\n", versionInfo.GitBranch)
	} else {
		printBuildInfo()
	}
	app.Exit(0)
	return nil
}
