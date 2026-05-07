package cli

import (
	"slices"
	"testing"

	"github.com/alecthomas/kong"
)

// kongParse is a helper that creates a Kong parser for the given struct
// and parses the given args, returning the parse context or failing the test.
func kongParse(t *testing.T, target any, args []string) *kong.Context {
	t.Helper()
	k := kong.Must(target)
	ctx, err := k.Parse(args)
	if err != nil {
		t.Fatalf("kong.Parse(%v) error: %v", args, err)
	}
	return ctx
}

func TestShellFlagsDefaults(t *testing.T) {
	var cli struct {
		Shell ShellFlags `embed:""`
	}
	kongParse(t, &cli, []string{})
	if cli.Shell.Shell != "/bin/zsh" {
		t.Errorf("expected default shell /bin/zsh, got %q", cli.Shell.Shell)
	}
	if cli.Shell.Atch {
		t.Error("expected Atch=false by default")
	}
}

func TestShellFlagsShortFlag(t *testing.T) {
	var cli struct {
		Shell ShellFlags `embed:""`
	}
	kongParse(t, &cli, []string{"-s", "/bin/bash"})
	if cli.Shell.Shell != "/bin/bash" {
		t.Errorf("expected /bin/bash, got %q", cli.Shell.Shell)
	}
}

func TestShellFlagsAtchFlag(t *testing.T) {
	var cli struct {
		Shell ShellFlags `embed:""`
	}
	kongParse(t, &cli, []string{"--atch"})
	if !cli.Shell.Atch {
		t.Error("expected Atch=true with --atch")
	}
}

func TestSSHAgentFlagDefaults(t *testing.T) {
	var cli struct {
		SSHAgentFlag `embed:""`
	}
	kongParse(t, &cli, []string{})
	if cli.SSHAgent {
		t.Error("expected SSHAgent=false by default")
	}
}

func TestSSHAgentFlagLongFlag(t *testing.T) {
	var cli struct {
		SSHAgentFlag `embed:""`
	}
	kongParse(t, &cli, []string{"--ssh-agent"})
	if !cli.SSHAgent {
		t.Error("expected SSHAgent=true with --ssh-agent")
	}
}

func TestSandboxCreationFlagsDefaults(t *testing.T) {
	var cli struct {
		SandboxCreationFlags `embed:""`
	}
	kongParse(t, &cli, []string{})
	if cli.EnvFile != ".env" {
		t.Errorf("expected default EnvFile .env, got %q", cli.EnvFile)
	}
	if cli.ImageName != "" {
		t.Errorf("expected empty ImageName, got %q", cli.ImageName)
	}
	if cli.CloneFromDir != "" {
		t.Errorf("expected empty CloneFromDir, got %q", cli.CloneFromDir)
	}
	if cli.Rm {
		t.Error("expected Rm=false by default")
	}
}

func TestSandboxCreationFlagsShortFlags(t *testing.T) {
	var cli struct {
		SandboxCreationFlags `embed:""`
	}
	kongParse(t, &cli, []string{
		"-i", "myimage:latest",
		"-d", "/some/dir",
		"-e", "prod.env",
		"--rm",
	})
	if cli.ImageName != "myimage:latest" {
		t.Errorf("expected ImageName myimage:latest, got %q", cli.ImageName)
	}
	if cli.CloneFromDir != "/some/dir" {
		t.Errorf("expected CloneFromDir /some/dir, got %q", cli.CloneFromDir)
	}
	if cli.EnvFile != "prod.env" {
		t.Errorf("expected EnvFile prod.env, got %q", cli.EnvFile)
	}
	if !cli.Rm {
		t.Error("expected Rm=true")
	}
}

func TestSandboxCreationFlagsImageLongFlag(t *testing.T) {
	var cli struct {
		SandboxCreationFlags `embed:""`
	}
	kongParse(t, &cli, []string{"--image", "myimage:latest"})
	if cli.ImageName != "myimage:latest" {
		t.Errorf("expected ImageName myimage:latest, got %q", cli.ImageName)
	}
}

func TestProjectEnvFlagDefaults(t *testing.T) {
	var cli struct {
		ProjectEnvFlag `embed:""`
	}
	kongParse(t, &cli, []string{})
	if cli.ProjectEnv {
		t.Error("expected ProjectEnv=false by default")
	}
}

func TestProjectEnvFlagLongFlag(t *testing.T) {
	var cli struct {
		ProjectEnvFlag `embed:""`
	}
	kongParse(t, &cli, []string{"--project-env"})
	if !cli.ProjectEnv {
		t.Error("expected ProjectEnv=true with --project-env")
	}
}

