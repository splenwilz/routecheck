# V2 Design: Lifecycle Testing

This document defines the design for lifecycle testing in routecheck v2. This is a **design proposal**, not an implementation.

---

## Overview

Lifecycle testing enables create → read → update → delete (CRUD) sequences where each step depends on the previous one. Unlike v1's stateless testing, lifecycle tests:

- Extract values from responses (e.g., created resource IDs)
- Use extracted values in subsequent requests
- Attempt cleanup after test completion

---

## Goals

1. **Enable CRUD validation** — test that create/read/update/delete sequences work correctly
2. **Capture dynamic IDs** — extract resource identifiers from creation responses
3. **Best-effort cleanup** — attempt to delete created resources after testing
4. **Explicit opt-in** — lifecycle testing requires deliberate user action
5. **Fail-safe defaults** — refuse lifecycle tests without proper acknowledgements

---

## Non-Goals

1. **Guaranteed cleanup** — we cannot guarantee cleanup succeeds (network failure, permission changes, bugs)
2. **Transaction rollback** — we cannot undo partial sequences
3. **Database seeding** — we do not manage test fixtures or seed data
4. **Idempotent mutations** — we do not retry failed creates or deletes
5. **Parallel lifecycle tests** — sequences run serially to avoid race conditions
6. **Cross-lifecycle dependencies** — each lifecycle is independent

---

## Threat Model

### What Can Go Wrong

| Threat | Severity | Mitigation |
|--------|----------|------------|
| **Cleanup fails** — created resources persist | High | Warn user, log created IDs, provide manual cleanup command |
| **Partial sequence** — create succeeds, update fails, cleanup skipped | High | Always attempt cleanup regardless of sequence outcome |
| **Wrong environment** — lifecycle runs against production | Critical | Require explicit environment acknowledgement |
| **ID extraction fails** — capture expression doesn't match response | Medium | Fail sequence immediately, attempt cleanup of any created resources |
| **Rate limiting** — API throttles during sequence | Medium | Fail sequence, attempt cleanup |
| **Auth expires mid-sequence** — token expires during long sequence | Medium | Fail sequence, attempt cleanup |
| **Concurrent modification** — external process modifies resource | Low | Out of scope — user responsibility |
| **Cleanup deletes wrong resource** — ID collision or reuse | Low | Use exact captured ID, never guess |

### Unmitigable Risks

These risks cannot be eliminated by design:

1. **Network partition during cleanup** — resource created but cleanup request never arrives
2. **API bug in delete endpoint** — delete returns 200 but doesn't actually delete
3. **Cascading deletes** — deleting a resource deletes related resources unexpectedly
4. **Billing implications** — created resources may incur costs before cleanup

**Users must accept these risks explicitly.**

---

## Safety Guarantees

### What We Guarantee

1. **No lifecycle without explicit flag** — `--enable-lifecycle` required
2. **No lifecycle without acknowledgement** — `--i-accept-mutation-risk` required
3. **No lifecycle against implicit targets** — base URL must be explicitly provided
4. **Cleanup always attempted** — even if sequence fails midway
5. **Created IDs logged** — user can manually clean up if automated cleanup fails
6. **Sequence aborts on extraction failure** — we don't continue with missing IDs

### What We Do NOT Guarantee

1. **Cleanup success** — network, permissions, or bugs may prevent cleanup
2. **Atomic sequences** — partial completion is possible
3. **No side effects** — creates will create, even if cleanup fails
4. **Billing protection** — resources may incur costs

---

## Lifecycle Spec Format

### YAML Structure

```yaml
lifecycles:
  - name: user-crud
    description: Test user creation, retrieval, update, and deletion

    # Step 1: Create resource
    create:
      method: POST
      path: /api/v1/users
      body:
        name: "routecheck-test-user"
        email: "routecheck-test-{{timestamp}}@example.com"
      expected_status: [201]
      capture:
        user_id: "$.id"           # JSONPath expression
        user_email: "$.email"

    # Step 2: Read created resource (optional)
    read:
      method: GET
      path: /api/v1/users/{{user_id}}
      expected_status: [200]

    # Step 3: Update resource (optional)
    update:
      method: PATCH
      path: /api/v1/users/{{user_id}}
      body:
        name: "routecheck-updated-user"
      expected_status: [200]

    # Step 4: Cleanup (required)
    cleanup:
      method: DELETE
      path: /api/v1/users/{{user_id}}
      expected_status: [200, 204, 404]  # 404 acceptable if already deleted
```

### JSON Structure

