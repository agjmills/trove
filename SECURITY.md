# Security

This document describes security considerations and important behavioral changes for Trove operators.

## CSRF Protection Migration (gorilla/csrf → filippo.io/csrf)

### Overview

Trove has migrated from `gorilla/csrf` to `filippo.io/csrf` (v0.2.1) for CSRF protection. This migration addresses a security vulnerability in the gorilla/csrf package and implements modern browser security features.

### Why This Change?

The `gorilla/csrf` package has known security issues and is no longer actively maintained. The `filippo.io/csrf` package provides:

- **Fetch Metadata-based protection**: Uses modern browser headers (`Sec-Fetch-Site`, `Sec-Fetch-Mode`) instead of tokens
- **Better security model**: Fundamentally addresses CSRF as a browser-specific issue
- **Reduced complexity**: No token management, rotation, or synchronization required

### Behavioral Differences

#### For Browser Users (No Change Required)

Normal browser interactions work seamlessly:

| Scenario | Result |
|----------|--------|
| Same-origin form submissions | ✅ Allowed |
| Same-origin AJAX requests | ✅ Allowed |
| Direct navigation (bookmarks, URL bar) | ✅ Allowed |
| Cross-origin requests from malicious sites | ❌ Blocked |

#### For API/CLI Clients (Important)

Non-browser clients are handled differently:

| Client Type | Behavior | Notes |
|------------|----------|-------|
| CLI tools (curl, wget) | ✅ Allowed | No Sec-Fetch headers = non-browser |
| API clients (requests, axios) | ✅ Allowed | Session auth still required |
| Mobile apps | ✅ Allowed | Use session cookies or API auth |
| Server-to-server | ✅ Allowed | No browser headers present |
| Webhooks | ✅ Allowed | External callbacks work |

**Key Insight**: The absence of `Sec-Fetch-Site` header indicates a non-browser client, which cannot be exploited for CSRF attacks (CSRF requires a browser to carry cookies automatically).

#### Breaking Change: Same-Site Requests

⚠️ **Important**: `filippo.io/csrf` is **stricter** than `gorilla/csrf` for same-site requests.

Requests with `Sec-Fetch-Site: same-site` (e.g., from subdomains) are **blocked**.

**Affected Scenarios**:
- Form submissions from `api.example.com` to `example.com`
- AJAX from `cdn.example.com` to `app.example.com`

**Workarounds**:
1. Ensure all requests come from the same origin (not just same site)
2. Use API endpoints that are exempt from CSRF (see below)
3. Use non-browser clients for cross-subdomain operations

### Token Removal

**CSRF tokens have been removed from templates.** The `filippo.io/csrf` library:

- Does NOT use token-based validation
- Protection is solely based on Fetch Metadata headers
- Hidden `csrf_token` form fields are no longer rendered
- JavaScript no longer needs to extract or send tokens

If your application relied on extracting/validating CSRF tokens programmatically, those patterns are no longer applicable.

### CSRF-Exempt Endpoints

The following endpoints are intentionally exempt from CSRF middleware to support non-browser clients:

| Endpoint | Method | Reason |
|----------|--------|--------|
| `/upload` | POST | Streaming multipart uploads |
| `/api/uploads/init` | POST | Chunked upload initialization |
| `/api/uploads/{id}/chunk` | POST | Chunk upload |
| `/api/uploads/{id}/complete` | POST | Upload completion |
| `/api/uploads/{id}` | DELETE | Upload cancellation |
| `/api/uploads/{id}/status` | GET | Upload status (read-only) |
| `/api/files/status` | GET | SSE status stream (read-only) |

These endpoints rely on:
1. Session-based authentication (`RequireAuth` middleware)
2. `SameSite=Lax` cookie policy

### Configuration

CSRF protection can be disabled entirely via environment variable:

```bash
CSRF_ENABLED=false  # Default: true
```

**Warning**: Disabling CSRF is not recommended for production deployments.

### Testing

The migration includes comprehensive integration tests in `internal/middleware/middleware_integration_test.go`:

- `TestCSRFProtection`: Basic browser scenarios
- `TestCSRFNonBrowserClients`: CLI, API, webhook, mobile app scenarios
- `TestCSRFBrowserBehavior`: Cross-site attack prevention
- `TestCSRFTokenBehavior`: Token deprecation verification

Run tests:
```bash
go test -v ./internal/middleware/... -run "TestCSRF"
```

### Troubleshooting

#### "Forbidden" errors on legitimate requests

1. **Check `Sec-Fetch-Site` header**: If present as `cross-site` or `same-site`, the request will be blocked
2. **Verify Origin**: Mismatched Origin headers (even without Sec-Fetch-Site) are rejected
3. **Use browser DevTools**: Network tab shows all headers being sent

#### API client requests failing

Ensure your client is NOT sending browser-like headers:
- Remove `Sec-Fetch-Site` header if manually set
- Ensure `Origin` header matches the target server (or omit it)

#### Cross-subdomain requests failing

Options:
1. Configure your application to serve from a single origin
2. Use the CSRF-exempt API endpoints
3. Implement custom authentication for cross-subdomain flows

### Security Considerations

1. **Session Security**: All protected endpoints still require valid session authentication
2. **Cookie Policy**: SameSite=Lax cookies prevent cross-site request forgery at the cookie level
3. **Rate Limiting**: Authentication endpoints remain rate-limited (5 attempts/15 minutes)
4. **HTTPS**: Production deployments should always use HTTPS

### References

- [filippo.io/csrf documentation](https://pkg.go.dev/filippo.io/csrf)
- [Fetch Metadata Request Headers](https://developer.mozilla.org/en-US/docs/Glossary/Fetch_metadata_request_header)
- [CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
