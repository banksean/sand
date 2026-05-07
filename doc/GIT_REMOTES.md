# Git Remotes Between Host and Sandbox

TL;DR: Use git pull (but not push) to move commits between host and sandbox containers.

Sand's pull-only git sync workflow prioritizes host safety over agent capabilities.

The sandbox may consume host changes directly, but the host remains the gatekeeper for accepting sandbox changes.

## Why Pull-Only

The pull-only model makes the direction of authority explicit: each side imports changes from the other, but the sandbox cannot write directly back to the host repository through origin.

Some benefits (and trade-offs) of doing it this way:

- Host safety is more important than agent autonomy. A container process cannot accidentally or maliciously git push into the original host checkout through its origin; the host (i.e. the human at the keyboard) decides when to import sandbox commits.
- The workflow is less symmetrical. You may expect git push origin branch from /app to work. Instead, you need to commit in the sandbox, then run git pull sand/<sandboxname> <branch> from the host checkout to achieve the same result.
- Final adoption remains host-controlled. Sandbox agents can pull from the host, rebase or merge, resolve conflicts, and commit the result in the sandbox. But they cannot push that result into the host checkout; the user or host-side automation must still run git pull sand/<sandbox> <branch> to accept it.
- Sandbox updates from host are simple after the shared mirror has been refreshed. `git pull` from `/app` brings in committed host-side changes because `origin` points at a read-only mirror.
- Publishing sandbox work from host to other remotes requires one extra mental step. The sandbox can produce commits, but the host must pull them. This is safer, but slightly more manual than allowing agents to push directly.
- It favors review before adoption. Because changes are imported from the host side, users can fetch, diff, inspect, or pull intentionally rather than having sandbox work appear in the host checkout automatically.

## Implementation Details

When `sand` creates a new sandbox, it makes a copy-on-write (COW) clone of your original working directory using APFS's [`clonefile(2)`](https://eclecticlight.co/2020/04/14/copy-move-and-clone-files-in-apfs-a-primer/). 

It then automatically sets up git remotes linking the original checkout, a sand-managed shared bare mirror, and the sandbox's cloned checkout. APFS requires the CoW clone to live _on the same volume_ as the original.

Host-to-sandbox updates flow through the shared mirror. Sandbox-to-host updates flow through the `sand/<sandboxname>` remote added to the original checkout.

## Directory Structure

- **Original working directory (host)**: The directory where you ran `sand new` (e.g., `/Users/yourname/myproject`)
- **Shared host mirror (host)**: `${--app-base-dir}/git-mirrors/<repo-id>.git`
- **Sandbox clone directory (host)**: `${--app-base-dir}/clones/<sandbox-id>/app`
  - Default `--app-base-dir`: `~/Library/Application\ Support/Sand`
  - Example full path: `~/Library/Application\ Support/Sand/clones/3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b/app`
- **Container mount**: The sandbox clone is mounted to `/app` inside the container

## The Remote Relationship

When creating a sandbox named `my-sandbox` with ID `3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b` and current working directory `/Users/yourname/myproject`, `sand` establishes these git remotes:

### In the sandbox clone, on the host

Example clone dir: `~/Library/Application\ Support/Sand/clones/3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b/app`

```
Remote name: origin
Fetch URL:   ~/Library/Application\ Support/Sand/git-mirrors/<repo-id>.git
Push URL:    DISABLED
```

The sandbox branch upstream is set to `origin/<branch>`, so plain `git pull` pulls from the shared mirror instead of any upstream config copied from the original checkout.

### In the sandbox clone, as mounted inside the container at `/app`

```
Remote name: origin
Fetch URL:   /run/git-origin-ro  (read-only bind mount of the shared host mirror)
Push URL:    DISABLED
```

`/run/git-origin-ro` is a read-only bind mount of sand's shared bare mirror for the original host repository, not the live host checkout itself.

### In the original working directory

Example working dir: `/Users/yourname/myproject`

```
Remote name: sand/my-sandbox
Remote URL:  ~/Library/Application\ Support/Sand/clones/3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b/app  (the sandbox clone directory)
```

## How To Move Changes Between Host and Sandbox

`sand` sets up remotes for both directions, but they are used from different sides.

Inside the sandbox container, `/app` has an `origin` remote whose fetch URL is `/run/git-origin-ro`. That path is a read-only bind mount of sand's shared bare mirror for the original host repository, so `git pull` from `/app` can bring committed host changes into the sandbox after sand updates the mirror. Pushing back through this remote is intentionally disabled: `origin`'s push URL is set to `DISABLED`.

