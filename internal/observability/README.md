# Sand Observability

It may seem odd for `sand` to have an "observability" package since `sand` is a local-only sandbox system that doesn't implement or require any remote/cloud services. However, many tools and libraries designed for cloud service reliability use cases also make debugging easier when you're working with a bunch of local containers that need to communicate with each other and have their own distinct lifecycles.

## If you are not debugging `sand` itself, you can safely ignore all of this

- These features exist only to help debug problems with `sand` itself. 
- `sand` will work fine without running any of this additional infrastructure.

## How to Collect and View Traces

The `sand` and `sandd` executables include OpenTelemetry instrumentation to generate and export traces for some common operations (at time of this writing, it's limited only to gRPC client/server stub call sites).

`Taskfile.yaml` includes some helper tasks to start a local temp container (for collecting traces) and a local grafana instance (for viewing the collected traces in its web UI)

```sh
export CONTAINER_DNS_DOMAIN=$(container system property get dns.domain)

# start the grafana and tempo containers in a single invocation

task start-observability
export OTEL_EXPORTER_OTLP_ENDPOINT=tempo.$CONTAINER_DNS_DOMAIN:4317
export OTEL_EXPORTER_OTLP_INSECURE=true

# If sandd is already running, stop it so it picks up the above env vars
sandd stop

# automatically starts sandd:
sand ls 
```

Then navigate to http://grafana.dev.local:3000/a/grafana-exploretraces-app/explore (you can substitute whatever `container system property get dns.domain` returns for `dev.local` in that URL, if it isn't `dev.local`) to verify that the `sand ls` invocation generated a trace.

## Configuration

The tempo configuration should Just Work out of the box, but if you need to modify it the file is [../../observability/tempo.yaml](../../observability/tempo.yaml)

This file gets bind-mounted as a read-only volume in the tempo container.

The `grafana` task in [`../../Taskfile.yaml`](../../Taskfile.yaml) programmatically configures grafana to use this tempo data source by curl'ing a json blob to it after it starts up.

### Persistent observability data

Both tempo and grafana mount host filesystem directories as read-write volumes, so that trace data (tempo) and dashboard settings (grafana) can persist across container restarts.

These are the `hostdir:containerdir` paths that `../../Taskfile.yaml` passes to the containers.

- `tempo`: `{{.OBSERVABILITY_DATA_DIR}}/tempo:/var/tempo`
- `grafana`: `{{.OBSERVABILITY_DATA_DIR}}/grafana:/var/lib/grafana`

Note that `OBSERVABILITY_DATA_DIR` is set to `.observability-data` by default (and ignored by git). If you prefer to use an XDG-style base path, you can override it like so:

```sh
task start-observability OBSERVABILITY_DATA_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/sand/observability"
```

## Stopping grafana and tempo

```sh
task stop-observability
```

This will stop and remove the grafana and tempo containers, but will leave their respective persistent volumes' host directories untouched.  So if you run `task start-observability` after `task stop-observability`, you'll still see your previously collected trace data.

## Clearing persistent observability data

```sh
task clear-observability
```

This will remove the persistent volumes' host directories, so it clears out all trace data etc that has been collected so far.
