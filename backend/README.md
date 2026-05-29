# Shop backend

REST API for items and orders of a single Shop tenant. One backend instance
serves one Shop CR — the Kubernetes operator deploys one of these per Shop.

## Endpoints

| Method | Path                | Purpose                                |
|--------|---------------------|----------------------------------------|
| GET    | `/probe/liveness`   | Always 200. Kubelet liveness probe.    |
| GET    | `/probe/readiness`  | 200 only if Postgres responds to Ping. |
| GET    | `/metrics`          | Prometheus exposition format.          |
| GET    | `/api/items`        | List items.                            |
| POST   | `/api/items`        | Create item.                           |
| PUT    | `/api/items/:id`    | Update item.                           |
| DELETE | `/api/items/:id`    | Delete item.                           |
| GET    | `/api/orders`       | List orders.                           |
| POST   | `/api/orders`      | Create pending order.                  |

## Configuration

| Env var        | Required | Default | Notes                                   |
|----------------|----------|---------|-----------------------------------------|
| `DATABASE_URL` | yes      | —       | Postgres DSN. Operator maps the `uri` key from the CNPG `<shop-name>-app` Secret. |
| `PORT`         | no       | `8080`  | HTTP listen port.                       |

## Running locally

```bash
docker run -d --name shop-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:17
export DATABASE_URL="postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
go run ./...
```

The local Postgres has no `items` / `orders` tables. Apply [`testdata/schema.sql`](testdata/schema.sql)
to mirror what CNPG bootstraps in production.

## Schema

`items` and `orders` are bootstrapped by the Shop operator via CNPG
`postInitApplicationSQL`. See `shop-operator/internal/controller/apps/shop_controller.go`.
