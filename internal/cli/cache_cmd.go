package cli

import (
	"fmt"
)

type CacheCmd struct {
	HTTPProxy HTTPProxyCacheCmd `cmd:"" name:"http-proxy" help:"manage the shared HTTP proxy cache service"`
}

type HTTPProxyCacheCmd struct {
	Start   HTTPProxyCacheStartCmd   `cmd:"" help:"start the shared HTTP proxy cache service"`
	Status  HTTPProxyCacheStatusCmd  `cmd:"" help:"show shared HTTP proxy cache service status"`
	Stop    HTTPProxyCacheStopCmd    `cmd:"" help:"stop the shared HTTP proxy cache service"`
	Restart HTTPProxyCacheRestartCmd `cmd:"" help:"restart the shared HTTP proxy cache service"`
	Clear   HTTPProxyCacheClearCmd   `cmd:"" help:"remove the shared HTTP proxy cache service and cached data"`
}

type (
	HTTPProxyCacheStartCmd   struct{}
	HTTPProxyCacheStatusCmd  struct{}
	HTTPProxyCacheStopCmd    struct{}
	HTTPProxyCacheRestartCmd struct{}
	HTTPProxyCacheClearCmd   struct{}
)

func (c *HTTPProxyCacheStartCmd) Run(cctx *CLIContext) error {
	return cctx.Daemon.HTTPProxyCache(cctx.Context, "start")
}

func (c *HTTPProxyCacheStatusCmd) Run(cctx *CLIContext) error {
	status, err := cctx.Daemon.HTTPProxyCacheStatus(cctx.Context)
	if err != nil {
		return err
	}
	fmt.Printf("Name: %s\n", status.Name)
	fmt.Printf("State: %s\n", status.State)
	fmt.Printf("Running: %t\n", status.Running)
	fmt.Printf("Image: %s\n", status.Image)
	fmt.Printf("URL: %s\n", status.URL)
	fmt.Printf("Cache dir: %s\n", status.CacheDir)
	return nil
}

func (c *HTTPProxyCacheStopCmd) Run(cctx *CLIContext) error {
	return cctx.Daemon.HTTPProxyCache(cctx.Context, "stop")
}

func (c *HTTPProxyCacheRestartCmd) Run(cctx *CLIContext) error {
	return cctx.Daemon.HTTPProxyCache(cctx.Context, "restart")
}

func (c *HTTPProxyCacheClearCmd) Run(cctx *CLIContext) error {
	return cctx.Daemon.HTTPProxyCache(cctx.Context, "clear")
}
