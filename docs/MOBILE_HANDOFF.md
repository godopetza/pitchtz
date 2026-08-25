# PitchTZ API — Mobile Handoff

Everything a mobile dev needs to start building the PitchTZ player app against the real backend.

## Base URLs

| Environment | URL |
|---|---|
| Production | `https://pitchtz-production.up.railway.app` |
| Swagger UI | `https://pitchtz-production.up.railway.app/docs` |
| Raw OpenAPI spec | `https://pitchtz-production.up.railway.app/openapi.yaml` |
| Local dev | `http://localhost:8080` |

All API routes are under `/v1` except `/health`, `/docs`, and `/openapi.yaml`.

**The spec tags every operation `x-pitchtz-status: live` or `planned`.** Trust that tag over the shape of the schema — a `planned` operation is fully designed but returns a 404 today. Build against `live` only; treat `planned` as the roadmap.

## Response envelope

```json
// success
{ "success": true, "data": { ... }, "message": "" }

// error
{ "success": false, "error": { "code": "SOME_CODE", "message": "human-readable" } }
```

## What's live today (7 endpoints)

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/cities` | All cities with `status: "live"` or `"waitlist"` |
| GET | `/v1/venues` | Query: `city_id`, `format` (`5`,`7`,`11`,`futsal`), `near=lat,lng` + `radius_km` (default 10, max 100) |
| GET | `/v1/venues/:id` | Full venue incl. `pitches[]`, `photos[]`, `extras[]` |
| GET | `/v1/venues/:id/availability` | Query: `date=YYYY-MM-DD` (Africa/Dar_es_Salaam) or `from`/`to` (RFC3339, max 31-day window). Returns per-pitch unavailable windows — **not** a slot-picker list; compute free slots client-side from the gaps. |
| GET | `/v1/venues/:id/reviews` | — |
| GET | `/v1/venues/:id/extras` | Rentable add-ons (bibs, balls, jerseys, etc.) |
| POST | `/v1/waitlist` | Body: `{ city_id, email? or phone? }` — join the launch waitlist for a city not live yet. Rate-limited 5/min/IP. |

## What's `planned`, not implemented (don't build against these yet)

- **Player auth**: `/auth/otp/send`, `/auth/otp/verify`, `/auth/oauth/{provider}`, `/auth/refresh`, `/auth/logout` — there is currently **no player/customer authentication at all** in the backend. Only internal staff and venue-owner auth exist (not usable by the player app).
- **Player profile**: `/me`, `/me/bookings`, `/me/favorites`, `/me/devices`
- **Booking lifecycle**: `POST /bookings`, `/bookings/{id}`, `/bookings/{id}/pay`, `/bookings/{id}/cancel` — a player cannot create a booking through the API yet at all.
- **Payments**: `/pay/qr/{session}`
- **Social**: `/teams`, `/leagues`, `/tournaments`, `/matches/{id}/accept`, `/notifications`, `/disputes`

Heads-up: `pitchtz-client` (the web app) is in the exact same position — its sign-in modal and "Continue to booking" flow are UI-only mocks today, not wired to a real backend, for this same reason. Web and mobile can both ship real browsing/discovery now; neither can ship real sign-in-and-book until the player-auth + booking-creation work lands on the backend.

## Data shapes (from the real Postgres models — field names are the actual JSON keys)

```ts
City = { id, name, status: "live" | "waitlist", launchEta?, latitude, longitude }

Venue = {
  id, ownerId, cityId, name, area, latitude, longitude,
  status: "pending" | "review" | "active" | "suspended",
  feeRateBps, verified, rating,
  amenities: string[], rules: string[],
  peakMultiplierBps, cancelWindowHours, autoConfirm,
  city: City, pitches: Pitch[], photos: VenuePhoto[], extras: ExtraCatalog[]
}

Pitch = { id, venueId, name, format, surface, basePriceTzs, openHours }

Booking (not player-creatable yet; shape for reference) = {
  id, code, pitchId, userId?, teamId?, startsAt, endsAt, source,
  status: "pending" | "confirmed" | "part_paid" | "completed" | "cancelled" | "disputed",
  repeatRule, pitchFeeTzs, serviceFeeTzs, totalTzs, checkedInAt?, cancelledAt?
}
```

**Double-booking is enforced at the database level** (a Postgres exclusion constraint on `pitch_id` + time range, not just app logic), so once booking-creation ships for players, it inherits that same guarantee for free — a 409 comes back on any overlap, atomically, even under concurrent requests. Time ranges are half-open (`[start, end)`), so back-to-back bookings on the same pitch are allowed.

## Rate limits (per IP)

- Waitlist: 5/min
- Admin login: 10/min
- Owner login: 10/min
- Venue enrollment: 5/min

## Brand / design tokens (for matching `pitchtz-client` visually)

- **Font**: Plus Jakarta Sans (Google Font, weights 400–800)
- **Palette**: `#0E3B2C` deep green (primary/brand) · `#C9F24E` lime (accent/CTA) · `#F5F4EF` cream (background) · `#171B18` near-black (text) · `#6E756F` muted grey (secondary text) · `#E7E5DD` border · `#EAF2E4` / `#2E7D46` success · `#DFA53C` gold (ratings)
- **Corner radius scale**: 8–14px controls, 15–22px cards, 99px (full) pills/chips
- **Shadows**: soft, low-opacity, large-blur — not flat, not neumorphic (e.g. `0 2px 10px rgba(23,27,24,.06)` resting, `0 14px 30px rgba(23,27,24,.12)` elevated)
- **Logo**: real asset, not text-drawn. Full lockup + a separately-cropped icon-only mark (the baked-in wordmark font doesn't match Plus Jakarta Sans, so at small sizes pair the icon mark with "Pitch TZ" set in Plus Jakarta Sans, same as the web header does). Tagline: **"Find. Book. Play."**
- **Icons**: feature/marketing icons use Microsoft's Fluent Emoji (3D style — free, MIT-licensed, playful/glossy); small functional UI chrome (chevrons, search, close) uses a plain line-icon set (Lucide on web). For Flutter, the same Fluent Emoji 3D PNGs can be reused directly under their license; pair with a Lucide-equivalent Flutter icon pack for chrome.

## Known gaps worth planning around

- Production has **zero seeded venues/cities** right now beyond what comes through the real venue-enrollment → admin-approval flow — don't expect sample content in prod without checking first.
- No image/asset upload endpoint yet (`ASSET_BASE_URL` is read-only for constructing photo URLs today).
- No push-notification or device-registration endpoint yet (`/me/devices` is `planned`).
