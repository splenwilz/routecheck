# Roadmap

This document outlines current capabilities and potential future directions.

## v1 — Current Scope

The v1 series focuses on **stateless endpoint validation**. Each request is independent.

### Included

- **Endpoint discovery** from OpenAPI specs, manual maps, or auto-probing
- **Safe-by-default execution** — mutations require explicit `--unsafe`
- **Authentication** — Bearer tokens, API keys, Basic auth
- **Multiple output formats** — CLI (human-readable) and JSON (machine-readable)
- **CI/CD integration** — exit codes, JSON output, non-interactive operation

### Design Principles

- Single binary, minimal dependencies
- Works with any REST API (OpenAPI not required)
- No server-side agents or instrumentation
- Stateless — each request stands alone

### What v1 Does NOT Include

- Lifecycle testing (create → read → update → delete sequences)
- Response value extraction or chaining
- Database seeding or fixture management
- Performance/load testing
- GraphQL or gRPC support

---

## v2 — Future Ideas

These are potential directions, not commitments. Priorities will be shaped by real-world usage.

### Under Consideration

- **Response assertions** — validate response body structure or values
- **Environment variables** — inject values from environment into requests
- **Request chaining** — use values from one response in subsequent requests
- **Custom validators** — user-defined pass/fail logic
- **Watch mode** — re-run tests on file changes
- **Parallel test suites** — run multiple endpoint maps concurrently

### Explicitly Out of Scope

- Lifecycle/stateful testing — use integration test frameworks instead
- Mocking or stubbing — routecheck tests real APIs
- Load testing — use dedicated tools (k6, wrk, locust)
- API design linting — use spectral or similar

---

## Feedback

Feature requests and bug reports welcome at: https://github.com/splenwilz/routecheck/issues
