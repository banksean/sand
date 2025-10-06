# Git Remotes Between Host and Sandbox

## Overview

When `sand` creates a new sandbox, it makes a copy-on-write (COW) clone of your original working directory using APFS's `clonefile(2)` functionality. To facilitate git operations between the original directory and the sandbox clone, `sand` automatically sets up **bidirectional git remotes** linking these two checkouts.

## Directory Structure

- **Original working directory (host)**: The directory where you ran `sand new` (e.g., `/Users/yourname/myproject`)
- **Sandbox clone directory (host)**: `${SandBoxer.cloneRoot}/boxen/<sandbox-id>/app`
  - Default `cloneRoot`: `/tmp/sand/boxen`
  - Example full path: `/tmp/sand/boxen/my-sandbox/app`
- **Container mount**: The sandbox clone is mounted to `/app` inside the container

## The Bidirectional Remote Relationship

When creating a sandbox with ID `my-sandbox`, `sand` establishes these git remotes:

### In the sandbox clone (`/tmp/sand/boxen/my-sandbox/app`):
```
Remote name: origin-host-workdir
Remote URL:  /Users/yourname/myproject  (the original host working directory)
```

### In the original working directory (`/Users/yourname/myproject`):
```
Remote name: sandbox-clone-my-sandbox
Remote URL:  /tmp/sand/boxen/my-sandbox/app  (the sandbox clone directory)
```

## Important Caveat: Host-Only Paths

⚠️ **These git remote paths only make sense on the host OS, not inside the container.**

The remote URLs are absolute filesystem paths on the macOS host. Inside the container:
- The path `/tmp/sand/boxen/my-sandbox/app` doesn't exist
- The path `/Users/yourname/myproject` doesn't exist
- Only `/app` (the mounted clone) is visible

This means:
- ✅ `git fetch origin-host-workdir` works on the **host** (from the sandbox clone directory)
- ❌ `git fetch origin-host-workdir` **fails** inside the **container** (paths don't exist)
- ✅ `git fetch sandbox-clone-my-sandbox` works on the **host** (from the original working directory)
- ❌ `git fetch sandbox-clone-my-sandbox` **fails** inside the **container**

## Example: Comparing Original Working Directory to Sandbox

Let's say you have:
- Sandbox ID: `my-sandbox`
- Original working directory: `/Users/yourname/myproject`
- Sandbox clone: `/tmp/sand/boxen/my-sandbox/app`

### From the Host OS

You can diff from either directory:

#### Option 1: From the original working directory
```sh
# First, fetch the latest from the sandbox clone
cd /Users/yourname/myproject
git fetch sandbox-clone-my-sandbox

# Compare your current working tree to the sandbox's main branch
git diff sandbox-clone-my-sandbox/main

# Or compare specific commits/branches
git diff HEAD..sandbox-clone-my-sandbox/main
```

#### Option 2: From the sandbox clone directory
```sh
# First, fetch the latest from the original working directory
cd /tmp/sand/boxen/my-sandbox/app
git fetch origin-host-workdir

# Compare the sandbox's working tree to the original's main branch
git diff origin-host-workdir/main

# Or compare specific commits/branches
git diff HEAD..origin-host-workdir/main
```

### From Inside the Container

Inside the container, you **cannot** use the git remotes directly because the remote URLs point to host filesystem paths. However, you can still perform diffs using commit SHAs or by comparing against the initial clone state:

```sh
# Inside the container at /app
cd /app

# Show uncommitted changes in the sandbox
git status
git diff

# If you know a specific commit SHA from the original repo
git diff <commit-sha>

# Show the log to see what's been done in this sandbox
git log --oneline

# Compare to a specific branch if it was fetched before container creation
git diff main  # only works if main exists locally
```

#### Workaround: Use the Host for Remote Operations

To compare the container's current state with the original working directory:

1. **From a host shell**, commit or fetch the sandbox's changes:
   ```sh
   # On host: check what's in the sandbox clone
   cd /tmp/sand/boxen/my-sandbox/app
   git status
   git add -A
   git commit -m "sandbox changes"
   ```

2. **From the original working directory on host**, fetch and diff:
   ```sh
   cd /Users/yourname/myproject
   git fetch sandbox-clone-my-sandbox
   git diff sandbox-clone-my-sandbox/main
   ```

Alternatively, use `sand shell` to run git commands on the host OS that inspect the sandbox clone's state without entering the container.

## Future Enhancement Ideas

- Implement a host-side service that the container can communicate with to perform `git fetch` operations against the original working directory
- Provide `sand` subcommands like `sand diff <sandbox-id>` to facilitate these comparisons from the host
- Create container-accessible git remotes using a git server protocol or ssh access back to the host