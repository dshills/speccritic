# Account Data Export Service

## Purpose

This document specifies a service that lets an account owner request a copy of the data held for the account, produces that copy as a downloadable archive, and removes the archive after a fixed time.

## Goals

- An account owner obtains a complete archive of the account's data without help from support staff.
- An archive is available only to the owner of the account it was made for.
- An archive cannot be downloaded after its expiry, and is deleted within 1 hour of it unless deletion fails.

## Non-goals

- Importing an archive into another account.
- Exporting data for more than one account in one request.
- Partial exports limited to a date range or a subset of data categories.

## Definitions

- **Session token**: a token issued and validated by the platform's identity service, which this document does not specify. A session token is valid when the identity service accepts it. A valid session token gives one user id and, for each account, the roles that user holds there.
- **Account owner**: the user whose session token carries the role `owner` for an account.
- **Export request**: one request by an account owner to export one account. It is identified by a request id.
- **Archive**: one file in ZIP format that holds the exported data of one export request.
- **Data category**: one of `profile`, `messages`, `files` and `billing`.
- **Record**: one entry stored in the account database under a data category. A record belongs to the account whose account id it carries. The fields of each kind of record are defined by the account database schema, which this document does not specify; an archive reproduces them as stored.
- **Expiry**: the moment 7 days after an export request reaches the state `ready`. An archive expires at its expiry.
- **Download link**: an address that returns the archive of one export request.
- **Checksum**: the SHA-256 digest of the bytes of a file.
- **MUST** and **MUST NOT** mark binding requirements, with the meaning given in RFC 2119.

## Request lifecycle

An export request is always in exactly one of these states:

| State | Meaning |
|---|---|
| `queued` | Accepted and waiting for a worker |
| `running` | A worker is building the archive |
| `ready` | The archive exists and is available for download |
| `failed` | The archive could not be built |
| `expired` | The archive was deleted after its expiry |

- **`ex-state-1`**: The only allowed transitions are `queued` to `running`, `running` to `ready`, `running` to `failed`, `running` to `queued`, and `ready` to `expired`.
- **`ex-state-2`**: `failed` and `expired` are final. A request in a final state MUST never change state again.
- **`ex-state-3`**: Every transition MUST be recorded with the request id, the old state, the new state and the time of the transition.

## Authentication and authorization

- **`ex-auth-1`**: Every endpoint in "Endpoints" MUST require a session token in the header `Authorization`. A request without a valid session token MUST receive status 401 and the error code `unauthenticated`.
- **`ex-auth-2`**: A session token that does not carry the role `owner` for the account named in the request MUST receive status 403 and the error code `forbidden`.
- **`ex-auth-3`**: An account owner MUST be able to see and download only the export requests of the owner's own account.

## Endpoints

### POST /accounts/{account_id}/exports

Creates an export request.

- **`ex-create-1`**: If the account has an export request in the state `queued` or `running`, the service MUST NOT create another one and MUST respond with status 409, the error code `export_in_progress` and the request id of the existing request.
- **`ex-create-2`**: Otherwise the service MUST create an export request in the state `queued` and respond with status 202 and the body `{"request_id": "<request id>", "state": "queued"}`.
- **`ex-create-3`**: The check in `ex-create-1` and the creation in `ex-create-2` MUST be atomic per account: of two simultaneous requests for one account, exactly one creates an export request.
- **`ex-create-4`**: An account MUST be limited to 3 created export requests in any period of 24 consecutive hours. A request over that limit MUST receive status 429 and the error code `export_limit_reached`.

### GET /accounts/{account_id}/exports/{request_id}

Returns the state of an export request.

- **`ex-read-1`**: The service MUST respond with status 200 and the body `{"request_id": "<request id>", "state": "<state>", "expires_at": "<time or null>", "failure_reason": "<reason or null>"}`.
- **`ex-read-2`**: `expires_at` MUST be the expiry when the state is `ready`, and `null` in every other state. `failure_reason` MUST be the failure reason when the state is `failed`, and `null` in every other state.
- **`ex-read-3`**: If no export request with that request id exists for the account, the service MUST respond with status 404 and the error code `not_found`.

### GET /accounts/{account_id}/exports/{request_id}/download

Returns the archive.

- **`ex-download-1`**: If the export request is in the state `ready` and its expiry has not passed, the service MUST respond with status 200, the header `Content-Type: application/zip` and the archive as the body.
- **`ex-download-2`**: In the states `queued`, `running` and `failed` the service MUST respond with status 409 and the error code `not_ready`. In the state `expired`, and in the state `ready` once the expiry has passed, it MUST respond with status 410 and the error code `expired`.
- **`ex-download-3`**: Each successful download MUST be recorded with the request id, the user id from the session token and the time.

## Error responses

Every response with a status of 400 or above MUST have a body with the field `error` holding the error code, as in `{"error": "<error code>"}`. A response with the error code `export_in_progress` MUST also have the field `request_id` holding the request id of the existing request. The error codes are `unauthenticated`, `forbidden`, `not_found`, `export_in_progress`, `export_limit_reached`, `not_ready`, `expired` and `unavailable`.

