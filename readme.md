# minimetrics

A lightweight statsd server with a built in metrics dashboard.

![image](https://user-images.githubusercontent.com/725880/125639488-037f24ea-f1d2-4a35-a5f4-dd967e04f529.png)

Sometimes you don't need distributed multi-process multi-user redundant metrics, you just need a single app that's quick to use and simple to run within your existing environment.

Instrument and monitor your application during development or live, in real-time. No need to setup a database, user accounts, infrastructure. Everything included and zero-config for the "it just works" feel. Run the single binary and you're away.

## Use cases
- Quick, temporary metrics
- Performance monitoring during development
- Running in small, constrained environments
- Monitor your existing statsd metrics
- Make sure your apps statsd metrics are working

minimetrics intends to be small and quick to get running. For more advanced features such as multi user high availability long lived data persistence with far more indepth analytical features, you will find projects such as Prometheus, Grafana, ELK stack, and many others better suited.

## Features
- statsd counters, gauges, timers, sets
- Filtering on metric tags
- Ephemeral or persistent metric storage
- Dark theme
- Dashbaord mode

## Usage

1. Start minimetrics via `./minimetrics`
2. Set your application statsd server address to the address minimetrics is listening on
3. Open minimetrics in your browser


~~~shell
$ ./minimetrics
2021/08/01 22:51:09 Serving public/ for public HTTP
2021/08/01 22:51:09 Web server listening 0.0.0.0:8070
2021/08/01 22:51:09 Statsd server listening [::]:8125
2021/08/01 22:51:10 Receiving statsd data confirmed
~~~

~~~shell
$ ./minimetrics -h
Usage of ./minimetrics:
  -bind string
        The webserver bind address and port (default "0.0.0.0:8070")
  -db string
        The sqlite database path. "memory" for in-memory (default "./metrics.db")
  -statsd string
        The statsd bind address and port (default "0.0.0.0:8125")
~~~
