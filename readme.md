# minimetrics

A lightweight statsd server with a built in metrics dashboard.

Sometimes you don't need distributed multi-process multi-user redundant metrics, you just need a single app that's quick to use and simple to run in your existing environment.

Instrument and monitor your application during development or live, in real-time. No need to setup a database, user accounts, infrastructure. Everything included and zero-config for the "it just works" feel.

## Use cases
- Quick, temporary metrics
- Performance monitoring during development
- Running in small, constrained environments
- Monitor your existing statsd metrics

minimetrics has a small and simple use case. For multi user high availability long lived data persistence with far more indepth analytical features, you will find projects such as Prometheus, Grafana, ELK stack, and many others better suited.

## Supports
- statsd counters, gauges, timers, sets
- filtering on metric tags
- ephemeral or persistent metric storage

## Usage

1. Start minimetrics via `./minimetrics`
2. Set your application statsd server address to the address minimetrics is listening on
3. Open minimetrics in your browser


~~~
$ ./minimetrics -h
Usage of ./minimetrics:
  -bind string
        The webserver bind address and port (default "0.0.0.0:8070")
  -db string
        The sqlite database path. "memory" for in-memory (default "./metrics.db")
  -statsd string
        The statsd bind address and port (default "0.0.0.0:8125")
~~~
