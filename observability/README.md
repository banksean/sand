
# If you are not working on the `sand` code itself, ignore all of this

- These features exist only to help debug problems with `sand` itself. 
- `sand` will work fine without running any of this additional infrastructure.

It may seem odd for `sand` to have an "observability" package since `sand` is a local-only sandbox system that doesn't implement or require any remote/cloud services. However, many tools and libraries designed for cloud service reliability use cases also make debugging easier when you're working with a bunch of local containers that need to communicate with each other and have their own distinct lifecycles.


## Collect and View Telemetry Locally

The `sand` and `sandd` executables include OpenTelemetry instrumentation to generate and export traces for some common operations (at time of this writing, it's limited only to gRPC client/server stub call sites).

[`../Taskfile.yaml`](../Taskfile.yaml) includes helper tasks to start a local OpenTelemetry Collector, Tempo, Loki, and Grafana stack. The collector receives OTLP data, forwards traces to Tempo, and forwards logs to Loki.

```sh
export CONTAINER_DNS_DOMAIN=$(container system property get dns.domain)

# start grafana, tempo, loki, and the OpenTelemetry Collector in a single invocation

task start-observability
export OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector.$CONTAINER_DNS_DOMAIN:4317
export OTEL_EXPORTER_OTLP_INSECURE=true

# If sandd is already running, stop it so it picks up the above env vars
# the next time it starts
sandd stop

# automatically starts sandd, and generates a trace when it makes 
# the sand.daemon.v1.DaemonService/ListSandboxes gRPC call:
sand ls 
```

Then navigate to http://grafana.dev.local:3000/a/grafana-exploretraces-app/explore (you can substitute whatever `container system property get dns.domain` returns for `dev.local` in that URL, if it isn't `dev.local`) to verify that the `sand ls` invocation generated a trace. Use Grafana Explore with the Loki datasource to inspect collected logs.

## Configuration

The observability configuration should Just Work out of the box, but if you need to modify it the files are:

- [tempo.yaml](./tempo.yaml) for the trace backend
- [loki.yaml](./loki.yaml) for the log backend
- [otel-collector.yaml](./otel-collector.yaml) for OTLP receiving and signal routing

These files get bind-mounted as read-only volumes in their respective containers.

The `start-observability` task in [`../Taskfile.yaml`](../Taskfile.yaml) programmatically configures Grafana to use the Tempo and Loki data sources by curl'ing json blobs to it after it starts up.

### Persistent observability data

Tempo, Loki, and Grafana mount host filesystem directories as read-write volumes, so that trace data, log data, and dashboard settings can persist across container restarts.

These are the `hostdir:containerdir` paths that [`../Taskfile.yaml`](../Taskfile.yaml) passes to the containers.

- `tempo`: `{{.OBSERVABILITY_DATA_DIR}}/tempo:/var/tempo`
- `loki`: `{{.OBSERVABILITY_DATA_DIR}}/loki:/loki`
- `grafana`: `{{.OBSERVABILITY_DATA_DIR}}/grafana:/var/lib/grafana`

Note that `OBSERVABILITY_DATA_DIR` is set to `.observability-data` by default (and ignored by git). If you prefer to use an XDG-style base path, you can override it like so:

```sh
task start-observability \ 
  OBSERVABILITY_DATA_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/sand/observability"
```

## Stopping the observability stack

```sh
task stop-observability
```

This will stop and remove the Grafana, Tempo, Loki, and OpenTelemetry Collector containers, but will leave their respective persistent volumes' host directories untouched. So if you run `task start-observability` after `task stop-observability`, you'll still see your previously collected trace and log data.

## Clearing persistent observability data

```sh
task clear-observability
```

This will remove the persistent volumes' host directories, so it clears out all trace data etc that has been collected so far.
