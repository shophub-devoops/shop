# Shop frontend

Vite + React + TypeScript + Tailwind. Single-page app for one Shop tenant.

## Pages

| Route             | Purpose                                                 |
|-------------------|---------------------------------------------------------|
| `/`               | Storefront — browse items, add to cart, checkout stub.  |
| `/admin/login`    | Stub login (any non-empty credentials). Real JWT in D13.|
| `/admin`          | Admin dashboard — CRUD items, view orders.              |

## Running locally

```bash
npm install
npm run dev          # http://localhost:5173
```

Vite proxies `/api/*` to `http://localhost:8080`, so run the backend in another
terminal:

```bash
cd ../backend
DATABASE_URL=postgres://postgres:dev@localhost:5432/postgres?sslmode=disable \
  PORT=8080 go run ./...
```

## Production

`Dockerfile` builds the static bundle and serves it with `nginx-unprivileged`
on port 8080. `nginx.conf` proxies `/api/*` to the in-cluster backend Service
named `shop-backend`. The operator deploys both behind one Ingress.

## What's intentionally out of scope here

- Web3 checkout flow → D12.
- Real auth (JWT email+password or Web3 SIWE) → D13.
- Unit tests → D7.
