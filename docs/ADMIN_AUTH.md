# Admin authentication and staff provisioning

Admin accounts never use a public signup endpoint. Admin JWTs have the `pitchtz-admin` audience and cannot be used as player tokens.

## First super admin

Set these Railway API-service variables for one deployment:

```env
JWT_SECRET=<at-least-32-random-characters>
JWT_ISSUER=pitchtz-api
JWT_ACCESS_TTL_MINUTES=60
BOOTSTRAP_ADMIN_NAME=<your-name>
BOOTSTRAP_ADMIN_EMAIL=<your-email>
BOOTSTRAP_ADMIN_PASSWORD=<strong-unique-password-at-least-12-characters>
```

On startup, the API creates the bootstrap user only when `admin_staff` is empty. It never overwrites an existing password. After the log contains `bootstrap super admin created`, remove `BOOTSTRAP_ADMIN_PASSWORD`, `BOOTSTRAP_ADMIN_EMAIL`, and `BOOTSTRAP_ADMIN_NAME` from Railway and redeploy. Keep `JWT_SECRET` stable and secret.

## Private admin routes

- `POST /v1/admin/auth/login`
- `GET /v1/admin/auth/me`
- `POST /v1/admin/auth/change-password`
- `GET /v1/admin/users` — super admin only
- `POST /v1/admin/users` — super admin only

New admins receive a temporary password and must change it. Five failed logins lock the credential for 15 minutes. The supported roles are:

- `super_admin`
- `operations`
- `finance`
- `trust_safety`
- `support`
- `marketing`
- `analyst`

These routes are intentionally excluded from the client/mobile Swagger document.
