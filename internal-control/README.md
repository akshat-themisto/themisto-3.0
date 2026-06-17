# Themisto Internal Control

Themisto Internal Control is the staff-only control plane for organization lifecycle, device trust state, billing posture, and emergency deauthorization. It is intentionally separate from the customer-facing dashboard.

## What is implemented now
- Internal credentials-based auth with signed session cookies stored in Postgres
- Staff roles: owner, admin, operator, viewer
- Customers, contracts, organizations, deployments, devices, and audit actions
- Organization lifecycle actions: suspend, reactivate, terminate, revoke all devices, mark contract ended
- Device lifecycle actions: disable, reactivate, revoke
- Customer billing controls: mark active, overdue, churned
- Internal enforcement endpoints for future gateway/backend integration
- Audit log written alongside state changes
- Seed data for local exploration

## What is modeled for future downstream enforcement
This app intentionally does **not** try to reach onto customer machines. The intended downstream effects are:
- backend enrollment denial for suspended or terminated orgs
- gateway denial for non-active org/device states
- certificate revocation integration for revoked devices
- telemetry ingestion denial for non-active orgs

Today those are modeled by status and exposed through internal API routes, so the rest of Themisto can be wired to them next.

## Stack
- Next.js App Router
- TypeScript
- PostgreSQL via `pg`
- server actions for mutations
- custom internal auth/session layer

## Local setup
1. Copy envs:
   - `copy .env.example .env.local`
2. Point `DATABASE_URL` at a PostgreSQL 15+ database.
3. Install dependencies:
   - `npm install`
4. Run migrations:
   - `npm run db:migrate`
5. Seed sample data:
   - `npm run db:seed`
6. Optionally import the current local Themisto runtime orgs/devices:
   - `npm run db:sync-local`
7. Start dev server:
   - `npm run dev`
8. Open:
   - [http://localhost:4300](http://localhost:4300)

## Environment variables
- `DATABASE_URL` - Postgres connection string
- `DATABASE_SCHEMA` - schema namespace for this app inside Postgres, defaults to `internal_control`
- `SESSION_COOKIE_NAME` - cookie name for internal sessions
- `SESSION_TTL_HOURS` - session lifetime in hours
- `INTERNAL_API_TOKEN` - bearer token for `/api/v1/*` enforcement routes
- `INTERNAL_OWNER_EMAIL` - optional first owner email for seed
- `INTERNAL_OWNER_PASSWORD` - optional first owner password for seed
- `NEXT_PUBLIC_APP_NAME` - UI app name

## First login
- `admin@themisto.local` / `ChangeMe!123`
- `ops@themisto.local` / `OpsTemp!123`

## Internal API integration seam
These routes are ready for gateway/backend consumption:
- `GET /api/v1/enforcement/org-status/:externalOrgId`
- `GET /api/v1/enforcement/device-status/:externalDeviceId`
- `GET /api/v1/audit?limit=100`

Every route expects:
- `Authorization: Bearer <INTERNAL_API_TOKEN>`

## Why this is a separate app
The customer dashboard is scoped to one customer org and should never gain cross-tenant authority by accident. This app is the opposite: it is an internal, high-authority surface for Themisto staff. Keeping it separate gives you a different auth surface, different blast radius, different audit trail, and a cleaner mental model under pressure.

## Recommended next integration work
1. Make backend enrollment read org status from this control plane.
2. Make gateway reject non-active org/device states during trust checks.
3. Connect revoked device actions to certificate revocation updates.
4. Add forced password rotation for seeded staff accounts.

