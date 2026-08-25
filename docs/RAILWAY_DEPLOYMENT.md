# Railway deployment

PitchTZ is deployed from the repository root. Railway detects the root `Dockerfile`, builds the Go binary, and starts it on the injected `PORT`.

## Required services

- API service connected to this GitHub repository
- PostgreSQL service
- Redis service

## API variables

Use Railway reference variables so credentials rotate with the backing service:

```env
DB=${{Postgres.DATABASE_URL}}
REDIS_URL=${{Redis.REDIS_URL}}
GIN_MODE=release
PORT=8080
CORS_ALLOWED_ORIGINS=https://<admin-host>,https://<owner-host>
TRUSTED_PROXIES=
ASSET_BASE_URL=
WAITLIST_RATE_LIMIT_PER_MIN=5
WAITLIST_RATE_LIMIT_BURST=5
ADMIN_LOGIN_RATE_LIMIT_PER_MIN=10
ADMIN_LOGIN_RATE_LIMIT_BURST=10
JWT_SECRET=<at-least-32-random-characters>
JWT_ISSUER=pitchtz-api
JWT_ACCESS_TTL_MINUTES=60
```

`DATABASE_URL` is also supported when `DB` is not set. Do not commit resolved PostgreSQL or Redis credentials.

## Service settings

- Root directory: repository root
- Builder: Dockerfile
- Healthcheck path: `/health`
- Healthcheck timeout: `100`
- Restart policy: `ON_FAILURE`

The production health response should resemble:

```json
{"status":"ok","service":"pitchtz","database":"connected"}
```

`REDIS_URL` is reserved for the booking locks, idempotency, and queue slice. The current public catalog/waitlist slice does not establish a Redis connection yet.

See [`ADMIN_AUTH.md`](ADMIN_AUTH.md) for one-time super-admin bootstrapping. See [`CLIENT_API.md`](CLIENT_API.md) for the mobile contract.

## Later integrations

Venue photos and evidence need a Cloudflare R2 bucket plus an R2 API token. Do not add R2 secrets until the upload slice is implemented; the current API only reads `ASSET_BASE_URL` when constructing public photo URLs.
