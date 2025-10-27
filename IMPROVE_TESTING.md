# Test Coverage Improvement Plan

## ✅ Completed Refactorings

Three major refactorings have been completed to enable comprehensive testing:

1. **Container Operations** - `container_ops.go` provides ContainerOps and ImageOps interfaces
2. **Git Operations** - `git_ops.go` provides GitOps interface  
3. **File Operations** - `fileops.go` provides FileOps interface

All external dependencies (containers, git, filesystem) are now abstracted behind mockable interfaces. The codebase is ready for comprehensive unit testing!

---

## Current Coverage Status

**box.go**: 0% - No tests at all  
**boxer.go**: ~30% - Basic DB operations tested, but missing integration tests  
**workspace.go**: 0% - No tests at all

**Overall package coverage**: 20.7%

## Detailed Coverage Analysis

### box.go (0% → 80%+ target)

#### Uncovered Functions:
- `GetContainer` (box.go:56) - 0%
- `Sync` (box.go:68) - 0%
- `CreateContainer` (box.go:84) - 0%
- `StartContainer` (box.go:118) - 0%
- `Shell` (box.go:144) - 0%
- `Exec` (box.go:166) - 0%
- `effectiveMounts` (box.go:184) - 0%

#### Required Tests:

1. **GetContainer** - Test with valid/invalid container IDs, empty container lists
2. **Sync** - Test filesystem state validation, container state checking, error states
3. **CreateContainer** - Test container creation with various mount configurations, DNS domains, env files
4. **StartContainer** - Test startup with hooks (success/failure/partial failure scenarios), hook error aggregation
5. **Shell** - Test interactive command execution with stdin/stdout/stderr
6. **Exec** - Test non-interactive command execution, error handling
7. **effectiveMounts** - Test mount generation logic with custom mounts vs. defaults

#### Test Strategy:
- Mock ContainerOps interface (implemented in container_ops.go)
- Mock FileOps interface (implemented in fileops.go) for filesystem operations
- Test error paths (missing directories, failed container operations)
- Test hook execution and error aggregation with `errors.Join`
- Verify all error states are properly set (SandboxWorkDirError, SandboxContainerError)

---

### boxer.go (~30% → 80%+ target)

#### Covered Functions (keep these tests):
✓ `SaveSandbox` (80%) - boxer_test.go:10-92
✓ `Get` (77.8%) - boxer_test.go:94-161
✓ `List` (87.5%) - boxer_test.go:186-236
✓ `UpdateContainerID` (80%) - boxer_test.go:238-281
✓ `loadSandbox` (87.5%) - boxer_test.go:163-184
✓ `sandboxFromDB` (100%)
✓ `toNullString` (100%)
✓ `fromNullString` (100%)
✓ `NewBoxer` (56.2%) - Created in all tests
✓ `Close` (66.7%) - Called in defers

#### Uncovered Functions:
- `Sync` (boxer.go:86) - 0%
- `NewSandbox` (boxer.go:115) - 0%
- `AttachSandbox` (boxer.go:145) - 0%
- `Cleanup` (boxer.go:183) - 0%
- `EnsureImage` (boxer.go:243) - 0%
- `pullImage` (boxer.go:263) - 0%
- `StopContainer` (boxer.go:327) - 0%
- `userMsg` (boxer.go:286) - 0%

#### Required Tests:

1. **Sync** - Test database-filesystem-container synchronization loop, error handling per sandbox
2. **NewSandbox** - Integration test with WorkspaceCloner, verify DB persistence, directory creation
3. **AttachSandbox** - Test re-attaching to existing sandbox from DB
4. **Cleanup** - Test full cleanup (container stop/delete, git remote removal, directory deletion, DB deletion)
5. **EnsureImage** - Test image checking logic (already present vs. needs pull)
6. **pullImage** - Test image pull with user feedback, wait function handling
7. **StopContainer** - Test container stopping, error handling
8. **userMsg** - Test terminal output with/without terminalWriter

#### Test Strategy:
- Mock ContainerOps and ImageOps interfaces (implemented in container_ops.go)
- Mock GitOps interface (implemented in git_ops.go) for git operations
- Use test git repositories for integration tests if needed
- Test error handling in all paths
- Test terminalWriter output capture with bytes.Buffer
- Integration tests with real WorkspaceCloner

---

