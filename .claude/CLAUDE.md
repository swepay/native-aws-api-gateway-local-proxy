# native-aws-api-gateway-local-proxy

**Tipo:** CLI Tool - AWS API Gateway Local Simulator  
**Linguagem:** Go 1.21+  
**Dependencies:** Zero (stdlib only)  
**Uso:** Local development para testar Lambda functions com API Gateway events

## O que é

`native-aws-api-gateway-local-proxy` é um CLI tool Go que simula AWS API Gateway HTTP API v2 (payload format 2.0) **sem dependências externas**. Permite testar Lambda functions localmente com eventos realistas de API Gateway.

## Funcionalidades

### Multi-Route Support
Define múltiplas rotas via variável de ambiente JSON.

```bash
export ROUTES='[
  {"method":"GET","path":"/users","handler":"handleGetUsers"},
  {"method":"GET","path":"/users/{id}","handler":"handleGetUser"},
  {"method":"POST","path":"/users","handler":"handleCreateUser"}
]'

proxy -port 8080
```

### Path Parameter Extraction
Extrai path parameters de templates como `{id}`, `{proxy+}`.

```bash
GET /users/123
→ pathParameters: { "id": "123" }

GET /files/documents/2024/report.pdf (com /files/{proxy+})
→ pathParameters: { "proxy": "documents/2024/report.pdf" }
```

### JWT Parsing (sem validação)
Extrai claims de JWT sem validar assinatura (para dev/test).

```bash
Authorization: Bearer eyJhbGc...

Event JSON:
{
  "requestContext": {
    "authorizer": {
      "claims": {
        "sub": "user-123",
        "email": "user@example.com",
        "roles": ["admin"]
      }
    }
  }
}
```

### Header Normalization
Converte todos headers para lowercase.

```bash
X-Custom-Header: value
Authorization: Bearer token

→
headers: {
  "x-custom-header": "value",
  "authorization": "Bearer token"
}
```

### Debug Mode
Debug detalhado com `DEBUG=true`.

```bash
DEBUG=true ./proxy -port 8080

Output:
[DEBUG] Listening on :8080
[DEBUG] Route matched: GET /users/{id}
[DEBUG] Path parameters: {id=123}
[DEBUG] JWT claims: {sub=user-123, roles=[admin,user]}
[DEBUG] Lambda response: 200 OK
```

## Formato de Evento (API Gateway v2)

```json
{
  "version": "2.0",
  "routeKey": "GET /users/{id}",
  "rawPath": "/users/123",
  "rawQueryString": "page=1&limit=10",
  "headers": {
    "host": "localhost:8080",
    "user-agent": "curl/7.64.1",
    "content-type": "application/json",
    "authorization": "Bearer token123"
  },
  "queryStringParameters": {
    "page": "1",
    "limit": "10"
  },
  "pathParameters": {
    "id": "123"
  },
  "requestContext": {
    "http": {
      "method": "GET",
      "path": "/users/123",
      "protocol": "HTTP/1.1",
      "sourceIp": "127.0.0.1",
      "userAgent": "curl/7.64.1"
    },
    "routeKey": "GET /users/{id}",
    "stage": "dev",
    "requestId": "request-123",
    "timeEpoch": 1704067200000,
    "authorizer": {
      "claims": {
        "sub": "user-123",
        "email": "user@example.com",
        "scope": "read write"
      }
    }
  },
  "body": "{\"name\": \"John\"}",
  "isBase64Encoded": false
}
```

## Como Usar

### 1. Build

```bash
go build -o proxy main.go
```

### 2. Configurar Rotas

```bash
# Via environment variable (JSON)
export ROUTES='[
  {"method":"GET","path":"/users","handler":"listUsers"},
  {"method":"GET","path":"/users/{id}","handler":"getUser"},
  {"method":"POST","path":"/users","handler":"createUser"},
  {"method":"DELETE","path":"/users/{id}","handler":"deleteUser"}
]'

# Ou via arquivo
export ROUTES_FILE="./routes.json"
# routes.json:
# [
#   {"method":"GET","path":"/users","handler":"listUsers"}
# ]
```

### 3. Iniciar Proxy

```bash
# Padrão (localhost:8080)
./proxy

# Custom port
./proxy -port 9000

# Com debug
DEBUG=true ./proxy -port 8080

# Todos flags
./proxy -host 0.0.0.0 -port 8080 -stage prod
```

### 4. Testar com cURL

```bash
# GET
curl http://localhost:8080/users

# GET com path parameter
curl http://localhost:8080/users/123

# POST com body
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John","email":"john@example.com"}'

# Com JWT
curl http://localhost:8080/users \
  -H "Authorization: Bearer eyJhbGc..."

# Com query parameters
curl "http://localhost:8080/users?page=1&limit=10"
```

