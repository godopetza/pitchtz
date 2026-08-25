# PitchTZ client API

This is the handoff contract for the Flutter/mobile and client-web teams. It intentionally excludes admin, owner, payout, provider-webhook, and internal APIs.

- Production base URL: `https://pitchtz-production.up.railway.app/v1`
- Interactive Swagger: `https://pitchtz-production.up.railway.app/docs`
- OpenAPI file: `https://pitchtz-production.up.railway.app/openapi.yaml`
- Money: integer Tanzanian shillings (`*_tzs`)
- Time: RFC 3339 timestamps; availability `date` is interpreted in `Africa/Dar_es_Salaam`
- Authenticated requests: `Authorization: Bearer <player-access-token>`
- Retried writes: preserve the same UUID in `Idempotency-Key`

## Implementation status

The Swagger extension `x-pitchtz-status` is authoritative:

- `live`: deployed and safe to integrate now
- `planned`: agreed request/response contract; backend implementation is still pending

| Area | Live now | Contracted next |
| --- | --- | --- |
| Discovery | cities, venues, venue detail, availability, reviews, extras | favorites |
| Growth | city waitlist | — |
| Player auth | — | OTP, Google/Apple, refresh, logout |
| Profile | — | profile, booking history, push devices |
| Booking | — | lock/create, detail, pay, cancel, dispute |
| Payments | — | full, installment, split, guest QR share |
| Community | — | teams, join, challenge, accept |
| Competition | — | leagues, standings, tournaments |
| Notifications | — | inbox and read state |

## Mobile integration rules

1. Generate a typed Dart client from `openapi.yaml`, but expose only `live` operations in production screens until the matching backend slice is released.
2. Never send an admin token to these routes. Player JWTs use the `pitchtz-player` audience; admin tokens use a separate audience.
3. Treat payment responses as asynchronous. A `202` is processing, not proof of payment; reload the booking until its share/booking state changes.
4. Generate one idempotency UUID when a user starts a booking, payment, cancellation, challenge, or dispute action. Reuse it only when retrying that exact action.
5. Do not infer availability from cached search cards. Refresh `/venues/{venueId}/availability`, then let `POST /bookings` make the final atomic decision.

## Standard envelope

Successful responses use:

```json
{"success":true,"data":{}}
```

Errors use stable codes suitable for application logic:

```json
{"success":false,"error":{"code":"INVALID_INPUT","message":"human-readable detail"}}
```

The full schemas, examples, enums, security requirements, and request bodies live in [`openapi.yaml`](openapi.yaml).
