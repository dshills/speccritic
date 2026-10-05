# Link Shortener Service

## Purpose

This document specifies a service that turns a long web address into a short code and redirects visitors of the short code to the long address.

## Non-goals

- Click analytics. The service does not record who followed a link.
- Custom short codes chosen by the client.
- Editing the long address of an existing link.

## Definitions

- **Link**: a pair of one short code and one long address.
- **Short code**: exactly 7 characters, each drawn from the 62 characters `a`-`z`, `A`-`Z` and `0`-`9`.
- **Long address**: an absolute address whose scheme is `http` or `https` and whose length is between 1 and 2,048 bytes.
- **Client**: a program that calls the endpoints in "Endpoints" with a client key.
- **MUST** and **MUST NOT** mark binding requirements, with the meaning given in RFC 2119.

## Authentication

- **`ls-auth-1`**: Every request to `POST /links` and `DELETE /links/{code}` MUST carry a client key in the request header `X-Client-Key`.
- **`ls-auth-2`**: A client key is a 40-character string issued out of band. The service MUST compare it against the stored keys and, when no stored key matches, respond with status 401 and the error code `unauthorized`.
- **`ls-auth-3`**: `GET /{code}` MUST NOT require a client key.

## Endpoints

### POST /links

Creates a link.

Request body, encoded as JSON (JavaScript Object Notation):

| Field | Type | Required | Meaning |
|---|---|---|---|
| `address` | string | yes | The long address |

- **`ls-create-1`**: If `address` is missing or is not a long address as defined above, the service MUST respond with status 400 and the error code `invalid_address` and MUST NOT create a link.
- **`ls-create-2`**: Otherwise the service MUST create a link with a short code that no other link has ever used, and respond with status 201 and the body `{"code": "<short code>", "address": "<long address>"}`.
- **`ls-create-3`**: Two requests with the same `address` MUST produce two different links.

### GET /{code}

Follows a link.

- **`ls-follow-1`**: If a link with the short code `code` exists, the service MUST respond with status 302 and the header `Location` set to the long address of that link.
- **`ls-follow-2`**: If no such link exists, or the link was deleted, the service MUST respond with status 404 and the error code `not_found`.

### DELETE /links/{code}

Deletes a link.

- **`ls-delete-1`**: If the link exists and was created with the same client key as the request, the service MUST delete it and respond with status 204 and an empty body.
- **`ls-delete-2`**: If the link exists and was created with a different client key, the service MUST respond with status 403 and the error code `forbidden` and MUST NOT delete it.
- **`ls-delete-3`**: If the link does not exist or was already deleted, the service MUST respond with status 404 and the error code `not_found`.

## Error responses

Every response with a status of 400 or above MUST have the body `{"error": "<error code>"}`, where the error code is one of `invalid_address`, `unauthorized`, `forbidden`, `not_found`, `rate_limited` and `unavailable`.

## Rate limits

- **`ls-rate-1`**: The service MUST accept at most 60 `POST /links` requests per client key in any window of 60 consecutive seconds. A request over that limit MUST receive status 429, the error code `rate_limited` and the header `Retry-After` holding the number of whole seconds until the next request will be accepted, for example `12` for 12 seconds.
- **`ls-rate-2`**: `GET /{code}` is not rate limited.

## Requirements

- **`ls-inv-1`**: A short code, once assigned to a link, MUST never be assigned to another link, including after the first link is deleted.
- **`ls-store-1`**: If the link store is unreachable, every endpoint MUST respond with status 503 and the error code `unavailable`.
- **`ls-perf-1`**: `GET /{code}` MUST respond within 50 milliseconds at the 99th percentile, measured at the service over 100,000 requests for existing links while 10,000,000 links are stored.

## Acceptance criteria

- A test creates a link, follows it and observes status 302 with the original long address in `Location`.
- A test deletes a link and then observes status 404 when following it.
- A test creates a link after deleting another and verifies that the new short code differs from every short code issued earlier in the test.
- A test sends 61 `POST /links` requests in one second with one client key and observes status 429 on the 61st.
- A load test meets `ls-perf-1`.
