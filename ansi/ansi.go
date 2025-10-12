package ansi

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	reset = "\033[0m"

	Black        = 30
	Red          = 31
	Green        = 32
	Yellow       = 33
	Blue         = 34
	Magenta      = 35
	Cyan         = 36
	LightGray    = 37
	DarkGray     = 90
	LightRed     = 91
	LightGreen   = 92
	LightYellow  = 93
	LightBlue    = 94
	LightMagenta = 95
	LightCyan    = 96
	White        = 97
)

func Colorize(colorCode int, v string) string {
	lines := strings.Split(v, "\n")
	for i, line := range lines {
		lines[i] = fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), line, reset)
	}
	return strings.Join(lines, "\n")
}
