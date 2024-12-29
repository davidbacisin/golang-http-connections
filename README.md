# golang-http-connections
This sample application tests various configurations of the built-in `net/http` Go HTTP client
to explore how these settings impact performance. See 
[my blog post](https://davidbacisin.com/writing/golang-http-connection-pools-1) for a discussion of 
the project and its results.

## Requirements
- [Go version 1.23 or later](https://go.dev/dl/)
- Docker and Docker Compose, such as [Docker Desktop](https://www.docker.com/products/docker-desktop/)
- netstat in your `PATH`

## Running the examples
To run the examples, start in the project root.

Create the following directories to mount Docker volumes for the monitoring stack:

```sh
mkdir container/data container/data/grafana container/data/prometheus
```

Then run `docker compose up`. This launches two containers:

1. An **nginx** container to act as the target server. The application will bombard this server 
with requests. The container provisions a self-signed certificate for HTTPS. The root 
endpoint returns a hard-coded response to minimize server latency.
1. An **OTEL LGTM** container that runs Loki, Grafana, Tempo, and Prometheus, with an OpenTelemetry 
collector on ports 4317 and 4318. This allows us to collect and view metrics, logs, and 
traces through a Grafana dashboard at `http://localhost:3000/`.

Wait a few moments until the OTEL LGTM container says something like, "The OpenTelemetry 
collector and the Grafana LGTM stack are up and running."

Back in a console, launch the application with the desired example:

```sh
go run . <example-id>
```

where `<example-id>` is one of `1.1`, `1.2`, `1.3`, `2.1`, `2.2`, or `2.3`. This should print 
something like `Starting Example 2.1: default HTTP/2 client stage 0`
to the console. At the start of each stage, a new message is printed.
Examples `1.x` use HTTP/1.1, while examples `2.x` use HTTP/2.

In a web browser, navigate to `http://localhost:3000/` and select the "Golang HTTP Connections" 
dashboard. Now we can monitor our application as it runs.

## Contributing
Have a question? Start a discussion in the [GitHub Discussions](https://github.com/davidbacisin/golang-http-connections/discussions) tab.

Found an issue? Check if someone else has [filed a similar issue in the GitHub project](https://github.com/davidbacisin/golang-http-connections/issues). If not, file a new one.