To move commits from the sandbox back to your original host checkout, run git from the host checkout and pull from the sandbox remote that `sand` added there:

```sh
cd /Users/yourname/myproject
git pull sand/my-sandbox <branchname>
```

In short: update the shared host mirror with sandbox creation, sandbox start, or `sand git sync-host <sandboxname>`; pull host changes into the sandbox with `git pull` from `/app`; pull sandbox changes back to the host with `git pull sand/<sandboxname> <branchname>` from the host checkout.

Because deleted sandbox names can be reused, creating a new active sandbox with the same name replaces the host-side `sand/<sandboxname>` remote so it points at the new sandbox clone.

Sand does not migrate old sandbox containers across changes to this git mirror model. After upgrading across this change, remove and recreate existing sandboxes.

## Example: Comparing Original Working Directory to Sandbox

Let's say you have:
- Sandbox name: `my-sandbox`
- Sandbox ID: `3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b`
- Original working directory: `/Users/yourname/myproject`
- Sandbox clone: `~/Library/Application\ Support/Sand/clones/3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b/app`

### Using the `sand git` Commands

Sand provides some convenience sub-commands that wrap a few common git operations.

#### Viewing Sandbox Status

To see the git status of a sandbox's working tree:

```sh
# From anywhere on the host
sand git status my-sandbox
```

This runs `git status` in the sandbox's working directory and shows you what files have been modified, staged, or are untracked.

#### Viewing Sandbox Log

To see the git commit log of a sandbox's working tree:

```sh
# From anywhere on the host
sand git log my-sandbox
```

This runs `git log` in the sandbox's working directory and shows you the commit history.

#### Syncing Host Commits

To refresh the shared mirror for a sandbox's original host repo:

```sh
# From anywhere on the host
sand git sync-host my-sandbox
```

After this succeeds, run `git pull` inside `/app` in the sandbox to fetch committed host changes from the refreshed mirror.

#### Comparing with Diff

The easiest way to compare your working directory with a sandbox is to use the built-in diff command:

```sh
# From your original working directory
cd /Users/yourname/myproject

# Compare with committed changes in the sandbox
sand git diff my-sandbox

# Include uncommitted changes from the sandbox's working tree
sand git diff --include-uncommitted my-sandbox
# or use the short flag
sand git diff -u my-sandbox

# Diff against a specific branch (default is the active host branch)
sand git diff -b main my-sandbox
```

The `--include-uncommitted` flag is useful when you want to see all changes in the sandbox, including files that haven't been committed yet. This creates a temporary commit in the sandbox, fetches it, shows the diff, and then cleans up the temporary commit automatically.

### Manual Git Operations

You can also diff manually from either side:

#### Option 1: From the original working directory
```sh
# First, fetch the latest from the sandbox clone
cd /Users/yourname/myproject
git fetch sand/my-sandbox

# Compare your current working tree to the sandbox's main branch
git diff sand/my-sandbox/main

# Or compare specific commits/branches
git diff HEAD..sand/my-sandbox/main
```

#### Option 2: From inside the container at `/app`
```sh
# Pull the latest host commits into the sandbox
cd /app
git pull

# Compare the sandbox's working tree to the original's main branch
git diff origin/main

# Or compare specific commits/branches
git diff HEAD..origin/main
```

### From Inside the Container

Inside the container, use `origin` to pull host commits into the sandbox. Do not push to `origin`; it is intentionally disabled.

```sh
# Inside the container at /app
cd /app

# Pull the latest committed host changes from the refreshed shared mirror
git pull

# Show uncommitted changes in the sandbox
git status
git diff

# If you know a specific commit SHA from the original repo
git diff <commit-sha>

# Show the log to see what's been done in this sandbox
git log --oneline

# Compare to a fetched host branch
git diff origin/main
```

#### Pulling Sandbox Commits Back to the Host

To bring committed sandbox changes back to the original working directory:

1. **Inside the container**, commit the sandbox changes:
   ```sh
   cd /app
   git status
   git add -A
   git commit -m "sandbox changes"
   ```

2. **From the original working directory on the host**, pull from the sandbox remote:
   ```sh
   cd /Users/yourname/myproject
   git pull sand/my-sandbox <branchname>
   ```

Alternatively, use `sand shell` to run git commands on the host OS that inspect the sandbox clone's state without entering the container.
