package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand/cli"
	"github.com/banksean/sand/mux"
	kongcompletion "github.com/jotaen/kong-completion"
)

// Cross-compile this for use inside the linux container like so:
// CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ./bin/innie ./cmd/innie
//
// The host OS must already have dns entry in the apple container system, created like so:
// `sudo container system dns create host.container.internal --localhost 203.0.113.113`
//
// TODO: Evaluate SSH local port forwarding (make `sand exec` use `ssh -L 4242:4242 ...`) to
// replace the above "container system dns create ..." approach. SSH would require less apple container
// service futzing, but is probably more prone to losing connections as ssh port forward is known
// to do. It also wouldn't have to use a host name from innie - just localhost:4242.

// type Innie is the container-side cli to work with sand.  It shares a lot of the same subcommands
// with the host-side sand command, but they work slightly differently when invoked in a container.
// Notably, the innie connects to the sandd process running on the host via HTTP+JSON over TCP,
// rather than via unix domain socket.

// TODO:
// - Automate the ./bin/innie cross-compilation step.  Might have to bake this into the default image or some such.
// - figure out where the innie should send its logs to, if anywhere.
// - sort out how `sand new` should work when you run it from inside a sandbox container. There are multiple ways to do this. Some options:
//.  - A: should it create a clone from the innie's sandbox's original parent, or
//   - B: should it create a new clone using the innie's sandbox's current state as its parent
//.  - if we want it to do the latter, do we know if apple's container system can clone the entire sandbox container (including FS changes, running processes etc)?

type Innie struct {
	LogFile    string                    `default:"/tmp/sand/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel   string                    `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string                    `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`
	HTTPPort   string                    `default:"4242" placeholder:"<local port>" help:"container host http port to connect to, for commands running inside containers"`
	Completion kongcompletion.Completion `cmd:"" help:"Outputs shell code for initialising tab completions"`

	New     cli.NewCmd     `cmd:"" help:"create a new sandbox and shell into its container"`
	Shell   cli.ShellCmd   `cmd:"" help:"shell into a sandbox container (and start the container, if necessary)"`
	Exec    cli.ExecCmd    `cmd:"" help:"execute a single command in a sanbox"`
	Ls      cli.LsCmd      `cmd:"" help:"list sandboxes"`
	Rm      cli.RmCmd      `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop    cli.StopCmd    `cmd:"" help:"stop sandbox container"`
	Git     cli.GitCmd     `cmd:"" help:"git operations with sandboxes"`
	Version cli.VersionCmd `cmd:"" help:"print version infomation about this command"`
	// TODO: VSCCmd should work here too. "%sandbox-vm> sand vsc" should open a vsc remote window on the host OS as though the user had run "%host> sand vsc <this sandbox name>".
}

func (c *Innie) initSlog() {
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
		f, err = os.CreateTemp("/tmp", "sand-log")
		if err != nil {
			panic(err)
		}
	} else {
		logDir := filepath.Dir(c.LogFile)
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			panic(err)
		}
		f, err = os.OpenFile(c.LogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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

const description = `The "innie" side of the "sand" command, communicates with the host OS to execute sandbox related commands.`

func main() {
	var app Innie
	app.initSlog()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Catch control-C so if you break out of "sand new" because it's taking too long
	// to download a container image (which runs in exec.CommandContext subprocess),
	// we also kill any subprocesses that were started with ctx.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	appBaseDir := "/outie" // connect to the sandd process running on the host via socket.

	kongApp := kong.Must(&app)
	predictorMC, err := mux.NewHTTPClient(ctx, "4242")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandmux client, error: %v\n", err)
		os.Exit(1)
	}
	namePredictor := cli.NewSandboxNamePredictor(predictorMC)

	kongcompletion.Register(kongApp, kongcompletion.WithPredictor("sandbox-name", namePredictor))
	kongCtx := kong.Parse(&app,
		kong.Configuration(kong.JSON, ".sand.json", "~/.sand.json"),
		kong.Description(description))

	// Yes, we already called initSlog(), but this time we've parsed the cli flags which may specify other log settings than the defaults.
	app.initSlog()

	slog.Info("main", "appBaseDir", appBaseDir)

	if app.AppBaseDir == "" {
		app.AppBaseDir = appBaseDir
	}

	mc, err := mux.NewHTTPClient(ctx, app.HTTPPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandmux client, error: %v\n", err)
		os.Exit(1)
	}
	err = kongCtx.Run(&cli.CLIContext{
		MuxClient:  mc,
		Context:    ctx,
		AppBaseDir: appBaseDir,
		LogFile:    app.LogFile,
		LogLevel:   app.LogLevel,
		CloneRoot:  app.AppBaseDir,
	})
	kongCtx.FatalIfErrorf(err)
}