```json
{
  "lifecycles": [
    {
      "name": "user-crud",
      "description": "Test user creation, retrieval, update, and deletion",
      "create": {
        "method": "POST",
        "path": "/api/v1/users",
        "body": {
          "name": "routecheck-test-user",
          "email": "routecheck-test-{{timestamp}}@example.com"
        },
        "expected_status": [201],
        "capture": {
          "user_id": "$.id",
          "user_email": "$.email"
        }
      },
      "read": {
        "method": "GET",
        "path": "/api/v1/users/{{user_id}}",
        "expected_status": [200]
      },
      "update": {
        "method": "PATCH",
        "path": "/api/v1/users/{{user_id}}",
        "body": {
          "name": "routecheck-updated-user"
        },
        "expected_status": [200]
      },
      "cleanup": {
        "method": "DELETE",
        "path": "/api/v1/users/{{user_id}}",
        "expected_status": [200, 204, 404]
      }
    }
  ]
}
```

### Spec Rules

1. **`create` is required** — every lifecycle must create something
2. **`cleanup` is required** — every lifecycle must define cleanup
3. **`capture` in create is required** — must capture at least one ID
4. **`read` and `update` are optional** — can test create-then-delete only
5. **`{{variable}}` interpolation** — captured values available in subsequent steps
6. **`{{timestamp}}`** — built-in, replaced with Unix timestamp for uniqueness
7. **JSONPath for capture** — standard JSONPath expressions (e.g., `$.id`, `$.data.user.id`)

---

## CLI Flags

### Required Flags for Lifecycle Testing

```
--enable-lifecycle          Enable lifecycle testing mode
--i-accept-mutation-risk    Acknowledge that mutations will occur
--lifecycle-file PATH       Path to lifecycle spec file (required if --enable-lifecycle)
```

### Optional Flags

```
--cleanup-only              Only run cleanup steps (for manual recovery)
--dry-run                   Show what would be executed without making requests
--cleanup-on-failure        Attempt cleanup even if create fails (default: true)
--log-created-ids PATH      Write created resource IDs to file for manual cleanup
```

### Example Usage

```bash
# Full lifecycle test
routecheck validate \
  --enable-lifecycle \
  --i-accept-mutation-risk \
  --lifecycle-file ./lifecycles.yaml \
  --auth 'bearer:token' \
  https://api.staging.example.com

# Dry run (no actual requests)
routecheck validate \
  --enable-lifecycle \
  --lifecycle-file ./lifecycles.yaml \
  --dry-run \
  https://api.staging.example.com

# Manual cleanup after failed run
routecheck validate \
  --enable-lifecycle \
  --i-accept-mutation-risk \
  --lifecycle-file ./lifecycles.yaml \
  --cleanup-only \
  --auth 'bearer:token' \
  https://api.staging.example.com
```

---

## When Lifecycle Testing is Allowed

Lifecycle testing proceeds when ALL of the following are true:

| Condition | Rationale |
|-----------|-----------|
| `--enable-lifecycle` flag present | Explicit opt-in |
| `--i-accept-mutation-risk` flag present | User acknowledges risk |
| `--lifecycle-file` points to valid spec | Must have defined sequences |
| Spec has valid `create` and `cleanup` for each lifecycle | Safety requirement |
| Base URL explicitly provided | No implicit targets |
| `--dry-run` NOT set (for actual execution) | Dry run is always allowed |

---

## When Lifecycle Testing is Refused

Lifecycle testing is **refused** if ANY of the following are true:

| Condition | Error Message |
|-----------|---------------|
| `--enable-lifecycle` without `--i-accept-mutation-risk` | "Lifecycle testing requires --i-accept-mutation-risk flag. This acknowledges that resources will be created and cleanup may fail." |
| `--enable-lifecycle` without `--lifecycle-file` | "Lifecycle testing requires --lifecycle-file to specify test sequences." |
| Lifecycle spec missing `create` step | "Lifecycle '{name}' missing required 'create' step." |
| Lifecycle spec missing `cleanup` step | "Lifecycle '{name}' missing required 'cleanup' step." |
| Lifecycle spec missing `capture` in create | "Lifecycle '{name}' create step must capture at least one ID for cleanup." |
| `--lifecycle-file` doesn't exist | "Lifecycle file not found: {path}" |
| `--lifecycle-file` is invalid YAML/JSON | "Failed to parse lifecycle file: {error}" |

---

## Execution Flow