### workspace.go (0% → 70%+ target)

#### Uncovered Functions (all at 0%):
- `String` (workspace.go:23)
- `Name` (workspace.go:46)
- `OnStart` (workspace.go:50)
- `NewContainerStartupHook` (workspace.go:55)
- `NewDefaultWorkspaceCloner` (workspace.go:93)
- `Prepare` (workspace.go:101)
- `Hydrate` (workspace.go:138)
- `mountPlanFor` (workspace.go:153)
- `userMsg` (workspace.go:172)
- `cloneWorkDir` (workspace.go:180)
- `cloneHostKeyPair` (workspace.go:233)
- `cloneDotfiles` (workspace.go:261)
- `defaultContainerHook` (workspace.go:326)

#### Required Tests:

1. **MountSpec.String** - Unit test for mount string formatting (readonly vs. readwrite)
2. **containerHook** - Test hook name retrieval and execution
3. **NewContainerStartupHook** - Test hook creation and invocation
4. **DefaultWorkspaceCloner.Prepare** - Test full workspace preparation with all cloning steps
5. **DefaultWorkspaceCloner.Hydrate** - Test box hydration with mounts/hooks, error on nil box
6. **mountPlanFor** - Unit test for mount plan generation
7. **cloneWorkDir** - Test git clone, remote setup (both directions), fetch operations
8. **cloneHostKeyPair** - Test SSH key copying to sandbox
9. **cloneDotfiles** - Test dotfile cloning with:
   - Regular files
   - Symlinks (relative and absolute)
   - Missing files (should create empty)
   - Symlinks to missing files (should create empty)
10. **defaultContainerHook** - Test default bootstrap logic, error aggregation

#### Test Strategy:
- Mock GitOps interface (implemented in git_ops.go) for git operations
- Mock FileOps interface (implemented in fileops.go) for filesystem operations
- Test symlink handling comprehensively (relative, absolute, broken)
- Test missing file scenarios
- Mock Box.Exec for hook testing
- Integration tests can use real git and filesystem if needed

---

## Suggested Refactorings

### 1. Extract Container Operations Interface (boxer.go, box.go) ✅ COMPLETED

**Status**: Implemented in `container_ops.go`

**Solution**:
```go
type ContainerOps interface {
    Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error)
    Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error)
    Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error)
    Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error)
    Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env, args []string) (string, error)
    ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error)
    Inspect(ctx context.Context, containerID string) ([]types.Container, error)
}

type ImageOps interface {
    List(ctx context.Context) ([]types.Image, error)
    Pull(ctx context.Context, image string) (func() error, error)
}
```

**Benefit**: Enables mocking for comprehensive unit tests without real containers. Can inject mock services into Boxer and Box.

**Changes Made**:
- Created `container_ops.go` with ContainerOps and ImageOps interfaces
- Box now has `containerOps ContainerOps` field
- Boxer now has `containerOps` and `imageOps` fields
- All direct `ac.Containers.*` and `ac.Images.*` calls replaced with interface methods

---

### 2. Extract Git Operations (workspace.go, boxer.go) ✅ COMPLETED

**Status**: Implemented in `git_ops.go`

**Solution**:
```go
type GitOps interface {
    AddRemote(ctx context.Context, dir, name, url string) error
    RemoveRemote(ctx context.Context, dir, name string) error
    Fetch(ctx context.Context, dir, remote string) error
}
```

**Benefit**: Simplifies testing cloneWorkDir without actual git operations. Enables fast unit tests with mocked git.

**Changes Made**:
- Created `git_ops.go` with GitOps interface and defaultGitOps implementation
- DefaultWorkspaceCloner now has `gitOps GitOps` field
- Boxer now has `gitOps GitOps` field
- cloneWorkDir simplified from ~40 lines to ~10 lines
- Boxer.Cleanup now uses gitOps.RemoveRemote instead of exec.Command

---

### 3. Extract Filesystem Operations (workspace.go, boxer.go) ✅ COMPLETED

**Status**: Implemented in `fileops.go`

**Solution**:
```go
type FileOps interface {
    MkdirAll(path string, perm os.FileMode) error
    Copy(ctx context.Context, src, dst string) error
    Stat(path string) (os.FileInfo, error)
    Lstat(path string) (os.FileInfo, error)
    Readlink(path string) (string, error)
    Create(path string) (*os.File, error)
    RemoveAll(path string) error
    WriteFile(path string, data []byte, perm os.FileMode) error
}
```

