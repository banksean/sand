package main

import (
	"embed"
)

//go:embed defaultimage/*
var defaultImageFS embed.FS
