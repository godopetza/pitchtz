# PitchTZ API

Go backend for PitchTZ: pitch discovery, bookings, split payments, venue operations, teams, and payouts in Tanzania.

## Current foundation

The first vertical slice is implemented:

- `GET /health`
- `GET /v1/cities`
- `GET /v1/venues`
- `GET /v1/venues/:id`
- `GET /v1/venues/:id/availability`
- `GET /v1/venues/:id/reviews`
- `GET /v1/venues/:id/extras`
- `POST /v1/waitlist`

Interactive client/mobile documentation is served at `GET /docs`, with the raw OpenAPI 3.1 contract at `GET /openapi.yaml`. The spec contains no admin or owner routes and marks each operation as `live` or `planned`.

Public handlers return dedicated DTOs. They never serialize GORM entities directly, which prevents owner IDs, platform fee rates, payout data, and internal metadata from leaking through public endpoints.

The domain schema also establishes the tables needed for users, venues, bookings, split-payment shares, provider transactions, payouts, teams, matches, leagues, tournaments, promotions, notifications, reviews, and disputes.

## Run locally

1. Copy `.env.example` to `.env` and set `DB` (or `DATABASE_URL`).
2. Create the PostgreSQL database.
3. Run `go run .`.

If `DB` is empty, the server runs with an in-memory store. This is useful for router tests, but persistent application data requires PostgreSQL.

```bash
go test ./...
go run .
```

## Venue search filters

`GET /v1/venues` currently accepts:

- `city_id=<uuid>`
- `format=5|7|11|futsal`
- `lat=<number>&lng=<number>&radius_km=<number>`
- `near=<latitude>,<longitude>&radius=<kilometres>` (design-document alias)

Geo filtering uses latitude/longitude in the initial schema. A PostGIS migration is planned before production traffic so the same API can use a spatial index.

## Availability

`GET /v1/venues/:id/availability` accepts either:

- `date=YYYY-MM-DD`, interpreted in `Africa/Dar_es_Salaam`; or
- `from=<RFC3339>&to=<RFC3339>`.

The public result exposes unavailable time windows, not booking identities or customer data.

## Design source

The original HTML documents are preserved under [`docs/design`](docs/design):

- `system-design.html`
- `api-diagrams-v2.html`

The Go delivery sequence and architecture decisions are in [`docs/BACKEND_PLAN.md`](docs/BACKEND_PLAN.md).

Railway service configuration and reference variables are documented in [`docs/RAILWAY_DEPLOYMENT.md`](docs/RAILWAY_DEPLOYMENT.md).

The mobile handoff is in [`docs/CLIENT_API.md`](docs/CLIENT_API.md), and private staff provisioning is in [`docs/ADMIN_AUTH.md`](docs/ADMIN_AUTH.md).
