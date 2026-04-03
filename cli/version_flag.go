package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand/version"
)

type VersionFlag string

func (v VersionFlag) Decode(ctx *kong.DecodeContext) error { return nil }
func (v VersionFlag) IsBool() bool                         { return true }
func (v VersionFlag) BeforeApply(app *kong.Kong, vars kong.Vars) error {
	versionInfo := version.Get()
	if versionInfo.GitBranch != "" {
		fmt.Printf("%s\n", versionInfo.GitBranch)
	} else {
		fmt.Printf("(not a release build; try `sand build-info` for details)\n")
	}
	app.Exit(0)
	return nil
}
