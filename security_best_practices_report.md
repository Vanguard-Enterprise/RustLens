# RustLens Security Best Practices Report

## Executive Summary

RustLens has a reasonable baseline in a few areas: password hashes use `bcrypt`, token material is generated with `crypto/rand`, and most JSON handlers bound request body sizes with `http.MaxBytesReader`. The highest-impact gaps are around edge hardening and auth abuse resistance. The backend HTTP server is missing several production timeout/header limits, the public email-status flow exposes account-existence and password-login state, and the public auth surface has no visible throttling. The browser app also does not set a CSP or other baseline security headers in app code.

## High Severity

### RL-001

- Rule ID: `GO-HTTP-001`
- Severity: High
- Location: `apps/backend/cmd/server/main.go:54-58`
- Evidence:

```go
server := &http.Server{
	Addr:              ":" + cfg.Port,
	Handler:           router,
	ReadHeaderTimeout: 10 * time.Second,
}
```

- Impact: The backend only sets `ReadHeaderTimeout`. Missing `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes` leaves the public listener more exposed to slow-client and oversized-header resource exhaustion.
- Fix: Configure a full production `http.Server` baseline, for example explicit `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and `MaxHeaderBytes`, calibrated for this API’s expected traffic.
- Mitigation: Enforce stricter timeouts and header-size limits in the reverse proxy as defense in depth, but keep app-level limits as well.
- False positive notes: If an upstream proxy already rejects slow/oversized requests, that reduces exposure, but it does not remove the value of app-level limits.

### RL-002

- Rule ID: `NEXT-AUTH-001`
- Severity: High
- Location: `apps/frontend/app/auth/email/status/route.ts:4-19`, `apps/backend/internal/http/app_handlers.go:183-204`
- Evidence:

```ts
const status = await getEmailAuthStatus(email);
return NextResponse.json(status);
```

```go
writeJSON(w, http.StatusOK, map[string]any{
	"email":       status.Email,
	"hasPassword": status.HasPassword,
})
```

- Impact: An unauthenticated caller can submit arbitrary email addresses and learn whether the address exists in RustLens and whether it already has a password login. That materially lowers the cost of account enumeration and credential-stuffing targeting.
- Fix: Stop returning per-email auth state to unauthenticated callers. Collapse the flow to a generic response such as “continue” and decide login vs registration only after a password attempt or a one-time verification step.
- Mitigation: If the UX must stay email-first, rate-limit the endpoint aggressively and return the same payload for existing and non-existing emails.
- False positive notes: This is exploitable from the public frontend route even though the backend itself is hidden behind `APP_API_INTERNAL_SECRET`.

## Medium Severity

### RL-003

- Rule ID: `NEXT-DOS-001`
- Severity: Medium
- Location: `apps/backend/internal/http/router.go:102-111`, `apps/backend/internal/http/app_handlers.go:207-317`
- Evidence:

```go
r.Route("/api/app", func(r chi.Router) {
	r.Use(appInternalMiddleware(cfg.AppAPIInternalSecret))
	r.Post("/auth/register", handler.RegisterPasswordUser)
	r.Post("/auth/login", handler.LoginPasswordUser)
	r.Post("/auth/password/reset/request", handler.RequestPasswordReset)
	r.Post("/auth/password/reset/consume", handler.ConsumePasswordReset)
	r.Post("/auth/email/verify/request", handler.RequestEmailVerification)
	r.Post("/auth/email/verify/consume", handler.ConsumeEmailVerification)
})
```

The corresponding handlers validate request shape and size, but there is no visible route-level throttling or abuse control in router middleware or handler logic.

- Impact: Login, registration, and reset flows are exposed to brute-force, credential-stuffing, and inbox-spam style abuse. Even if the frontend hides some detail, the auth endpoints remain automation-friendly.
- Fix: Add rate limiting for auth endpoints at both the edge and application layer, ideally keyed by IP plus normalized email, with tighter budgets on login and reset flows.
- Mitigation: If immediate app-layer throttling is hard, apply strict reverse-proxy/CDN rate limits first and add alerting on repeated failures.
- False positive notes: There may be rate limits outside the repository (Traefik, CDN, WAF); they are not visible in app code.

### RL-004

- Rule ID: `JS-CSP-001`
- Severity: Medium
- Location: `apps/frontend/next.config.ts:1-5`, `apps/frontend/app/layout.tsx:30-50`
- Evidence:

```ts
const nextConfig: NextConfig = {};
```

The root layout renders the HTML shell without adding a CSP meta tag, and there is no visible headers configuration for CSP, `X-Frame-Options`/`frame-ancestors`, `X-Content-Type-Options`, `Referrer-Policy`, or `Permissions-Policy`.

- Impact: A browser-authenticated app without baseline response headers has a weaker defense-in-depth posture against XSS, clickjacking, content-type confusion, and overbroad browser feature access.
- Fix: Add a baseline security-headers policy, preferably via Next.js `headers()` config or edge infrastructure. At minimum add a CSP, `X-Content-Type-Options: nosniff`, clickjacking protection, and `Referrer-Policy`.
- Mitigation: If headers are managed at the proxy/CDN, verify them at runtime and document that ownership in the repo.
- False positive notes: These headers may already be added at an external edge layer; they are simply not visible in this codebase.

## Residual Risks / Follow-Up Checks

1. `apps/frontend/app/auth/email/verify/request/route.ts:5-30` relies on the session cookie for auth but does not perform explicit CSRF token or Origin checks in app code. `SameSite=Lax` reduces exposure, but this should still be verified against the final deployment model.
2. `apps/backend/internal/http/router.go:145-154` compares `X-RustLens-App-Secret` with a normal string equality check. The risk is limited because this is an internal shared-secret boundary, but constant-time comparison would be a cleaner default.

