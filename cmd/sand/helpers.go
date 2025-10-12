package main

import (
	"strings"

	"github.com/banksean/sand/applecontainer/types"
)

func getContainerHostname(ctr *types.Container) string {
	for _, n := range ctr.Networks {
		return strings.TrimSuffix(n.Hostname, ".")
	}
	return ctr.Configuration.ID
}
