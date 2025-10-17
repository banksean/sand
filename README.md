[![Go Reference](https://pkg.go.dev/badge/github.com/banksean/sand.svg)](https://pkg.go.dev/github.com/banksean/sand) 
[![Main Commit Queue](https://github.com/banksean/sand/actions/workflows/queue-main.yml/badge.svg)](https://github.com/banksean/sand/actions/workflows/queue-main.yml)

If you don't write code on a Mac, and you don't deploy it to Linux, you can stop reading this now.

# TL;DR

```sh
% go install github.com/banksean/sand/cmd/sand
% sand new your-new-sandbox-name
```

You are now root, in a Linux container, with your CWD set to a copy-on-write clone of the directory you were at when you ran `sand` on your MacOS host. 

## You work with a sandboxed clone of `./`
The sandbox starts out with a clone of your MacOS current directory, mounted as `/app` inside the container. 

This operation actually uses much less disk space than a full copy of the original directory, because `sand` clones it using copy-on-write (via APFS's `clonefile(2)` call). Additional disk space is only required when you make changes to the cloned files.

The original files on your MacOS host filesystem are not affected by changes made to the clones of those files inside the sandbox.

## Getting changes out of the sandbox

You can use `git` commands to push changes from the container to github, or wherever your origin is. 

Git ssh authentication passes from your MacOS host through `sand` containers, via `ssh-agent`. This means that if the git checkout on your MacOS host is authenticated with ssh (`git remote -v origin` prints something that starts with `git@github.com:...`), then you don't need to log in again inside the container to make git push/pull to work.  

Using `ssh-agent` also means you don't have to leave copies of your github credentials scattered around in places where they shouldn't be.

See [cmd/sand/GIT_REMOTES.md](cmd/sand/GIT_REMOTES.md) for a more detailed explanation of how `sand` uses git locally to link the MacOS-side clones back to your original working directory.  If you are a git enthusiast, you can probably figure out how move changes around between your MacOS host and your sandbox containers without involving github at all.

## Some other handy commands

```sh
$ sand --help # a much more complete list of commands and flags
% sand ls # lists your current sandboxes
% sand git status your-sandbox-name # prints the results of running "git status" in the sandbox's working directory
% sand git diff your-sandbox-name # compares your working directory to the sandbox's clone of your working directory
% sand vsc your-sandbox-name # launches a vscode window, connected "remotely" to your-sandbox-name
% sand shell your-sandbox-name # open a new shell into the your-sandbox-name's container
% sand stop your-sanbox-name # stops the sandbox container, but does *not* delete its filesystem
% sand rm your-sandbox-name # stops and removes the container, and *does* remove the sandbox's filesystem.
```

For more information about `sand`'s subcommands and other options, see [cmd/sand/HELP.md](./cmd/sand/HELP.md)

## Requirements
- Only works on Apple hardware (of course).
- Install [`apple/container`](https://github.com/apple/container) first, since these helper functions just shell out to it. 
