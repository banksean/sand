# How to use `sand` to sandbox claude code on your mac

You will need to install `sand` first, if you haven't done so yet.

```sh
% brew install banksean/tap/sand
```

## One-time (or once a year, rather) setup

On your macOS host machine, run:

```sh
% claude setup-token
```

Follow the directions to do the browser copy-and-paste dance, and then save that `CLAUDE_CODE_OAUTH_TOKEN=<token>` a `.env` file (`sand` uses `./.env` as the default, but you can specify another location with `--env-file`).

```sh
[macos host shell] % sand new -a claude
```

You should now have a claude code session running in a sandbox, atop a clone of your original working directory. 