**Benefit**: Enables testing without filesystem side effects. Can use in-memory implementations for fast tests.

**Changes Made**:
- Created `fileops.go` with FileOps interface and defaultFileOps implementation
- DefaultWorkspaceCloner now has `fileOps FileOps` field
- Boxer now has `fileOps FileOps` field
- All os.MkdirAll, os.Stat, os.Lstat, os.Readlink, os.Create, os.RemoveAll, os.WriteFile calls replaced
- All exec.Command("cp", ...) calls replaced with fileOps.Copy
- Helper functions like writeKeyToFile and createKeyPairIfMissing now accept FileOps parameter

---

### 4. Separate Hook Registration from Execution (box.go:118-141)

**Current Issue**: `StartContainer` mixes container starting with hook execution, hard to test independently.

**Proposed Solution**:
```go
func (sb *Box) StartContainer(ctx context.Context) error {
    if err := sb.startContainerProcess(ctx); err != nil {
        return err
    }
    return sb.executeHooks(ctx)
}

func (sb *Box) startContainerProcess(ctx context.Context) error {
    slog.InfoContext(ctx, "Box.startContainerProcess", "containerID", sb.ContainerID)
    output, err := ac.Containers.Start(ctx, nil, sb.ContainerID)
    if err != nil {
        slog.ErrorContext(ctx, "startContainerProcess", "error", err, "output", output)
        return err
    }
    slog.InfoContext(ctx, "Box.startContainerProcess succeeded", "output", output)
    return nil
}

func (sb *Box) executeHooks(ctx context.Context) error {
    slog.InfoContext(ctx, "Box.executeHooks", "hookCount", len(sb.ContainerHooks))
    var hookErrs []error
    for _, hook := range sb.ContainerHooks {
        slog.InfoContext(ctx, "Box.executeHooks running hook", "hook", hook.Name())
        if err := hook.OnStart(ctx, sb); err != nil {
            slog.ErrorContext(ctx, "Box.executeHooks hook error", "hook", hook.Name(), "error", err)
            hookErrs = append(hookErrs, fmt.Errorf("%s: %w", hook.Name(), err))
        }
    }
    if len(hookErrs) > 0 {
        return errors.Join(hookErrs...)
    }
    return nil
}
```

**Benefit**: Easier to test hooks independently. Can test hook execution without starting containers. Better separation of concerns.

**Impact**: Small refactor - only affects StartContainer, backwards compatible.

---

### 5. Extract Mount Building Logic (box.go:184-207)

**Current Issue**: `effectiveMounts` is method on Box but is pure logic, hard to test variations.

**Proposed Solution**:
```go
type MountBuilder struct {
    sandboxWorkDir string
    customMounts   []MountSpec
}

func NewMountBuilder(sandboxWorkDir string, customMounts []MountSpec) *MountBuilder {
    return &MountBuilder{
        sandboxWorkDir: sandboxWorkDir,
        customMounts:   customMounts,
    }
}

func (mb *MountBuilder) Build() []MountSpec {
    if len(mb.customMounts) > 0 {
        return mb.customMounts
    }
    if mb.sandboxWorkDir == "" {
        return nil
    }
    return []MountSpec{
        {
            Source:   filepath.Join(mb.sandboxWorkDir, "hostkeys"),
            Target:   "/hostkeys",
            ReadOnly: true,
        },
        {
            Source:   filepath.Join(mb.sandboxWorkDir, "dotfiles"),
            Target:   "/dotfiles",
            ReadOnly: true,
        },
        {
            Source: filepath.Join(mb.sandboxWorkDir, "app"),
            Target: "/app",
        },
    }
}

// In Box:
func (sb *Box) effectiveMounts() []MountSpec {
    return NewMountBuilder(sb.SandboxWorkDir, sb.Mounts).Build()
}
```

**Benefit**: Pure function, easier to test all mount scenarios. Can test independently of Box.

**Impact**: Small refactor - only affects effectiveMounts, backwards compatible.

**Priority**: Low - current code is testable as-is, but this improves clarity.

---

### 6. Error Wrapping Consistency (all files)

**Current Issue**: Some errors include context (sandbox ID, paths), others don't. Inconsistent debugging experience.