## Archive requirements

- **`ex-arch-1`**: The archive MUST contain one directory per data category, named after the category.
- **`ex-arch-2`**: Each directory MUST contain every record of that category that belonged to the account at the moment the request entered the state `running`. Records created later are not part of the archive.
- **`ex-arch-3`**: Records of the categories `profile`, `messages` and `billing` MUST be written as one JSON (JavaScript Object Notation) document per record, holding every field of the record under its stored field name with its stored value. Records of the category `files` MUST be written as the original bytes under the original file name.
- **`ex-arch-4`**: The archive MUST contain a file `manifest.json` at the top level that lists, for every other file in the archive, its path, its size in bytes and its checksum.
- **`ex-arch-5`**: An archive MUST NOT exceed 50 gibibytes. If the exported data would exceed that size, the request MUST move to the state `failed` with the failure reason `too_large`.

## Building the archive

- **`ex-build-1`**: A worker MUST move a request from `queued` to `running` before it reads any account data.
- **`ex-build-2`**: A worker MUST finish building an archive within 6 hours of entering the state `running`. A request that is still `running` after 6 hours MUST move to the state `failed` with the failure reason `deadline_exceeded`.
- **`ex-build-3`**: If a worker stops before the archive is complete, the request MUST move from `running` back to `queued`, and any partial archive MUST be deleted. A request MUST return to `queued` at most 2 times; the third time it MUST move to `failed` with the failure reason `worker_lost`.
- **`ex-build-4`**: If writing the archive to the archive store fails, the worker MUST retry the write 3 times, waiting 10 seconds between tries, and then move the request to `failed` with the failure reason `storage_error`.
- **`ex-build-5`**: A request MUST move to `ready` only after the archive and `manifest.json` are completely written and the checksums in the manifest have been verified against the stored files.
- **`ex-build-6`**: If reading account data or reading a stored file back for verification fails, the worker MUST retry the read 3 times, waiting 10 seconds between tries, and then move the request to `failed` with the failure reason `read_error`.
- **`ex-build-7`**: If a checksum computed from a stored file differs from the checksum in `manifest.json`, the worker MUST move the request to `failed` with the failure reason `checksum_mismatch` without retrying.

## Notifications

- **`ex-notify-1`**: When a request moves to `ready` or `failed`, the service MUST send one email to the address of the account owner. The email for `ready` MUST contain the request id and the expiry. The email for `failed` MUST contain the request id and the failure reason.
- **`ex-notify-2`**: The email MUST NOT contain the archive or a download link. The account owner downloads the archive after signing in.
- **`ex-notify-3`**: If sending the email fails, the service MUST retry once per hour for 24 hours and then stop. A failure to send the email MUST NOT change the state of the request.

## Expiry and deletion

- **`ex-expiry-1`**: At the expiry the service MUST start deleting the archive from the archive store. Once the archive is deleted, the request MUST move to the state `expired`.
- **`ex-expiry-2`**: Unless deleting the archive fails, deletion MUST complete within 1 hour of the expiry.
- **`ex-expiry-3`**: If deleting the archive fails, the service MUST retry every 10 minutes until it succeeds and MUST raise the alert `export_deletion_overdue` once deletion is more than 1 hour late. The request stays in the state `ready` until the archive is gone, but downloads MUST be refused with status 410 from the expiry onward.

## Performance and outages

- **`ex-perf-1`**: `POST /accounts/{account_id}/exports` and `GET /accounts/{account_id}/exports/{request_id}` MUST respond within 300 milliseconds at the 99th percentile, measured at the service over 10,000 requests.
- **`ex-perf-2`**: The download endpoint MUST send the first byte of the archive within 2 seconds at the 99th percentile.
- **`ex-outage-1`**: If the request database is unreachable, every endpoint MUST respond with status 503 and the error code `unavailable`.
- **`ex-outage-2`**: If the archive store is unreachable, the download endpoint MUST respond with status 503 and the error code `unavailable`.
- **`ex-outage-3`**: If the identity service is unreachable, every endpoint MUST respond with status 503 and the error code `unavailable`.

## Acceptance criteria

- A test creates an export request, waits for `ready`, downloads the archive and verifies every checksum in `manifest.json`.
- A test requests an export while another is `running` for the same account and observes status 409 with the existing request id.
- A test creates 4 export requests for one account within 24 hours and observes status 429 on the 4th.
- A test uses a session token for a different account and observes status 403 on every endpoint.
- A test advances the clock to the expiry, 7 days past `ready`, and observes that the download endpoint returns status 410. It then advances the clock 1 more hour and observes that the archive is gone from the archive store and the state is `expired`.
- A test stops the worker during a build three times and observes the state `failed` with the failure reason `worker_lost`.
- A test makes the archive store reject writes and observes the state `failed` with the failure reason `storage_error` after 3 retries.
- A test makes reads of account data fail and observes the state `failed` with the failure reason `read_error` after 3 retries.
- A test alters a stored file before verification and observes the state `failed` with the failure reason `checksum_mismatch`.
- A load test meets `ex-perf-1` and `ex-perf-2`.
