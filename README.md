# routecheck

Runtime REST API testing tool. Validates that your API endpoints actually work.

## What routecheck is

routecheck is a **runtime truth checker** for REST APIs. It makes real HTTP requests to your API and reports what actually happens. Use it to:

- Verify endpoints respond with expected status codes
- Catch misconfigurations before they hit production
- Validate API behavior matches your OpenAPI spec
- Smoke test APIs during CI/CD

## What routecheck is NOT

- **Not unit tests** - routecheck tests the running API, not isolated code
- **Not mocks** - all requests go to real servers
- **Not a replacement for integration tests** - it validates routes exist and respond, not business logic
- **Not a load testing tool** - designed for correctness, not stress testing

## Installation

```bash
go install github.com/splenwilz/routecheck/cmd/routecheck@latest
```

Or build from source:

```bash
git clone https://github.com/splenwilz/routecheck
cd routecheck
go build -o routecheck ./cmd/routecheck
```

## Quick Start

### Discover endpoints (read-only)

List what endpoints are available without making test requests:

```bash
routecheck discover https://api.example.com
routecheck discover --openapi ./openapi.yaml https://api.example.com
```

### Probe endpoints (safe methods only)

Test endpoints using only safe HTTP methods (GET, HEAD, OPTIONS):

```bash
routecheck probe https://api.example.com
```

### Validate endpoints

Test endpoints including unsafe methods (POST, PUT, PATCH, DELETE):

```bash
# Safe methods only (default)
routecheck validate https://api.example.com

# Include mutation methods (requires explicit opt-in)
routecheck validate --unsafe https://api.example.com
```

### With authentication

```bash
# Bearer token
routecheck validate --auth 'bearer:your-jwt-token' https://api.example.com

# API key (default header: X-API-Key)
routecheck validate --auth 'api_key:your-api-key' https://api.example.com

# API key with custom header
routecheck validate --auth 'api_key:your-key' --auth-header 'Authorization' https://api.example.com

# Basic auth
routecheck validate --auth 'basic:username:password' https://api.example.com
```

### With OpenAPI spec

routecheck reads your OpenAPI/Swagger spec to discover endpoints:

```bash
# From file
routecheck validate --openapi ./openapi.yaml https://api.example.com

# From URL
routecheck validate --openapi https://api.example.com/openapi.json https://api.example.com
```

### With manual endpoint map

For APIs without OpenAPI specs, define endpoints in YAML or JSON:

```bash
routecheck validate --map ./endpoints.yaml https://api.example.com
```

Example `endpoints.yaml`:

```yaml
base_url: https://api.example.com
endpoints:
  - method: GET
    path: /health
    expected_status: [200]

  - method: GET
    path: /users/{id}
    params:
      path:
        id: integer
    expected_status: [200, 404]

  - method: POST
    path: /users
    body:
      content_type: application/json
      example:
        name: John Doe
        email: john@example.com
    expected_status: [201]
```

## Safety Guarantees

routecheck is **safe by default**:

1. **Safe methods only** - `probe` command only uses GET/HEAD/OPTIONS
2. **Explicit opt-in for mutations** - `validate` requires `--unsafe` flag for POST/PUT/PATCH/DELETE
3. **Warning before unsafe operations** - 3-second countdown before executing mutations
4. **No side effects from discovery** - `discover` command never makes HTTP requests to your API

### Safety classification

| Method  | Classification | Requires --unsafe |
|---------|---------------|-------------------|
| GET     | Safe          | No                |
| HEAD    | Safe          | No                |
| OPTIONS | Safe          | No                |
| TRACE   | Safe          | No                |
| POST    | Unsafe        | Yes               |
| PUT     | Unsafe        | Yes               |
| PATCH   | Unsafe        | Yes               |
| DELETE  | Unsafe        | Yes               |

## FastAPI Users

FastAPI automatically generates OpenAPI specs. Point routecheck at your `/openapi.json` endpoint:

```bash
# Start your FastAPI app
uvicorn main:app --host 0.0.0.0 --port 8000

# In another terminal
routecheck validate \
  --openapi http://localhost:8000/openapi.json \
  --auth 'bearer:your-token' \
  http://localhost:8000
```

## Commands

### discover

List endpoints without executing requests.

```bash
routecheck discover [options] <base-url>

Options:
  --openapi   Path or URL to OpenAPI/Swagger spec
  --map       Path to manual endpoint map file
  --output    Output format: cli, json (default: cli)
```

### probe

Discover and test endpoints using safe methods only.

```bash
routecheck probe [options] <base-url>

Options:
  --openapi      Path or URL to OpenAPI/Swagger spec
  --map          Path to manual endpoint map file
  --auth         Authentication (bearer:token, api_key:key, basic:user:pass)
  --auth-header  Custom header name for API key auth
  --timeout      Request timeout in seconds (default: 30)
  --concurrency  Max concurrent requests (default: 5)
  --output       Output format: cli, json (default: cli)
  --verbose      Enable verbose output
```

### validate

Test all endpoints. Safe methods run by default; use `--unsafe` for mutations.

```bash
routecheck validate [options] <base-url>

Options:
  --openapi      Path or URL to OpenAPI/Swagger spec
  --map          Path to manual endpoint map file
  --unsafe       Enable testing of POST, PUT, PATCH, DELETE
  --auth         Authentication (bearer:token, api_key:key, basic:user:pass)
  --auth-header  Custom header name for API key auth
  --timeout      Request timeout in seconds (default: 30)
  --concurrency  Max concurrent requests (default: 5)
  --output       Output format: cli, json (default: cli)
  --verbose      Enable verbose output
```

## Exit Codes

| Code | Meaning                |
|------|------------------------|
| 0    | All tests passed       |
| 1    | One or more tests failed or error occurred |

## Output Formats

### CLI (default)

Human-readable output with colors and suggestions:

```
✓ GET  /health [200] (45ms)
✗ GET  /users/1 [404] (120ms)
   Not Found - endpoint or resource does not exist
   Suggestions:
   • Verify the endpoint path is correct
   • Check if path parameters are valid

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
FAILED
Total: 2 | Passed: 1 | Failed: 1
```

### JSON

Machine-readable output for CI integration:

```bash
routecheck validate --output json https://api.example.com
```

## Endpoint Discovery Priority

routecheck discovers endpoints in this order:

1. **OpenAPI spec** (if `--openapi` provided) - fails fast on error
2. **Manual map** (if `--map` provided) - fails fast on error
3. **Auto-probe** (fallback) - silently probes common paths

## License

MIT
