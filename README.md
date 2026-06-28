# Container Agent

Go agent for managing Docker containers via HTTP REST API.

> **Full documentation:** see [`MANUAL.md`](MANUAL.md) for configuration,
> endpoint resolution, the complete API reference, image-update details,
> architecture, security notes, and troubleshooting.

## Build

```bash
go build -o container_agent .
```

## Run

```bash
./container_agent
```

Listens on `:8080`. Requires Docker daemon access.

**Create container:**
```bash
curl -X POST http://localhost:8080/containers \
  -H "Content-Type: application/json" \
  -d '{"name":"my-app","image":"nginx:latest","labels":{"env":"prod"}}'
```

**List all containers:**
```bash
curl http://localhost:8080/containers?all=true
```

**Stop and remove:**
```bash
curl -X DELETE http://localhost:8080/containers/my-app
```

**Replace container:**
```bash
curl -X PUT http://localhost:8080/containers/my-app \
  -H "Content-Type: application/json" \
  -d '{"image":"nginx:1.25","labels":{"env":"prod"}}'
```

**Check for image updates (no restart):**
```bash
curl -X POST http://localhost:8080/containers/check \
  -H "Content-Type: application/json" \
  -d '{"names":["my-app"]}'
```

**Update stale containers (pull newer image + recreate):**
```bash
# All containers; remove superseded images afterwards
curl -X POST http://localhost:8080/containers/update \
  -H "Content-Type: application/json" \
  -d '{"cleanup":true}'
```

Update/check request fields (all optional): `names`, `disable_names`,
`enable_label`, `scope`, `cleanup`, `no_restart`, `no_pull`,
`timeout_seconds`, `monitor_only`. An empty body targets all containers.

Image-update checks are powered by
[`github.com/dockerutil/watchtower`](https://github.com/dockerutil/watchtower).
