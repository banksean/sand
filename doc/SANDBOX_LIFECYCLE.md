# `sand.Box` Lifecycle

This diagram illustrates the interaction flow when creating a new sandbox with `sand`.

```mermaid
sequenceDiagram
    participant CLI as CLI
    participant Mux as MuxServer
    participant Boxer
    participant Cloner as WorkspaceCloner
    participant bBox as Box
    participant AC as AppleContainer
    participant Hooks as ContainerStartupHooks

    CLI->>Mux: CreateSandbox(id, dir, image)
    activate Mux
    
    Mux->>Boxer: NewSandbox(id, hostWorkDir, imageName)
    activate Boxer
    
    Note over Boxer: ensureCloner()
    
    Boxer->>Cloner: Prepare(CloneRequest)
    activate Cloner
    
    Note over Cloner: Clone work directory (COW)
    Cloner->>Cloner: cloneWorkDir()
    Note over Cloner: Setup git remotes
    
    Cloner->>Cloner: cloneDotfiles()
    Note over Cloner: Clone .gitconfig, .zshrc, etc.
    
    Cloner->>Cloner: cloneClaudeDir()
    Note over Cloner: Clone .claude directory
    
    Cloner->>Cloner: cloneHostKeyPair()
    Note over Cloner: Copy SSH host keys
    
    Cloner->>Cloner: mountPlanFor()
    Note over Cloner: Define bind mounts:<br/>/hostkeys, /dotfiles, /app
    
    Cloner->>Cloner: defaultContainerStartupHook()
    Note over Cloner: Create bootstrap hook
    
    Cloner-->>Boxer: CloneResult{SandboxWorkDir, Mounts, Hooks}
    deactivate Cloner
    
    Boxer->>Boxer: Create Box struct
    Note over Boxer: Populate with mounts & hooks
    
    Boxer->>Boxer: SaveSandbox(box)
    Note over Boxer: Persist to SQLite DB
    
    Boxer-->>Mux: Box
    deactivate Boxer
     
    Mux->>Boxer: UpdateContainerID(box, containerID)
    Note over Boxer: Update DB with container ID
    
    Mux->>bBox: StartContainer()
    activate bBox
    
    bBox->>AC: Containers.Start(containerID)
    AC-->>bBox: started
    
    Note over bBox: Container is running,<br/>now run hooks
    
    loop For each ContainerStartupHook
        bBox->>Hooks: hook.OnStart(ctx, box)
        activate Hooks
        
        Note over Hooks: Default bootstrap hook:
        Hooks->>bBox: Exec("cp /dotfiles /root")
        bBox->>AC: Containers.Exec(...)
        AC-->>bBox: output
        
        Hooks->>bBox: Exec("cp authorized_keys")
        bBox->>AC: Containers.Exec(...)
        AC-->>bBox: output
        
        Hooks->>bBox: Exec("cp /hostkeys /etc/ssh")
        bBox->>AC: Containers.Exec(...)
        AC-->>bBox: output
        
        Hooks->>bBox: Exec("/usr/sbin/sshd")
        bBox->>AC: Containers.Exec(...)
        AC-->>bBox: output
        
        Hooks-->>bBox: nil (success)
        deactivate Hooks
    end
    
    bBox-->>Mux: nil (success)
    deactivate bBox
    
    Mux-->>CLI: Box
    deactivate Mux
    
    CLI->>bBox: Shell(ctx, env, "/bin/zsh")
    activate bBox
    Note over bBox: User interacts with sandbox
    bBox->>AC: Containers.ExecStream(...)
    Note over AC: Stream I/O to/from container
    deactivate bBox
```

## Key Components

### Boxer
- Central manager for sandbox lifecycle
- Maintains SQLite database of sandboxes
- Delegates workspace setup to `WorkspaceCloner`
- Persists sandbox metadata

### WorkspaceCloner
- Abstracts workspace preparation
- Default implementation clones directories using copy-on-write (COW)
- Sets up git remotes between host and sandbox
- Defines mount specifications for the container
- Creates `ContainerStartupHook` instances for post-start customization

### CloneResult
- Returned by `WorkspaceCloner.Prepare()`
- Contains:
  - `SandboxWorkDir`: Host path to cloned workspace
  - `Mounts`: List of bind mount specifications
  - `ContainerStartupHooks`: Bootstrap logic to run after container starts

### Box
- Represents a single sandbox instance
- Wraps Apple Container operations
- Executes hooks after container starts
- Provides `Shell()` and `Exec()` methods for user interaction

### ContainerStartupHooks
- Run after container starts but before user interaction
- Default hook copies dotfiles, SSH keys, and starts sshd
- Custom hooks can be injected via `WorkspaceCloner`

## Flow Summary

1. **Cloneing Phase**: `Boxer` calls `WorkspaceCloner.Prepare()` to clone workspace and define mounts/hooks
2. **Creation Phase**: `Box` creates container with specified mounts
3. **Bootstrap Phase**: `Box.StartContainer()` runs all `ContainerStartupHooks` to configure the running container
4. **Interactive Phase**: User attaches to container via `Box.Shell()`
