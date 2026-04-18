# RustLens Security Best Practices Report

Date: 2026-04-12

## Executive Summary

The previously identified `/api/app/*` impersonation risk, missing Discord PKCE, non-reproducible frontend image install, and missing backend race-test coverage in CI have been remediated in the current workspace.

The backend now derives the acting user from the validated signed frontend session token for authenticated `/api/app/*` routes instead of trusting a caller-supplied user ID header. The Discord OAuth flow now uses both `state` and PKCE. The frontend Docker image now enforces the lockfile, and backend CI now includes `go test -race`.

## Remediations Completed

### Fix 1

- Rule ID: NEXT-AUTH-001 / GO-HTTP-003
- Status: Resolved
- Location: [apps/backend/internal/http/router.go](B:\.dev\RustLens\RustLens-monorepo\apps\backend\internal\http\router.go), [apps/backend/internal/http/app_handlers.go](B:\.dev\RustLens\RustLens-monorepo\apps\backend\internal\http\app_handlers.go), [apps/backend/internal/auth/app_session.go](B:\.dev\RustLens\RustLens-monorepo\apps\backend\internal\auth\app_session.go), [apps/frontend/lib/rustlens-api.ts](B:\.dev\RustLens\RustLens-monorepo\apps\frontend\lib\rustlens-api.ts)
- Summary: Authenticated `/api/app/*` routes now require the validated signed `rustlens_session` token via a server-only header. The backend parses that token with `APP_SESSION_SECRET` and derives the actor from the verified session claims rather than accepting `X-RustLens-User-Id`.

### Fix 2

- Rule ID: OAuth hardening
- Status: Resolved
- Location: [apps/frontend/app/auth/discord/route.ts](B:\.dev\RustLens\RustLens-monorepo\apps\frontend\app\auth\discord\route.ts), [apps/frontend/app/auth/callback/discord/route.ts](B:\.dev\RustLens\RustLens-monorepo\apps\frontend\app\auth\callback\discord\route.ts)
- Summary: The Discord authorization-code flow now creates a PKCE verifier/challenge pair, stores the verifier in an `HttpOnly` cookie, and submits the verifier during token exchange. The outbound token/profile fetches now also use explicit timeouts.

### Fix 3

- Rule ID: REACT-SUPPLY-001 / NEXT-SUPPLY-001
- Status: Resolved
- Location: [apps/frontend/Dockerfile](B:\.dev\RustLens\RustLens-monorepo\apps\frontend\Dockerfile)
- Summary: The frontend image now installs dependencies with `pnpm install --frozen-lockfile`, restoring reproducible container builds.

### Fix 4

- Rule ID: GO-CONC-001
- Status: Resolved in CI configuration
- Location: [\.github/workflows/ci.yml](B:\.dev\RustLens\RustLens-monorepo\.github\workflows\ci.yml)
- Summary: Backend CI now runs `go test -race ./...` in addition to the standard Go test suite.

## Verification

- `pnpm --filter @rustlens/frontend lint`
  Result: passed with pre-existing `@next/next/no-img-element` warnings only.
- `pnpm --filter @rustlens/frontend build`
  Result: passed.
- `go test ./...` in `apps/backend`
  Result: passed.

## Local Environment Note

- `go test -race ./...` could not be executed successfully in this Windows workspace because CGO is disabled and no C compiler (`gcc`/`clang`) is installed locally. CI now covers that path once the runner has a race-capable Go toolchain.