**Proposed Standard**:
```go
// Good examples:
return fmt.Errorf("failed to create container for sandbox %s: %w", sb.ID, err)
return fmt.Errorf("sync failed for sandbox %s, workdir %s: %w", sb.ID, sb.SandboxWorkDir, err)

// Current issues:
// box.go:111 - no context about which sandbox
// box.go:122 - no context about which sandbox
// boxer.go:48 - no context about dbPath
```

**Action Items**:
- Audit all error returns
- Add context (sandbox ID, paths, container IDs) to all errors
- Use consistent format: `"operation failed for <resource>: %w"`

**Benefit**: Better debugging and error tracing in production. Easier to correlate logs with errors.

**Impact**: Small refactor - mechanical change, low risk.

---

### 7. Consolidate User Messaging (boxer.go:286, workspace.go:172)

**Current Issue**: Both `Boxer` and `DefaultWorkspaceCloner` have identical `userMsg` methods. Violates DRY.

**Proposed Solution**:
```go
// In new file: usermsg.go
type UserMessenger interface {
    Message(ctx context.Context, msg string)
}

type terminalMessenger struct {
    writer io.Writer
}

func NewTerminalMessenger(writer io.Writer) UserMessenger {
    return &terminalMessenger{writer: writer}
}

func (tm *terminalMessenger) Message(ctx context.Context, msg string) {
    if tm.writer == nil {
        slog.DebugContext(ctx, "userMsg (no writer)", "msg", msg)
        return
    }
    fmt.Fprintln(tm.writer, "\033[90m"+msg+"\033[0m")
}

// Null messenger for tests
type nullMessenger struct{}

func (nm *nullMessenger) Message(ctx context.Context, msg string) {
    slog.DebugContext(ctx, "userMsg (null messenger)", "msg", msg)
}
```

**Benefit**: DRY principle, consistent formatting, easier to test, can capture messages in tests.

**Impact**: Small refactor - replace two methods with interface, update callers.

---

## Priority Test Files to Create

### 1. **box_test.go** (HIGH PRIORITY)
- **Current coverage**: 0%
- **Target coverage**: 80%+
- **Estimated effort**: 2-3 hours
- **Blockers**: Need container service mock
- **Tests to add**:
  - TestGetContainer
  - TestGetContainer_NotFound
  - TestSync_WorkDirMissing
  - TestSync_ContainerMissing
  - TestCreateContainer
  - TestStartContainer_Success
  - TestStartContainer_WithHooks
  - TestStartContainer_HookFailures
  - TestShell
  - TestExec
  - TestEffectiveMounts_Default
  - TestEffectiveMounts_Custom

### 2. **workspace_test.go** (HIGH PRIORITY)
- **Current coverage**: 0%
- **Target coverage**: 70%+
- **Estimated effort**: 3-4 hours
- **Blockers**: Need git repos for testing
- **Tests to add**:
  - TestMountSpecString
  - TestContainerHook
  - TestNewContainerStartupHook
  - TestDefaultWorkspaceCloner_Prepare
  - TestDefaultWorkspaceCloner_Hydrate
  - TestMountPlanFor
  - TestCloneWorkDir
  - TestCloneHostKeyPair
  - TestCloneDotfiles_Regular
  - TestCloneDotfiles_Symlinks
  - TestCloneDotfiles_Missing
  - TestCloneDotfiles_BrokenSymlinks
  - TestDefaultContainerHook

### 3. **boxer_integration_test.go** (MEDIUM PRIORITY)
- **Current coverage**: N/A (new file)
- **Target**: Integration test coverage
- **Estimated effort**: 2-3 hours
- **Tests to add**:
  - TestBoxer_NewSandbox_EndToEnd
  - TestBoxer_Sync_WithRealContainers
  - TestBoxer_Cleanup_EndToEnd
  - TestBoxer_AttachSandbox_AfterRestart
  - TestBoxer_EnsureImage_PullFlow

### 4. **mocks_test.go** or use mockgen (SUPPORT FILE)
- **Estimated effort**: 1-2 hours
- **Contents**:
  - Mock ContainerOps (from container_ops.go)
  - Mock ImageOps (from container_ops.go)
  - Mock GitOps (from git_ops.go)
  - Mock FileOps (from fileops.go)
  - Mock WorkspaceCloner
