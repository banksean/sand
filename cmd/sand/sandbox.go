// command devsandbox manages containerized dev sandobx environments on MacOS
//
// On startup, devsandbox will:
// - if --attach=${id} is not set:
//   - choose a new unused ${id}
//   - create a new copy-on-write clone of the current working directory in ~/sandboxen/${id} on the MacOS host
//   - create a new container instance with name ${id} and ~/sandboxen/${id} mounted to /app in the container, using bind-mode
// - start container named ${id}
// - exec a shell in the container and connect this process's stdio to that shell in the container
//
// On exit, devsandbox will
// - stop the container named ${id}
// - if --rm is set, delete the container

package main

import (
	"embed"

	_ "embed"
)

var (
	//go:embed defaultcontainer/*
	defaultContainer embed.FS
)
