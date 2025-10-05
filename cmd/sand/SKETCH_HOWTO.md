# How to use `sand` to sandbox sketch on your mac

You will need to install `sand` first, if you haven't done so yet.

```sh
% go install github.com/banksean/apple-container/cmd/sand@latest
```

## One-time setup

Get an Anthropic API Key.

On your macOS host machine edit a `.env` file (in your project directory, or your home directory etc) to include the line (no quotes around the value):

```  
ANTHROPIC_API_KEY=sk-ant-...
```

## Run `sand` 
```sh

[macos host shell] % sand new --env-file .env

# ... container starts up ...

[linux container shell] % sketch --unsafe --addr 0.0.0.0:80 --ska-band-addr=""

# ... "You're Absolutely Right" ...
```

You should now have a sketch session running in a sandbox, atop a clone of your original working directory. 

To access the web interface, open `http://<sandbox-id>.test/` (no https!) in your browser.

## More advanced use cases

### Sketch sessions are independent of `sand` sandbox lifecycle

You can kill a sketch session with ctrl-C without destroying the changes it made in the sandbox's filesystem.

You can start another sketch session over the remains left by a previous sketch session in the same sandbox.

This makes it easier to chain a series of short sessions over the same isolated clone of your project directory.

### Custom hostnames

To set the hostname to something specific (instead of the random guid that `sand` will pick for you):
 - Run `sand` with an explicit sandbox name, e.g.: `sand new --env-file .env local-sketch`. In this case the sandbox name is `local-sketch`
 - `sand` creates a container with a dns name of `local-sketch.test.` (per Apple's `container system dns` settings)
 - Therefore the sketch web interface will be visible to you on your macOS host machine at `http://local-sketch.test/`.