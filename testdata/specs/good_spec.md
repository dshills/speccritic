# User Authentication Service Specification

## Overview

This specification defines the User Authentication Service (UAS), a stateless
HTTP microservice responsible for validating credentials and issuing JWT tokens.

## Scope

The UAS handles only authentication. Authorization (permission checks) is out of scope
and is handled by the caller.

## Functional Requirements

### Login

- The UAS SHALL accept POST /login with a JSON body `{"username": string, "password": string}`.
- On valid credentials, the UAS SHALL return HTTP 200 with body `{"token": string, "expires_at": string}`.
  - `token` is a signed JWT (HS256) with claims: `sub` (user ID), `iat`, `exp`.
  - `expires_at` is an ISO-8601 UTC timestamp 24 hours after issuance.
- On invalid credentials, the UAS SHALL return HTTP 401 with body `{"error": "invalid_credentials"}`.
- On missing or malformed request body, the UAS SHALL return HTTP 400 with body `{"error": "bad_request"}`.

### Token Validation

- The UAS SHALL accept GET /validate with header `Authorization: Bearer <token>`.
- On a valid, unexpired token the UAS SHALL return HTTP 200 with body `{"sub": string, "exp": string}`.
- On an invalid or expired token the UAS SHALL return HTTP 401 with body `{"error": "invalid_token"}`.
- On a missing Authorization header the UAS SHALL return HTTP 400 with body `{"error": "missing_token"}`.

## Non-Functional Requirements

- P99 latency for /login SHALL be ≤ 200 ms under 500 concurrent requests.
- P99 latency for /validate SHALL be ≤ 50 ms under 500 concurrent requests.
- The service SHALL be stateless; no session state is stored server-side.

## Error Handling

- The UAS SHALL never return HTTP 5xx for client errors.
- On an internal error (e.g., signing key unavailable), the UAS SHALL return HTTP 503
  with body `{"error": "service_unavailable"}` and log the error at ERROR level.
- The UAS SHALL return JSON for all responses, including errors.

## Security

- Passwords MUST NOT be logged at any level.
- The JWT signing key is injected via environment variable `JWT_SECRET` (minimum 32 bytes).
- If `JWT_SECRET` is absent at startup, the UAS SHALL exit with code 1 and log
  `"FATAL: JWT_SECRET not set"`.
