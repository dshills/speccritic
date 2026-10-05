# Request Rate Limiter

## Purpose

This document specifies a rate limiter library that decides whether a client is allowed to perform one more request. Services embed it to bound the request rate of each client.

## Non-goals

- Agreement between limiter instances beyond the shared counter store described in "Counter store".
- Queuing or delaying requests. A request is either allowed or rejected immediately.
- Billing and quota reporting.

## Definitions

- **Key**: a string supplied by the calling service that identifies the client being limited.
- **Limit**: the maximum number of allowed requests for one key in one window.
- **Window**: a fixed 60-second interval aligned to the Unix epoch. Window `n` covers the half-open interval from `60n` seconds to `60n + 60` seconds after 1970-01-01T00:00:00Z.
- **Counter**: the number of requests allowed so far for one key in one window.
- **MUST** and **MUST NOT** mark binding requirements, with the meaning given in RFC 2119.

## Interface

The library exposes one operation:

```
Allow(key string, limit int) (Decision, error)
```

`Decision` has three fields:

| Field | Type | Meaning |
|---|---|---|
| `Allowed` | boolean | Whether the request is permitted |
| `Remaining` | integer | `limit` minus the counter after this call, never below 0 |
| `ResetAt` | timestamp | The start of the next window, in Coordinated Universal Time |

## Requirements

### Input validation

- **`rl-val-1`**: `key` MUST be between 1 and 256 bytes long. For a key outside that range, `Allow` MUST return `Allowed = false` and the error `ErrInvalidKey` without reading or writing the counter store.
- **`rl-val-2`**: `limit` MUST be between 1 and 1,000,000 inclusive. For a limit outside that range, `Allow` MUST return `Allowed = false` and the error `ErrInvalidLimit` without reading or writing the counter store.

### Decision

- **`rl-dec-1`**: `Allow` MUST read the counter for `key` in the current window. A key with no counter has a counter of 0.
- **`rl-dec-2`**: If the counter is less than `limit`, `Allow` MUST increment the counter by exactly 1 and return `Allowed = true`.
- **`rl-dec-3`**: If the counter is equal to or greater than `limit`, `Allow` MUST return `Allowed = false` and MUST NOT change the counter.
- **`rl-dec-4`**: The read in `rl-dec-1` and the increment in `rl-dec-2` MUST be one atomic operation in the counter store, so that two concurrent calls for the same key never both read the same counter value and both increment it.
- **`rl-dec-5`**: The current window MUST be computed from the clock of the counter store, not from the clock of the process that calls `Allow`.

### Counter store

- **`rl-store-1`**: Counters MUST be kept in one counter store shared by every limiter instance of a deployment.
- **`rl-store-2`**: The counter store MUST delete a counter no earlier than the end of its window and no later than 120 seconds after the end of its window.
- **`rl-store-3`**: If the counter store returns an error, or does not answer within 50 milliseconds, `Allow` MUST return `Allowed = true`, `Remaining = 0` and the error `ErrStoreUnavailable`, and MUST increment the metric `limiter_store_errors_total` by 1.

### Performance

- **`rl-perf-1`**: With a counter store that answers in 1 millisecond, `Allow` MUST add no more than 2 milliseconds to the calling request at the 99th percentile, measured over 100,000 calls on the reference machine named in "Acceptance criteria".

### Observability

- **`rl-obs-1`**: `Allow` MUST increment the metric `limiter_decisions_total` exactly once per call, with the label `result` set to `allowed` or `rejected`.

## Acceptance criteria

- A test makes `limit + 1` sequential calls for one key inside one window and observes exactly `limit` allowed decisions followed by one rejected decision.
- A test makes 1,000 concurrent calls for one key with a limit of 100 and observes exactly 100 allowed decisions.
- A test makes the counter store unreachable and observes the behavior required by `rl-store-3`.
- A benchmark on the reference machine, which has 4 processor cores at 3.0 gigahertz and 16 gibibytes of memory, meets `rl-perf-1`.