- **Alternative**: Use `github.com/golang/mock` or `github.com/stretchr/testify/mock`
- **Note**: All major external dependencies now have interfaces ready for mocking!

---

## Quick Wins (Low-Hanging Fruit)

These tests can be written quickly and will significantly improve coverage:

### 1. **MountSpec.String** (workspace.go:23)
```go
func TestMountSpecString(t *testing.T) {
    tests := []struct {
        name string
        spec MountSpec
        want string
    }{
        {
            name: "readonly mount",
            spec: MountSpec{Source: "/host/path", Target: "/container/path", ReadOnly: true},
            want: "type=bind,source=/host/path,target=/container/path,readonly",
        },
        {
            name: "readwrite mount",
            spec: MountSpec{Source: "/host/rw", Target: "/container/rw", ReadOnly: false},
            want: "type=bind,source=/host/rw,target=/container/rw",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := tt.spec.String(); got != tt.want {
                t.Errorf("MountSpec.String() = %v, want %v", got, tt.want)
            }
        })
    }
}
```
**Estimated time**: 5 minutes  
**Coverage gain**: workspace.go function

### 2. **effectiveMounts** (box.go:184)
```go
func TestBox_EffectiveMounts(t *testing.T) {
    tests := []struct {
        name string
        box  *Box
        want int // number of mounts
    }{
        {
            name: "custom mounts",
            box: &Box{
                SandboxWorkDir: "/tmp/sandbox",
                Mounts: []MountSpec{{Source: "/custom", Target: "/target"}},
            },
            want: 1,
        },
        {
            name: "default mounts",
            box: &Box{
                SandboxWorkDir: "/tmp/sandbox",
                Mounts: nil,
            },
            want: 3, // hostkeys, dotfiles, app
        },
        {
            name: "no workdir",
            box: &Box{
                SandboxWorkDir: "",
                Mounts: nil,
            },
            want: 0,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.box.effectiveMounts()
            if len(got) != tt.want {
                t.Errorf("effectiveMounts() returned %d mounts, want %d", len(got), tt.want)
            }
        })
    }
}
```
**Estimated time**: 10 minutes  
**Coverage gain**: box.go function

### 3. **containerHook** (workspace.go:41-52)
```go
func TestContainerHook(t *testing.T) {
    called := false
    hook := NewContainerStartupHook("test-hook", func(ctx context.Context, b *Box) error {
        called = true
        return nil
    })

    if hook.Name() != "test-hook" {
        t.Errorf("Expected name 'test-hook', got %s", hook.Name())
    }

    ctx := context.Background()
    box := &Box{ID: "test"}
    if err := hook.OnStart(ctx, box); err != nil {
        t.Errorf("OnStart() error = %v", err)
    }

    if !called {
        t.Error("Hook function was not called")
    }
}
```
**Estimated time**: 10 minutes  
**Coverage gain**: workspace.go functions

### 4. **mountPlanFor** (workspace.go:153)
```go
func TestDefaultWorkspaceCloner_MountPlanFor(t *testing.T) {
    cloner := &DefaultWorkspaceCloner{}
    mounts := cloner.mountPlanFor("/tmp/sandbox")

    if len(mounts) != 3 {
        t.Errorf("Expected 3 mounts, got %d", len(mounts))
    }

    // Verify hostkeys mount
    if mounts[0].Source != "/tmp/sandbox/hostkeys" || mounts[0].Target != "/hostkeys" || !mounts[0].ReadOnly {
        t.Errorf("Invalid hostkeys mount: %+v", mounts[0])
    }

    // Verify dotfiles mount
    if mounts[1].Source != "/tmp/sandbox/dotfiles" || mounts[1].Target != "/dotfiles" || !mounts[1].ReadOnly {
        t.Errorf("Invalid dotfiles mount: %+v", mounts[1])
    }

    // Verify app mount
    if mounts[2].Source != "/tmp/sandbox/app" || mounts[2].Target != "/app" || mounts[2].ReadOnly {
        t.Errorf("Invalid app mount: %+v", mounts[2])
    }
}
```
**Estimated time**: 15 minutes  
**Coverage gain**: workspace.go function

### 5. **toNullString/fromNullString** (Already at 100%)
These helper functions already have perfect coverage from existing tests. Good pattern to follow!

---

## Testing Best Practices to Follow

