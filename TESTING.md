# Testing Philosophy

This document explains routecheck's testing approach and why certain behaviors are expected.

## Why ID Routes May Return 404

When testing endpoints like `GET /users/{id}`, routecheck generates placeholder values (e.g., `1`, `test`, `example`). These resources likely don't exist in your database, so a 404 response is **correct behavior**.

### This is not a bug

```
✗ GET  /users/1 [404]
   Not Found - endpoint or resource does not exist
```

A 404 here means:
- The route exists and is correctly configured
- The server correctly handles requests for non-existent resources
- Your API is working as designed

### What would be a bug

- **500 Internal Server Error** - the endpoint crashed
- **Connection refused** - the server isn't running
- **Timeout** - the endpoint is hanging

### How to handle this

1. **Accept 404 as valid** - In your manual endpoint map, include 404 as an expected status:

   ```yaml
   - method: GET
     path: /users/{id}
     expected_status: [200, 404]  # Both are valid
   ```

2. **Use real IDs** - If you need to test specific resources, create a manual map with known IDs:

   ```yaml
   - method: GET
     path: /users/123  # Known test user
     expected_status: [200]
   ```

3. **Understand the purpose** - routecheck validates that routes exist and respond correctly, not that specific data exists.

## Stateless vs Stateful Testing

routecheck performs **stateless testing**. Each request is independent.

### What stateless means

- No state is shared between requests
- Each endpoint is tested in isolation
- Test order doesn't matter
- No dependencies between tests

### What stateless is good for

- Smoke testing: "Do my routes respond?"
- Configuration validation: "Is my API deployed correctly?"
- Regression detection: "Did a deployment break something?"
- CI/CD gates: "Is the API healthy enough to proceed?"

### What stateless is NOT good for

- Testing business logic sequences (create → read → update → delete)
- Validating data transformations
- Testing authentication flows
- Verifying side effects

## Why Lifecycle Testing is Out of Scope (v1)

**Lifecycle testing** means testing sequences like:
1. Create a user (POST /users)
2. Read that user (GET /users/{created_id})
3. Update the user (PUT /users/{created_id})
4. Delete the user (DELETE /users/{created_id})

### Why we don't support this (yet)

1. **Complexity explosion** - Lifecycle tests require:
   - Extracting values from responses
   - Managing state between requests
   - Handling cleanup on failure
   - Defining dependency graphs

2. **Diminishing returns** - Once you need lifecycle testing, you're typically better served by:
   - Integration test frameworks (pytest, Jest)
   - Contract testing tools (Pact)
   - End-to-end test suites

3. **Scope creep** - routecheck's value is simplicity:
   - One binary, no dependencies
   - Point at an API, get results
   - Works with any REST API

### When to use other tools

| Use Case | Recommended Tool |
|----------|-----------------|
| Routes respond correctly | routecheck |
| Business logic works | Integration tests |
| Data flows correctly | Contract tests |
| Full user journeys | E2E tests |

## Expected Failures

Some failures are expected and don't indicate bugs:

### Authentication required (401/403)

If you don't provide `--auth`, protected endpoints will fail:

```
✗ GET  /admin/users [401]
   Unauthorized - authentication required
```

**Solution:** Add authentication: `--auth 'bearer:token'`

### Missing path parameters (400)

When routecheck generates placeholder values, validation may fail:

```
✗ GET  /users/invalid-uuid [400]
   Bad Request - invalid parameters sent
```

**Solution:** Accept this as valid route configuration, or use specific values in a manual map.

### Rate limiting (429)

Heavy testing may trigger rate limits:

```
✗ GET  /api/data [429]
   Too Many Requests
```

**Solution:** Reduce concurrency: `--concurrency 1`

## CI Integration

routecheck is designed for CI/CD pipelines:

### Exit codes

- **0** - All tests passed
- **1** - One or more tests failed

### Example GitHub Actions

```yaml
- name: Smoke test API
  run: |
    routecheck probe https://api.staging.example.com
```

### Example with auth from secrets

```yaml
- name: Validate API
  env:
    API_TOKEN: ${{ secrets.API_TOKEN }}
  run: |
    routecheck validate \
      --auth "bearer:$API_TOKEN" \
      https://api.staging.example.com
```

### JSON output for processing

```yaml
- name: Test and report
  run: |
    routecheck validate \
      --output json \
      https://api.example.com > results.json
```

## Interpreting Results

### Passed (green checkmark)

The endpoint responded with a 2xx status code (or matched expected_status):

```
✓ GET  /health [200] (45ms)
```

### Failed (red X)

The endpoint responded with an unexpected status code:

```
✗ GET  /users [500] (120ms)
   Server Error - the server encountered an error
```

### Common status codes

| Code | Meaning | Is it a bug? |
|------|---------|--------------|
| 200-299 | Success | No |
| 400 | Bad request | Maybe - check parameters |
| 401 | Unauthorized | No - add --auth |
| 403 | Forbidden | Maybe - check permissions |
| 404 | Not found | Maybe - check if route exists |
| 429 | Rate limited | No - reduce concurrency |
| 500+ | Server error | Yes - investigate |

## Best Practices

1. **Start with `discover`** - See what endpoints exist before testing
2. **Use `probe` first** - Safe methods won't modify data
3. **Add `--auth` for protected APIs** - Most real APIs need authentication
4. **Use `--output json` in CI** - Machine-readable output for parsing
5. **Create manual maps for known resources** - Test specific IDs when needed
6. **Accept 404 for parameterized routes** - It's correct behavior
