# Background Job Queue

## Purpose

This document specifies a queue that accepts jobs from producers and delivers them to workers that run them. It is used for work that must happen after a request has been answered, such as sending a receipt.

## Non-goals

- Scheduling a job for a future time. A job is deliverable as soon as it is enqueued.
- Job priorities. The only ordering rule is the one in "Ordering".
- Exactly-once execution. Workers MUST be written to tolerate running the same job more than once.

## Definitions

- **Job**: a payload plus a group key, enqueued by a producer.
- **Payload**: between 1 and 65,536 bytes of opaque data.
- **Group key**: a string of 1 to 128 bytes. Jobs with the same group key form a group.
- **Worker**: a process that receives jobs, runs them and acknowledges them.
- **Lease**: the 30 seconds after a delivery during which the job is hidden from other workers.
- **Attempt**: one delivery of a job to a worker.
- **Receipt**: a string that `Receive` returns with a job and that is unique to one attempt. `Ack` and `Nack` name the attempt they refer to by its receipt.
- **MUST** and **MUST NOT** mark binding requirements, with the meaning given in RFC 2119.

## Interface

The queue exposes four operations:

| Operation | Called by | Result |
|---|---|---|
| `Enqueue(group_key, payload)` | producer | The `id` of the stored job, or an error |
| `Receive()` | worker | The next deliverable job and the receipt of this attempt, or the error `ErrNoJob` when no job is deliverable |
| `Ack(receipt)` | worker | Nothing, or an error |
| `Nack(receipt)` | worker | Nothing, or an error |

A job is delivered when `Receive` returns it.

## Event schema

A job returned by `Receive` has these fields:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Assigned by the queue at enqueue time; unique among all jobs ever enqueued |
| `group_key` | string | The group key given by the producer |
| `payload` | bytes | The payload given by the producer |
| `attempt` | integer | 1 for the first delivery, increased by 1 for each later delivery |
| `receipt` | string | The receipt of this attempt |

## Requirements

### Enqueue

- **`jq-enq-1`**: `Enqueue(group_key, payload)` MUST reject a payload or group key outside the sizes given in "Definitions" with the error `ErrInvalidJob` and MUST NOT store the job.
- **`jq-enq-2`**: Otherwise `Enqueue` MUST store the job durably before it returns the job `id`. A job whose `id` was returned MUST survive a restart of the queue.

### Delivery guarantees

- **`jq-del-1`**: Delivery is at least once: every stored job MUST be delivered to a worker until one attempt is acknowledged or the job is moved to the dead-letter queue.
- **`jq-del-2`**: When a job is delivered, the queue MUST hide it from all other workers until its lease ends or the worker calls `Ack` or `Nack` for that attempt, whichever comes first.
- **`jq-del-3`**: A worker acknowledges a job by calling `Ack(receipt)` before the lease of that attempt ends. An acknowledged job MUST be deleted and MUST NOT be delivered again.
- **`jq-del-4`**: `Ack(receipt)` or `Nack(receipt)` called after the lease of that attempt has ended MUST return the error `ErrLeaseExpired` and MUST NOT change the job, even when the job has since been delivered again.
- **`jq-del-5`**: `Ack` or `Nack` called with a receipt that the queue did not issue MUST return the error `ErrUnknownReceipt`.

### Ordering

- **`jq-ord-1`**: Within one group, jobs MUST be delivered in the order in which `Enqueue` returned their `id`.
- **`jq-ord-2`**: The queue MUST NOT deliver a job while an earlier job of the same group is hidden by its lease or is still waiting for redelivery.
- **`jq-ord-3`**: There is no ordering rule between jobs of different groups.

### Redelivery and dead-letter behavior

- **`jq-redeliver-1`**: If an attempt numbered 1 to 4 is not acknowledged before its lease ends, the queue MUST make the job deliverable again, and its next delivery is a new attempt.
- **`jq-redeliver-2`**: A worker reports failure by calling `Nack(receipt)` before the lease of that attempt ends. `Nack` ends the lease at once. After a `Nack` on an attempt numbered 1 to 4, the queue MUST make the job deliverable again `2^attempt` seconds after it accepted the `Nack`, where `attempt` is the number of the attempt that failed.
- **`jq-redeliver-3`**: When the 5th attempt of a job receives a `Nack` or its lease ends unacknowledged, the queue MUST move the job to the dead-letter queue and MUST NOT deliver it again.
- **`jq-redeliver-4`**: A job in the dead-letter queue MUST be kept for 14 days and then deleted. Moving a job to the dead-letter queue unblocks later jobs of its group.

### Consumer failure behavior

- **`jq-fail-1`**: A worker that crashes or loses its connection while holding a job is treated exactly as a worker that did not acknowledge: `jq-redeliver-1` applies.
- **`jq-fail-2`**: If the job store is unreachable, `Enqueue`, `Receive`, `Ack` and `Nack` MUST return the error `ErrUnavailable` and delivery MUST pause until the store answers again. No job is lost or moved to the dead-letter queue because of the outage.

## Acceptance criteria

- A test enqueues three jobs with one group key and observes them delivered in enqueue order, each only after the previous one was acknowledged.
- A test never acknowledges a job and observes exactly 5 attempts, then finds the job in the dead-letter queue.
- A test calls `Nack` on attempt 2 and observes that the next delivery happens no sooner than 4 seconds later.
- A test restarts the queue after `Enqueue` returned and observes that the job is still delivered.
- A test calls `Ack` 31 seconds after delivery and observes `ErrLeaseExpired`.
