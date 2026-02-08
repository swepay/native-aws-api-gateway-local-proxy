# Copilot Instructions - AWS API Gateway HTTP API v2 Local Proxy

## Project Overview

This is a **pure Go** HTTP proxy that simulates AWS API Gateway HTTP API v2 behavior locally. It converts incoming HTTP requests into API Gateway v2 events (`version: "2.0"`) and forwards them to local Lambda runtimes. Supports **multi-route** configuration to route different path prefixes to different Lambda functions.

## Critical Constraints

- **Zero external dependencies** - Only Go standard library allowed
- **No reflection** - Use explicit struct definitions
- **Single file architecture** - All code in `main.go` (~700 lines)
- **Go 1.21+** required

## Architecture

```
HTTP Request → MultiRouteProxy.ServeHTTP() → findRoute() → buildEvent() → invokeLambda() → writeResponse()
                                                  ↓
                                        RouteHandler + PathMatcher (regex-based path param extraction)
```

### Key Components in `main.go`

| Component                 | Purpose                                                                      |
| ------------------------- | ---------------------------------------------------------------------------- |
| `Config`                  | ENV-based configuration with Routes array                                    |
| `RouteConfig`             | Single route: pathPrefix, routeTemplate, lambdaUrl, name                     |
| `MultiRouteProxy`         | Main HTTP handler with `ServeHTTP`, `findRoute`, `buildEvent`, `invokeLambda`|
| `RouteHandler`            | Per-route handler with PathMatcher and route config                          |
| `APIGatewayV2HTTPRequest` | Event struct matching AWS format                                             |
| `PathMatcher`             | Converts route templates like `/{realm}/token` to regex for param extraction |

## Developer Commands

```bash
# Build & run (single route - legacy)
go build -o proxy main.go && DEBUG=true ./proxy

# Build & run (multi-route)
ROUTES='{"routes":[{"pathPrefix":"/api","routeTemplate":"/api/{proxy+}","lambdaUrl":"http://localhost:9000/2015-03-31/functions/function/invocations","name":"api"}]}' ./proxy

# Run tests
go test -v ./...

# Docker
docker-compose up -d        # Full stack with mock Lambda
make docker-run             # Just the proxy
```

## Multi-Route Configuration

Routes can be configured via:
- `ROUTES` env var (JSON string)
- `ROUTES_FILE` env var (path to JSON file)
- Legacy single route via `LAMBDA_INVOKE_URL` + `ROUTE_TEMPLATE`

Routes are automatically sorted by path prefix length (most specific first).

```json
{
  "routes": [
    {"pathPrefix": "/identity", "routeTemplate": "/identity/{proxy+}", "lambdaUrl": "http://localhost:9001/...", "name": "identity"},
    {"pathPrefix": "/admin", "routeTemplate": "/admin/{proxy+}", "lambdaUrl": "http://localhost:9002/...", "name": "admin"},
    {"pathPrefix": "/", "routeTemplate": "/{proxy+}", "lambdaUrl": "http://localhost:9000/...", "name": "default"}
  ]
}
```

## Code Patterns

### Configuration via Environment Variables

Always use `getEnv(key, default)` pattern - never hardcode values:

```go
LambdaInvokeURL: getEnv("LAMBDA_INVOKE_URL", "http://localhost:9000/...")
```

### Headers Must Be Lowercase

AWS API Gateway converts all headers to lowercase. Follow this in `buildEvent()`:

```go
headers[strings.ToLower(key)] = strings.Join(values, ",")
```

### Path Parameter Extraction

Use `PathMatcher` with template syntax: `{param}` for single segment, `{proxy+}` for greedy match.

### Route Matching

Routes are matched by longest prefix first. Use `findRoute()` in `MultiRouteProxy`:

```go
handler := proxy.findRoute(r.URL.Path)
if handler == nil {
    // No matching route - return 404
}
```

### Lambda Response Handling

Always handle both structured `LambdaResponse` and raw JSON fallback in `invokeLambda()`.

## Testing Patterns

- Use `httptest.NewRequest` and `httptest.NewRecorder` for handler tests
- Mock Lambda server with `httptest.NewServer` for integration tests
- Use `newTestProxy(routes)` helper for creating test instances
- Test `PathMatcher` separately with various route patterns
- Test route specificity with overlapping prefixes
- See `main_test.go` for examples

## What NOT to Do

- Don't add external dependencies (chi, gin, etc.)
- Don't parse request body - preserve as-is
- Don't use reflection for JSON handling
- Don't modify the API Gateway v2 event format structure
