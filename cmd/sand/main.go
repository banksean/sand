package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/zalando/go-keyring"
)

type Context struct {
	LogFile   string
	LogLevel  string
	CloneRoot string
	Keychain  map[string]string
}

type CLI struct {
	LogFile   string `default:"/tmp/sand/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel  string `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	CloneRoot string `default:"/tmp/sand/boxen" placeholder:"<clone-root-dir>" help:"root dir to store sandbox clones of working directories"`

	Shell ShellCmd `cmd:"" help:"create or revive a sandbox and shell into its container"`
	Exec  ExecCmd  `cmd:"" help:"execute a single command in a sanbox"`
	Ls    LsCmd    `cmd:"" help:"list sandboxes"`
	Rm    RmCmd    `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop  StopCmd  `cmd:"" help:"stop sandbox container"`
	Doc   DocCmd   `cmd:"" help:"print complete command help formatted as markdown"`
}

func (c *CLI) initSlog() {
	var level slog.Level
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to info if invalid
	}

	// Create a new logger with a JSON handler writing to standard error
	var f *os.File
	var err error
	if c.LogFile == "" {
		f, err = os.CreateTemp("/tmp", c.LogFile)
		if err != nil {
			panic(err)
		}
	} else {
		logDir := filepath.Dir(c.LogFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			panic(err)
		}
		f, err = os.OpenFile(c.LogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			panic(err)
		}
	}

	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	slog.Info("slog initialized")
}

type CredsCmd struct{}

func (c *CredsCmd) Run(cctx *Context) error {
	fmt.Printf("%+v\n", cctx.Keychain)
	return nil
}

const description = `Manage lightweight linux container sandboxes on MacOS.`

func main() {
	var cli CLI

	ctx := kong.Parse(&cli,
		kong.Description(description))
	cli.initSlog()

	keychain := map[string]string{}
	key, err := keyring.Get("Claude Code-credentials", os.Getenv("USER"))
	if err == nil {
		keychain["CLAUDE_CREDENTIALS"] = key
	}

	err = ctx.Run(&Context{
		LogFile:   cli.LogFile,
		LogLevel:  cli.LogLevel,
		CloneRoot: cli.CloneRoot,
		Keychain:  keychain,
	})
	ctx.FatalIfErrorf(err)
}
