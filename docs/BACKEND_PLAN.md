# PitchTZ Go backend plan

## Translation from the design documents

The design diagrams propose Node/Hono. This project keeps their product and security decisions while implementing the API in the same Go stack used by the other Godopetza backends:

- Gin for HTTP routing and middleware
- GORM with PostgreSQL
- explicit `models`, `store`, `server/handlers`, `initializers`, and `utils` packages
- dependency-injected stores so handlers are testable without a live database
- public DTOs separated from persistence models

Redis, a queue worker, provider integrations, and PostGIS remain planned components. They should be introduced when the corresponding vertical slice needs them, rather than as idle infrastructure.

## Non-negotiable rules from the designs

1. Public endpoints never serialize database entities directly.
2. Booking conflicts must be rejected by PostgreSQL inside the booking transaction; a Redis TTL is only an acceleration layer.
3. Money is stored as integer Tanzanian shillings.
4. Every provider charge/refund has a unique idempotency key and signed webhook verification.
5. A booking confirms only after its payable shares are funded.
6. Payout items uniquely reference successful transactions so money cannot be paid out twice.
7. Owner and admin access is scoped; owner queries must always be venue-bound.
8. QR payment sessions are expiring capabilities and reveal only masked payment context.

## Delivery order

### 1. Public catalog and city waitlist — implemented

Cities, sanitized venue browse/detail, pitch availability windows, reviews, extras, and anonymous waitlist registration.

### 2. Authentication and identity

Phone/email OTP, Google and Apple identity verification, short-lived access tokens, rotating refresh sessions, `/v1/me`, and role-aware middleware.

### 3. Booking transaction

Create single and multi-slot bookings, enforce conflict constraints in PostgreSQL, calculate price snapshots, create repeat groups, and expire pending locks. This slice should include concurrency tests.

### 4. Payment ledger

Full payment, installments, split shares, QR sessions, provider adapter, signed/idempotent webhooks, reconciliation, cancellation/refund policy, and booking funding transitions.

### 5. Venue owner operations

Venue applications, schedules, manual bookings, slot blocks, extras inventory, payout settings, and payout statements.

### 6. Community and back office

Teams, challenges, matches, standings, reviews, promotions, disputes, notifications, waitlist launch jobs, and scoped admin operations.

## Production migrations still required

- Enable PostGIS and replace latitude/longitude scan filtering with `geography(Point, 4326)` plus a GiST index.
- Add the final booking exclusion/uniqueness constraint after deciding whether adjacent custom-duration bookings can share a start boundary.
- Add partial/functional unique indexes for normalized phone and email identity values.
- Add payout settlement and transaction-state check constraints.
- Gate schema migration execution separately from ordinary API startup in production.