### 5. Testar com sam local

```bash
# Terminal 1: Iniciar proxy
./proxy -port 8080

# Terminal 2: Iniciar sam local
sam local start-api --port 3000

# Terminal 3: Testar
curl http://localhost:3000/users
```

### 6. Docker Multi-Stage

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o proxy main.go

# Runtime stage
FROM alpine:latest
COPY --from=builder /build/proxy /usr/local/bin/proxy
EXPOSE 8080
ENV ROUTES='[]'
ENV DEBUG=false
CMD ["proxy"]
```

```bash
# Build
docker build -t api-gateway-proxy:latest .

# Run
docker run -p 8080:8080 \
  -e ROUTES='[{"method":"GET","path":"/health","handler":"health"}]' \
  api-gateway-proxy:latest

# Ou com arquivo
docker run -p 8080:8080 \
  -v $(pwd)/routes.json:/routes.json \
  -e ROUTES_FILE=/routes.json \
  api-gateway-proxy:latest
```

## Path Template Matching

### Exatos
```
Pattern: /health
Request: /health → MATCH
Request: /healthz → NO MATCH
```

### Path Parameter `{id}`
```
Pattern: /users/{id}
Request: /users/123 → MATCH (id=123)
Request: /users/abc → MATCH (id=abc)
Request: /users/123/profile → NO MATCH
```

### Proxy/Catch-All `{proxy+}`
```
Pattern: /files/{proxy+}
Request: /files/docs/2024/report.pdf → MATCH (proxy=docs/2024/report.pdf)
Request: /files/ → NO MATCH
Request: /other/path → NO MATCH
```

### Combinações
```
Pattern: /api/{version}/users/{id}
Request: /api/v1/users/123 → MATCH (version=v1, id=123)

Pattern: /downloads/{proxy+}
Request: /downloads/folder/subfolder/file.zip → MATCH (proxy=folder/subfolder/file.zip)
```

## JWT Parsing (sem validação)

```
Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEyMyIsImVtYWlsIjoidXNlckBleGFtcGxlLmNvbSIsInJvbGVzIjpbImFkbWluIl0sImlhdCI6MTcwNDA2NzIwMH0.sig...

Parsed claims (sem validação de assinatura):
{
  "sub": "user-123",
  "email": "user@example.com",
  "roles": ["admin"],
  "iat": 1704067200
}
```

## Debug Mode

```bash
DEBUG=true ./proxy -port 8080

# Output:
[DEBUG] Starting API Gateway proxy
[DEBUG] Listening on :8080
[DEBUG] Route configuration: 3 routes

[2024-01-01T12:00:00Z] [INFO] GET /users → status 200
[DEBUG]   PathParameters: {}
[DEBUG]   QueryString: {}
[DEBUG]   Body: empty

[2024-01-01T12:00:01Z] [INFO] POST /users → status 201
[DEBUG]   PathParameters: {}
[DEBUG]   QueryString: {}
[DEBUG]   Body: {"name":"John","email":"john@example.com"}
[DEBUG]   JWT Claims: {sub: user-123}

[2024-01-01T12:00:02Z] [ERROR] GET /invalid → status 404
[DEBUG]   Route not matched
```

## Features Internamente

### Gerador de RequestID
Cada requisição tem UUID único.

```
requestId: "a1b2c3d4-e5f6-4789-abcd-ef1234567890"
```

### Timestamp
`timeEpoch` em milliseconds (Unix timestamp × 1000).

```
timeEpoch: 1704067200000  // 2024-01-01 12:00:00 UTC
```

### Source IP
Detecta automaticamente IP do cliente.

```
sourceIp: "127.0.0.1"  (localhost)
sourceIp: "192.168.1.100"  (network)
```

## Premissas

- **Sem dependencies:** Apenas stdlib Go
- **HTTP/1.1:** Não suporta HTTP/2
- **TLS:** Não suporta HTTPS (use proxy reverso)
- **JWT:** Apenas parsing, sem validação de signature
- **Payload v2:** Apenas formato v2.0 (não v1.0)
- **Síncrono:** Retorna resposta imediatamente

## Limitações

- Sem autenticação real (JWT não é validado)
- Sem rate limiting ou throttling
- Sem CORS handling automático
- Sem gzip compression
- Sem WebSocket support
- Sem streaming response
- Máximo request size: ~1GB (default net/http)

## Terminologia

- **Route:** Path + Method (GET /users)
- **Path Parameter:** `{id}` em template
- **Proxy Parameter:** `{proxy+}` catch-all
- **Route Key:** String "GET /users/{id}"
- **Path Template:** `/users/{id}` ou `/files/{proxy+}`
