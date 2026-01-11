# Release Notes — v2.0.0

Lifecycle testing release.

## Supported Platforms

routecheck is a single Go binary with no runtime dependencies.

| Platform | Architecture | Status |
|----------|--------------|--------|
| Linux    | amd64        | Supported |
| Linux    | arm64        | Supported |
| macOS    | amd64        | Supported |
| macOS    | arm64 (Apple Silicon) | Supported |
| Windows  | amd64        | Supported |

Build from source with Go 1.21 or later:

```bash
go build -o routecheck ./cmd/routecheck
```

## Safety Guarantees

routecheck is designed to be safe by default.

### Safe Methods Only (Default)

The `probe` command and `validate` command (without `--unsafe`) only execute:
- GET
- HEAD
- OPTIONS
- TRACE

These methods are defined as "safe" by HTTP semantics — they should not modify server state.

### Explicit Opt-In for Mutations

To execute POST, PUT, PATCH, or DELETE requests, you must:
1. Use the `validate` command
2. Pass the `--unsafe` flag
3. Wait through a 3-second warning countdown

### No Accidental Writes

- `discover` command never makes HTTP requests to your API
- Auto-probing only uses GET requests
- Authentication credentials are never logged (masked in verbose output)

## Known Limitations

### ID Routes Return 404

Endpoints with path parameters like `/users/{id}` will likely return 404 because routecheck generates placeholder values (`1`, `test`, etc.) that don't exist in your database.

**This is expected behavior.** A 404 means the route is correctly configured and handles missing resources properly.

To test specific resources, use a manual endpoint map with known IDs.

### Stateless Testing (Default Mode)

In default mode (`probe` / `validate`), each request is independent. For dependent request sequences, use lifecycle mode.

### Lifecycle Testing (v2)

Lifecycle mode supports CREATE → READ → UPDATE → CLEANUP sequences:
- Extract values from responses (JSONPath capture)
- Use captured values in subsequent requests (path interpolation)
- Automatic cleanup even when earlier steps fail

See README.md for lifecycle spec format and examples.

### Parameter Generation

routecheck generates simple placeholder values for path and query parameters:
- Strings: `"test"`, `"example"`
- Integers: `1`
- UUIDs: `"00000000-0000-0000-0000-000000000001"`

These may not satisfy complex validation rules. Use manual endpoint maps with explicit examples for better control.

### No Request Body Inference

For POST/PUT/PATCH requests, routecheck uses:
1. Examples from OpenAPI spec (if available)
2. Examples from manual endpoint map (if provided)
3. Empty body (if neither available)

Complex request bodies should be defined in a manual map.

## Upgrade Policy

### Pre-1.0 Expectations

routecheck follows semantic versioning, but pre-1.0 releases may include breaking changes in minor versions.

| Version Bump | What to Expect |
|--------------|----------------|
| 0.1.x → 0.1.y | Bug fixes, no breaking changes |
| 0.1.x → 0.2.0 | May include breaking CLI or config changes |
| 0.x → 1.0.0 | Stable API, breaking changes only in major versions |

### What "Breaking Change" Means

- CLI flag renamed or removed
- Exit code semantics changed
- Manual map format changed
- Output format (JSON) schema changed

### What is NOT a Breaking Change

- New CLI flags (additive)
- New output fields (additive)
- Improved error messages
- Bug fixes that change behavior to match documentation

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All tests passed |
| 1 | One or more tests failed |
| 2 | Lifecycle cleanup failed (orphaned resources may exist) |

Exit code 2 only applies to lifecycle mode. In CI, treat exit code 2 as requiring manual intervention.

## Changelog

### v2.0.0

Lifecycle testing release.

- Lifecycle testing mode (`--enable-lifecycle`)
- Full CRUD sequences: CREATE → READ → UPDATE → CLEANUP
- JSONPath capture from responses (e.g., `$.id`, `$.data.user.id`)
- Path interpolation with `{{variable}}` syntax
- Automatic cleanup regardless of step failures
- Orphaned resource detection and warnings
- Exit code 2 for cleanup failures
- Invalid flag combination validation
- Prominent lifecycle mode warning banner

### v0.1.0

Initial release.

- `discover` command — list endpoints without execution
- `probe` command — test safe methods only
- `validate` command — test all methods with `--unsafe`
- OpenAPI/Swagger spec support (file or URL)
- Manual endpoint map support (YAML/JSON)
- Auto-probing fallback
- Authentication: Bearer, API Key, Basic
- JSON output for CI integration