### 1. Table-Driven Tests
Use for functions with multiple scenarios:
```go
tests := []struct {
    name    string
    input   Type
    want    Type
    wantErr bool
}{
    // test cases
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test body
    })
}
```

### 2. Test Fixtures
Create reusable test data:
```go
func newTestBox(t *testing.T) *Box {
    t.Helper()
    return &Box{
        ID:             "test-id",
        ContainerID:    "test-container",
        SandboxWorkDir: t.TempDir(),
        ImageName:      "test-image",
    }
}
```

### 3. Cleanup with t.Cleanup or defer
```go
func TestSomething(t *testing.T) {
    tmpDir := t.TempDir() // auto-cleanup
    // or
    tmpDir, _ := os.MkdirTemp("", "test-*")
    t.Cleanup(func() { os.RemoveAll(tmpDir) })
}
```

### 4. Use Subtests for Isolation
```go
t.Run("success case", func(t *testing.T) {
    // isolated test
})
t.Run("error case", func(t *testing.T) {
    // isolated test
})
```

### 5. Test Error Messages
Don't just check `err != nil`, verify error content:
```go
if err == nil {
    t.Fatal("expected error, got nil")
}
if !strings.Contains(err.Error(), "expected message") {
    t.Errorf("error message = %v, want substring 'expected message'", err)
}
```

### 6. Use t.Helper() in Helper Functions
```go
func assertNoError(t *testing.T, err error) {
    t.Helper() // makes test failures point to caller
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

---

## Test Execution Strategy

### Phase 1: Quick Wins (1 day)
- Write tests for pure functions: MountSpec.String, effectiveMounts, mountPlanFor, containerHook
- Target: 10-15% coverage increase
- No refactoring needed

### Phase 2: Unit Tests (2-3 days)
- Create mocks for external dependencies
- Write unit tests for all uncovered functions
- Target: 60% coverage
- May require minimal refactoring (extract interfaces)

### Phase 3: Integration Tests (2-3 days)
- Write end-to-end tests with real containers (if possible)
- Write integration tests with real git repos
- Target: 80% coverage
- Verify all workflows work together

### Phase 4: Refactoring (1-2 days)
- Implement suggested refactorings based on test insights
- Update tests to use new interfaces
- Target: 85%+ coverage, better maintainability

---

## Success Metrics

- **box.go**: 0% → 80%+ coverage
- **boxer.go**: 30% → 80%+ coverage
- **workspace.go**: 0% → 70%+ coverage
- **Overall package**: 20.7% → 75%+ coverage
- **All public APIs tested**
- **Critical error paths covered**
- **CI passing with race detector**: `GOEXPERIMENT=synctest go test -race ./...`

---

## Notes

- Existing `boxer_test.go` has good patterns to follow (table-driven, temp dirs, cleanup)
- The BUG comment in workspace.go:76 indicates `Hydrate` isn't being called - should add test and fix this
- Consider using `t.TempDir()` instead of `os.MkdirTemp` in new tests (auto-cleanup)
- All tests should work with `GOEXPERIMENT=synctest` flag
- Consider adding `-short` flag support to skip slow integration tests

---

## Refactoring Implementation Summary

The following interfaces have been implemented to enable comprehensive mocking and testing:

### container_ops.go
```go
type ContainerOps interface {
    Create, Start, Stop, Delete, Exec, ExecStream, Inspect
}
type ImageOps interface {
    List, Pull
}
// Default implementation: appleContainerOps, appleImageOps
```

### git_ops.go
```go
type GitOps interface {
    AddRemote(ctx, dir, name, url)
    RemoveRemote(ctx, dir, name)
    Fetch(ctx, dir, remote)
}
// Default implementation: defaultGitOps
```

### fileops.go
```go
type FileOps interface {
    MkdirAll, Copy, Stat, Lstat, Readlink, Create, RemoveAll, WriteFile
}
// Default implementation: defaultFileOps
```

**Impact**:
- Box struct now has `containerOps ContainerOps` field
- Boxer struct now has `containerOps`, `imageOps`, `gitOps`, and `fileOps` fields
- DefaultWorkspaceCloner now has `gitOps` and `fileOps` fields
- All direct calls to `applecontainer`, `exec.Command("git",...)`, `exec.Command("cp",...)`, and `os.*` file operations replaced with interface methods
- Ready for comprehensive unit testing with mock implementations
