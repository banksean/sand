# Building `sand` locally

Instructions for building `sand` from source.

## Prerequisites
- macOS 26+
- [mise](https://mise.en.dev/) manages tool installation and version pinning

After installing mise, run `mise activate --help` for instructions to activate automatically in your preferred shell.  If you use `zsh` it will be something like:

```sh
echo 'eval "$(mise activate zsh)"' >> ~/.zshrc
```

Note: You may need to run `source ~/.zshrc` after the above command, just to activate it in the shell you installed mise from.

## Basic Tasks

These are regularly used tasks.

### Build from source
```sh
task build
```

### Test
```sh
task test
```

### Install from source
```sh
task install
```

## Observability Tasks (optional, advanced)

These tasks start/stop some optional observability service containers. They require some extra configuration (like specific environment variables to be set) in order to work properly. See [observability/README.md](./observability/README.md) for more information on how this works.

### Start observability services

```sh
task start-observability
```


### Stop observability services

```sh
task stop-observability
```