func TestSandboxNameFlagArg(t *testing.T) {
	var cli struct {
		SandboxNameFlag `embed:""`
	}
	kongParse(t, &cli, []string{"my-sandbox"})
	if cli.SandboxName != "my-sandbox" {
		t.Errorf("expected SandboxName my-sandbox, got %q", cli.SandboxName)
	}
}

func TestMultiSandboxNameFlagsName(t *testing.T) {
	var cli struct {
		MultiSandboxNameFlags `embed:""`
	}
	kongParse(t, &cli, []string{"my-sandbox"})
	if !slices.Equal(cli.SandboxNames, []string{"my-sandbox"}) {
		t.Errorf("expected SandboxNames [my-sandbox], got %q", cli.SandboxNames)
	}
	if cli.All {
		t.Error("expected All=false")
	}
}

func TestMultiSandboxNameFlagsNames(t *testing.T) {
	var cli struct {
		MultiSandboxNameFlags `embed:""`
	}
	kongParse(t, &cli, []string{"first-sandbox", "second-sandbox"})
	if !slices.Equal(cli.SandboxNames, []string{"first-sandbox", "second-sandbox"}) {
		t.Errorf("expected SandboxNames [first-sandbox second-sandbox], got %q", cli.SandboxNames)
	}
	if cli.All {
		t.Error("expected All=false")
	}
}

func TestMultiSandboxNameFlagsAll(t *testing.T) {
	var cli struct {
		MultiSandboxNameFlags `embed:""`
	}
	kongParse(t, &cli, []string{"--all"})
	if !cli.All {
		t.Error("expected All=true")
	}
}

func TestMultiSandboxNameFlagsAllShort(t *testing.T) {
	var cli struct {
		MultiSandboxNameFlags `embed:""`
	}
	kongParse(t, &cli, []string{"-a"})
	if !cli.All {
		t.Error("expected All=true with -a short flag")
	}
}

func TestMultiSandboxNameFlagsNoArgs(t *testing.T) {
	var cli struct {
		MultiSandboxNameFlags `embed:""`
	}
	kongParse(t, &cli, []string{})
	if len(cli.SandboxNames) != 0 {
		t.Errorf("expected empty SandboxNames, got %q", cli.SandboxNames)
	}
	if cli.All {
		t.Error("expected All=false")
	}
}

func TestNewCmdDefaults(t *testing.T) {
	var cli struct {
		New NewCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"new"})
	if cli.New.Shell != "/bin/zsh" {
		t.Errorf("expected default shell /bin/zsh, got %q", cli.New.Shell)
	}
	if cli.New.EnvFile != ".env" {
		t.Errorf("expected default EnvFile .env, got %q", cli.New.EnvFile)
	}
	if cli.New.Agent != "" {
		t.Errorf("expected default Agent '', got %q", cli.New.Agent)
	}
	if cli.New.Branch {
		t.Error("expected Branch=false by default")
	}
	if cli.New.SSHAgent {
		t.Error("expected SSHAgent=false by default")
	}
}

func TestNewCmdWithSandboxName(t *testing.T) {
	var cli struct {
		New NewCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"new", "my-box"})
	if cli.New.SandboxName != "my-box" {
		t.Errorf("expected SandboxName my-box, got %q", cli.New.SandboxName)
	}
}

func TestNewCmdFlags(t *testing.T) {
	var cli struct {
		New NewCmd `cmd:""`
	}
	kongParse(t, &cli, []string{
		"new",
		"-i", "myimage:v2",
		"--ssh-agent",
		"-a", "claude",
		"-b",
		"-s", "/bin/sh",
		"sandbox-42",
	})
	if cli.New.ImageName != "myimage:v2" {
		t.Errorf("expected ImageName myimage:v2, got %q", cli.New.ImageName)
	}
	if cli.New.Agent != "claude" {
		t.Errorf("expected Cloner claude, got %q", cli.New.Agent)
	}
	if !cli.New.SSHAgent {
		t.Error("expected SSHAgent=true")
	}
	if !cli.New.Branch {
		t.Error("expected Branch=true")
	}
	if cli.New.Shell != "/bin/sh" {
		t.Errorf("expected Shell /bin/sh, got %q", cli.New.Shell)
	}
	if cli.New.SandboxName != "sandbox-42" {
		t.Errorf("expected SandboxName sandbox-42, got %q", cli.New.SandboxName)
	}
}

func TestLogCmdWithSandboxName(t *testing.T) {
	var cli struct {
		Log SandboxLogCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"log", "sandbox-42"})
	if cli.Log.SandboxName != "sandbox-42" {
		t.Errorf("expected SandboxName sandbox-42, got %q", cli.Log.SandboxName)
	}
}

