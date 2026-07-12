# shop

Aplikacija **jedne prodavnice** (storefront) na ShopHub platformi. Jedna instanca
opslužuje jedan `Shop` CR — Shop operator deploy-uje po jedan ovakav backend za
svaku prodavnicu (2 replike za `standard`, 3 za `high`).

## Struktura

```
shop
├── backend/    # Go REST API za artikle i porudžbine (slika: shop-backend)
└── frontend/   # React storefront (statika koju servira backend)
```

- **backend/** — REST API za `items` i `orders`, admin login (JWT), Prometheus
  metrike, liveness/readiness probe, i payment sweep koji potvrđuje porudžbine
  on-chain. Detalji i endpoint-i u [`backend/README.md`](backend/README.md).
- **frontend/** — storefront kroz koji kupci gledaju artikle i prave porudžbine;
  admin panel za upravljanje artiklima.

## Kako se pokreće

Ne pokreće se ručno u normalnom toku — **Shop operator ga deploy-uje** kad se
napravi `Shop` CR (obično preko ShopHub UI-ja). Bazu (`DATABASE_URL`) i admin
lozinku operator ubaci iz Secret-a prodavnice (`<shop>-app`, `<shop>-admin`).

Za lokalni razvoj backenda vidi [`backend/README.md`](backend/README.md).

## CI

- **test** — Go testovi (Testcontainers diže pravu bazu za testove store sloja).
- **docker-build / docker-publish** — build i push `shop-backend` slike.
- **commit-lint** — Conventional Commits.
