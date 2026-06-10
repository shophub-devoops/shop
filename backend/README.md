# Shop backend

REST API for items and orders of a single Shop tenant. One backend instance
serves one Shop CR — the Kubernetes operator deploys one of these per Shop.

## Endpoints

| Method | Path                | Purpose                                |
|--------|---------------------|----------------------------------------|
| GET    | `/probe/liveness`   | Always 200. Kubelet liveness probe.    |
| GET    | `/probe/readiness`  | 200 only if the database responds to Ping. |
| GET    | `/metrics`          | Prometheus exposition format.          |
| POST   | `/api/auth/login`   | Admin sign-in: `{password}` → `{token}`. |
| GET    | `/api/items`        | List items (public).                   |
| POST   | `/api/items`        | Create item (**admin**).               |
| PUT    | `/api/items/:id`    | Update item (**admin**).               |
| DELETE | `/api/items/:id`    | Delete item (**admin**).               |
| GET    | `/api/orders`       | List orders (**admin**, spec 2.2).     |
| POST   | `/api/orders`       | Create pending order (reserves stock). |
| GET    | `/api/orders/:id`   | Poll one order's payment status.       |

Admin endpoints take `Authorization: Bearer <token>` from `/api/auth/login`.
Orders reserve stock at creation; the payment sweep confirms them on-chain,
fails them (restoring stock), or expires abandoned ones after 30 minutes.

## Configuration

| Env var          | Required | Default | Notes                                   |
|------------------|----------|---------|-----------------------------------------|
| `DATABASE_URL`   | yes      | —       | Postgres or MongoDB DSN. Operator maps it from the `<shop-name>-app` Secret. |
| `SHOP_DB_NAME`   | mongo    | —       | Database name for the Mongo path (operator sets the Shop name). |
| `PORT`           | no       | `8080`  | HTTP listen port.                       |
| `ADMIN_PASSWORD` | no       | —       | Guards admin endpoints. Operator injects it from the `<shop>-admin` Secret; unset = auth disabled (local dev). |
| `WALLET_ADDRESS` | no       | —       | On-chain payment recipient; unset disables on-chain verification (sweep runs expiry-only). |

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