```
1. Parse and validate lifecycle spec
2. For each lifecycle:
   a. Display: "Starting lifecycle: {name}"
   b. Execute CREATE step
      - If fails: log error, attempt CLEANUP anyway, mark lifecycle FAILED
      - If succeeds: extract captured values
   c. If capture fails: log error, skip to CLEANUP, mark lifecycle FAILED
   d. Execute READ step (if defined)
      - If fails: log warning, continue to UPDATE
   e. Execute UPDATE step (if defined)
      - If fails: log warning, continue to CLEANUP
   f. Execute CLEANUP step
      - If fails: log ERROR with captured IDs for manual cleanup
      - If succeeds: log success
   g. Report lifecycle result
3. Summary: X lifecycles passed, Y failed, Z cleanup failures
```

---

## Output Format

### CLI Output

```
Starting lifecycle: user-crud
  ✓ CREATE POST /api/v1/users [201] (145ms)
    Captured: user_id=abc123
  ✓ READ   GET  /api/v1/users/abc123 [200] (52ms)
  ✓ UPDATE PATCH /api/v1/users/abc123 [200] (78ms)
  ✓ CLEANUP DELETE /api/v1/users/abc123 [204] (61ms)
  ✓ Lifecycle PASSED

Starting lifecycle: order-crud
  ✓ CREATE POST /api/v1/orders [201] (203ms)
    Captured: order_id=def456
  ✗ READ   GET  /api/v1/orders/def456 [500] (89ms)
    Server error - continuing to cleanup
  ⚠ CLEANUP DELETE /api/v1/orders/def456 [403] (45ms)
    CLEANUP FAILED - manual cleanup required
    Resource ID: def456
    Cleanup command: curl -X DELETE https://api.example.com/api/v1/orders/def456
  ✗ Lifecycle FAILED (cleanup failed)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
LIFECYCLE SUMMARY
Total: 2 | Passed: 1 | Failed: 1 | Cleanup failures: 1

⚠ WARNING: 1 resource may not have been cleaned up.
  See above for manual cleanup commands.
```

### JSON Output

```json
{
  "lifecycles": [
    {
      "name": "user-crud",
      "status": "passed",
      "steps": [
        {"step": "create", "status": "passed", "captured": {"user_id": "abc123"}},
        {"step": "read", "status": "passed"},
        {"step": "update", "status": "passed"},
        {"step": "cleanup", "status": "passed"}
      ],
      "cleanup_successful": true
    }
  ],
  "summary": {
    "total": 1,
    "passed": 1,
    "failed": 0,
    "cleanup_failures": 0
  },
  "orphaned_resources": []
}
```

---

## Manual Cleanup Recovery

When cleanup fails, routecheck provides:

1. **Console warning** with resource ID and suggested curl command
2. **`--log-created-ids` file** with all created resource IDs
3. **`--cleanup-only` mode** to retry cleanup without re-running creates

### Created IDs Log Format

```json
{
  "created_at": "2026-01-11T10:30:00Z",
  "base_url": "https://api.staging.example.com",
  "resources": [
    {
      "lifecycle": "user-crud",
      "captured": {"user_id": "abc123"},
      "cleanup_path": "/api/v1/users/abc123",
      "cleanup_method": "DELETE",
      "cleanup_status": "success"
    },
    {
      "lifecycle": "order-crud",
      "captured": {"order_id": "def456"},
      "cleanup_path": "/api/v1/orders/def456",
      "cleanup_method": "DELETE",
      "cleanup_status": "failed",
      "cleanup_error": "403 Forbidden"
    }
  ]
}
```

---

## Open Questions

1. **Should `--dry-run` require `--i-accept-mutation-risk`?**
   - Pro: Consistent flag requirements
   - Con: Dry run has no risk, friction is unnecessary

2. **Should we support multiple cleanup attempts with backoff?**
   - Pro: Network glitches could cause transient failures
   - Con: Complexity, may mask real permission issues

3. **Should we support pre-create checks (e.g., verify resource doesn't exist)?**
   - Pro: Prevents duplicate creation
   - Con: Scope creep, race conditions

4. **Should lifecycle specs be embeddable in the existing manual map format?**
   - Pro: Single file for all endpoint definitions
   - Con: Mixing concerns, complex format

---

## Summary

Lifecycle testing is a **high-risk feature** that creates real resources. The design prioritizes:

1. **Explicit consent** — multiple flags required
2. **Transparency** — log all created resources
3. **Best-effort cleanup** — always attempt, never guarantee
4. **Recovery path** — provide manual cleanup tools

Users who enable lifecycle testing accept that:
- Resources will be created
- Cleanup may fail
- Manual intervention may be required