func TestShellCmdDefaults(t *testing.T) {
	var cli struct {
		Shell ShellCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"shell", "my-box"})
	if cli.Shell.SSHAgent {
		t.Error("expected sand shell to disable SSHAgent by default")
	}
	if cli.Shell.ProjectEnv {
		t.Error("expected sand shell to disable ProjectEnv by default")
	}
}

func TestShellCmdSSHAgentFlag(t *testing.T) {
	var cli struct {
		Shell ShellCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"shell", "--ssh-agent", "my-box"})
	if !cli.Shell.SSHAgent {
		t.Error("expected sand shell --ssh-agent to enable SSHAgent")
	}
}

func TestShellCmdProjectEnvFlag(t *testing.T) {
	var cli struct {
		Shell ShellCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"shell", "--project-env", "my-box"})
	if !cli.Shell.ProjectEnv {
		t.Error("expected sand shell to set ProjectEnv with --project-env")
	}
}

func TestStopCmdSandboxName(t *testing.T) {
	var cli struct {
		Stop StopCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"stop", "my-box"})
	if !slices.Equal(cli.Stop.SandboxNames, []string{"my-box"}) {
		t.Errorf("expected SandboxNames [my-box], got %q", cli.Stop.SandboxNames)
	}
	if cli.Stop.All {
		t.Error("expected All=false")
	}
}

func TestStopCmdSandboxNames(t *testing.T) {
	var cli struct {
		Stop StopCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"stop", "my-box", "other-box"})
	if !slices.Equal(cli.Stop.SandboxNames, []string{"my-box", "other-box"}) {
		t.Errorf("expected SandboxNames [my-box other-box], got %q", cli.Stop.SandboxNames)
	}
}

func TestStopCmdAll(t *testing.T) {
	var cli struct {
		Stop StopCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"stop", "-a"})
	if !cli.Stop.All {
		t.Error("expected All=true")
	}
}

func TestRmCmdSandboxName(t *testing.T) {
	var cli struct {
		Rm RmCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"rm", "target-box"})
	if !slices.Equal(cli.Rm.SandboxNames, []string{"target-box"}) {
		t.Errorf("expected SandboxNames [target-box], got %q", cli.Rm.SandboxNames)
	}
}

func TestRmCmdSandboxNames(t *testing.T) {
	var cli struct {
		Rm RmCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"rm", "target-box", "old-box"})
	if !slices.Equal(cli.Rm.SandboxNames, []string{"target-box", "old-box"}) {
		t.Errorf("expected SandboxNames [target-box old-box], got %q", cli.Rm.SandboxNames)
	}
}

func TestStartCmdSandboxNames(t *testing.T) {
	var cli struct {
		Start StartCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"start", "first-box", "second-box"})
	if !slices.Equal(cli.Start.SandboxNames, []string{"first-box", "second-box"}) {
		t.Errorf("expected SandboxNames [first-box second-box], got %q", cli.Start.SandboxNames)
	}
}

func TestRmCmdAll(t *testing.T) {
	var cli struct {
		Rm RmCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"rm", "--all"})
	if !cli.Rm.All {
		t.Error("expected All=true")
	}
}

func TestRmCmdForceShort(t *testing.T) {
	var cli struct {
		Rm RmCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"rm", "-f", "target-box"})
	if !cli.Rm.Force {
		t.Error("expected Force=true")
	}
}

func TestRmCmdForceLong(t *testing.T) {
	var cli struct {
		Rm RmCmd `cmd:""`
	}
	kongParse(t, &cli, []string{"rm", "--force", "target-box"})
	if !cli.Rm.Force {
		t.Error("expected Force=true")
	}
}

func TestSandboxCreationFlagsVolume(t *testing.T) {
	var cli struct {
		SandboxCreationFlags `embed:""`
	}
	kongParse(t, &cli, []string{
		"-v", "/host/path:/container/path",
	})
	if len(cli.Volume) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(cli.Volume))
	}
	if cli.Volume[0] != "/host/path:/container/path" {
		t.Errorf("expected Volume /host/path:/container/path, got %q", cli.Volume[0])
	}
}

func TestSandboxCreationFlagsMultipleVolumes(t *testing.T) {
	var cli struct {
		SandboxCreationFlags `embed:""`
	}
	kongParse(t, &cli, []string{
		"-v", "/path1:/mount1",
		"--volume", "/path2:/mount2",
	})
	if len(cli.Volume) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(cli.Volume))
	}
	if cli.Volume[0] != "/path1:/mount1" {
		t.Errorf("expected first volume /path1:/mount1, got %q", cli.Volume[0])
	}
	if cli.Volume[1] != "/path2:/mount2" {
		t.Errorf("expected second volume /path2:/mount2, got %q", cli.Volume[1])
	}
}
