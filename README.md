# HTTP Sink Tap

A small HTTP sink with two ports:

- `8080`: accepts any HTTP method on any path, captures request details, and returns a success response.
- `8081`: exposes a browser viewer at `/`, a WebSocket stream at `/ws`, health at `/healthz`, and recent captured requests at `/requests`.

Captured request events include method, path, query, host, protocol, remote address, headers, content length, transfer encoding, timestamp, and body. UTF-8 bodies are emitted as text. Binary bodies are emitted as base64. Bodies are truncated after `BODY_LIMIT_BYTES`.

## Run Locally

```sh
go run .
```

Open the listener UI:

```sh
open http://localhost:8081
```

Send requests to the sink:

```sh
curl -i -X POST http://localhost:8080/anything/here \
  -H 'X-Test: demo' \
  -d '{"hello":"world"}'
```

Connect directly to the WebSocket stream:

```sh
websocat ws://localhost:8081/ws
```

## Kubernetes

Build and push an image that your cluster can pull:

```sh
DOCKER_REGISTRY=ghcr.io/example make docker-build docker-push
```

Apply the manifests with the image from your registry:

```sh
DOCKER_REGISTRY=ghcr.io/example make deploy
```

Port-forward both ports:

```sh
kubectl -n http-sink-tap port-forward svc/http-sink-tap 8080:8080 8081:8081
```

Then open `http://localhost:8081` and send traffic to `http://localhost:8080`.

Inside the cluster, send sink traffic to `http://http-sink-tap.http-sink-tap.svc.cluster.local:8080`.

## Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `SINK_ADDR` | `:8080` | Address for the catch-all sink server. |
| `LISTENER_ADDR` | `:8081` | Address for the viewer, WebSocket, and health server. |
| `BODY_LIMIT_BYTES` | `1048576` | Maximum request body bytes captured per request. |
| `HISTORY_LIMIT` | `200` | Number of recent requests replayed to new listeners and returned from `/requests`. |
| `RESPONSE_STATUS` | `200` | HTTP status returned by the sink. |
| `RESPONSE_BODY` | `ok\n` | HTTP response body returned by the sink. |

Do not expose this service publicly unless you intend to collect arbitrary request headers and bodies, including credentials.

## License

Apache License 2.0. See [LICENSE](LICENSE).
