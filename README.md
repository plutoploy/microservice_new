# Container Agent

Go agent for managing Docker containers via HTTP REST API.

## Build

```bash
go build -o container_agent .
```

## Run

```bash
./container_agent
```

Listens on `:8080`. Requires Docker daemon access.

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/containers` | Create container |
| `GET` | `/containers` | List containers (`?all=true`) |
| `DELETE` | `/containers/{id}` | Teardown (stop + remove) |
| `PUT` | `/containers/{id}` | Replace (stop + remove + create) |
| `POST` | `/containers/{id}/start` | Start container |
| `POST` | `/containers/{id}/stop` | Stop container |
| `POST` | `/containers/{id}/restart` | Restart container |
| `GET` | `/containers/{id}` | Inspect container |
| `POST` | `/containers/{id}/rename` | Rename container |
| `GET` | `/containers/{id}/labels` | Get labels |
| `PUT` | `/containers/{id}/labels` | Update labels |

## Examples

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
