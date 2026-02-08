# How to use `sand` to sandbox claude code on your mac

You will need to install `sand` first, if you haven't done so yet.

```sh
% go install github.com/banksean/sand/cmd/sand@latest
```

## One-time (or once a year, rather) setup

On your macOS host machine, run:

```sh
% claude setup-token
```

Follow the directions to do the browser copy-and-paste dance, and then save that token value somewhere (e.g. your `~/.env` file).

## Run `sand new --env-file .env` and then run `claude` in the container


```sh
[macos host shell] % sand new --env-file .env

# ... container starts up ...

[linux container shell] % claude

# ... "You're Absolutely Right" ...
```

You should now have a claude code session running in a sandbox, atop a clone of your original working directory. 
