---
name: developer
archetype: support-library
model: claude-sonnet-5
tools: [Read, Write, Edit, Bash, Grep, Glob]
description: >
  Implemente features seguindo o shared kernel e as convenções de código.
---

# Developer Agent - native-aws-api-gateway-local-proxy

**Modelo:** Claude Sonnet 4  
**Ferramentas:** read, write, bash, edit, grep, glob  
**Foco:** Adicionar features, fazer debug, build & Docker, route matching

## Responsabilidades

1. **Adicionar novo campo** no evento gerado (sem stdlib breaking)
2. **Implementar feature nova** (novo path template tipo, novo header handling)
3. **Fazer debug** de route matching e JWT parsing
4. **Build & Docker** (multi-stage, zero dependencies)
5. **Testar com sam local** (integração com Lambda local)

## Fluxo de Trabalho

### 1. Adicionar Novo Campo no Evento

**Passo 1: Definir struct**

```go
// main.go
package main

type APIGatewayV2Event struct {
    Version              string                 `json:"version"`
    RouteKey             string                 `json:"routeKey"`
    RawPath              string                 `json:"rawPath"`
    RawQueryString       string                 `json:"rawQueryString"`
    Headers              map[string]string      `json:"headers"`
    QueryStringParameters map[string]string     `json:"queryStringParameters"`
    PathParameters       map[string]string      `json:"pathParameters"`
    
    // Novo campo exemplo: Custom request ID generator
    CustomRequestID      string                 `json:"customRequestId"`
    
    RequestContext       RequestContext         `json:"requestContext"`
    Body                 string                 `json:"body"`
    IsBase64Encoded      bool                   `json:"isBase64Encoded"`
}

type RequestContext struct {
    HTTP       HTTPMetadata  `json:"http"`
    RouteKey   string        `json:"routeKey"`
    Stage      string        `json:"stage"`
    RequestID  string        `json:"requestId"`
    TimeEpoch  int64         `json:"timeEpoch"`
    Authorizer Authorizer    `json:"authorizer"`
}

type HTTPMetadata struct {
    Method    string `json:"method"`
    Path      string `json:"path"`
    Protocol  string `json:"protocol"`
    SourceIP  string `json:"sourceIp"`
    UserAgent string `json:"userAgent"`
}

type Authorizer struct {
    Claims map[string]interface{} `json:"claims"`
}
```

**Passo 2: Gerar campo no handler**

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    // ... match route ...
    
    event := APIGatewayV2Event{
        Version:              "2.0",
        RouteKey:             fmt.Sprintf("%s %s", r.Method, r.URL.Path),
        RawPath:              r.URL.Path,
        RawQueryString:       r.URL.RawQuery,
        Headers:              normalizeHeaders(r.Header),
        QueryStringParameters: parseQueryString(r.URL.RawQuery),
        PathParameters:       extractPathParameters(route.Pattern, r.URL.Path),
        
        // Novo campo
        CustomRequestID:      generateCustomRequestID(),
        
        RequestContext: RequestContext{
            HTTP: HTTPMetadata{
                Method:    r.Method,
                Path:      r.URL.Path,
                Protocol:  r.Proto,
                SourceIP:  extractSourceIP(r),
                UserAgent: r.Header.Get("User-Agent"),
            },
            RouteKey:  fmt.Sprintf("%s %s", r.Method, r.URL.Path),
            Stage:     getStage(),
            RequestID: generateRequestID(),
            TimeEpoch: time.Now().UnixMilli(),
            Authorizer: parseAuthorizer(r),
        },
        Body:            readBody(r),
        IsBase64Encoded: false,
    }
    
    // Serializar e enviar para Lambda
    invokeHandler(event)
}

func generateCustomRequestID() string {
    // Implementar lógica customizada
    return "custom-" + generateUUID()
}
```

**Passo 3: Testar**

```bash
curl -i http://localhost:8080/users/123

# Verificar no debug:
# DEBUG=true ./proxy -port 8080
# [DEBUG] Custom request ID: custom-a1b2c3d4-...
```

### 2. Implementar Route Template Matching

**Passo 1: Estrutura de matching**

```go
package main

import (
    "regexp"
    "strings"
)

type RoutePattern struct {
    Method     string
    Pattern    string
    Regex      *regexp.Regexp
    ParamNames []string
}

func compileRoutePattern(method, pattern string) RoutePattern {
    paramNames := []string{}
    regexPattern := "^"
    
    parts := strings.Split(pattern, "/")
    for _, part := range parts {
        if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
            // Parameter
            paramName := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
            
            if strings.HasSuffix(paramName, "+") {
                // Catch-all {proxy+}
                paramName = strings.TrimSuffix(paramName, "+")
                paramNames = append(paramNames, paramName)
                regexPattern += "(.+)"
            } else {
                // Regular parameter {id}
                paramNames = append(paramNames, paramName)
                regexPattern += "([^/]+)"
            }
        } else if part != "" {
            // Literal
            regexPattern += "/" + regexp.QuoteMeta(part)
        }
    }
    
    regexPattern += "$"
    
    return RoutePattern{
        Method:     method,
        Pattern:    pattern,
        Regex:      regexp.MustCompile(regexPattern),
        ParamNames: paramNames,
    }
}

func (rp RoutePattern) Match(method, path string) (map[string]string, bool) {
    if rp.Method != method {
        return nil, false
    }
    
    matches := rp.Regex.FindStringSubmatch(path)
    if matches == nil {
        return nil, false
    }
    
    params := make(map[string]string)
    for i, paramName := range rp.ParamNames {
        params[paramName] = matches[i+1]
    }
    
    return params, true
}
```

**Passo 2: Usar em handler**

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    for _, route := range routes {
        params, matched := route.Match(r.Method, r.URL.Path)
        
        if matched {
            if isDebug() {
                log.Printf("[DEBUG] Route matched: %s %s\n", r.Method, r.URL.Path)
                log.Printf("[DEBUG] Path parameters: %v\n", params)
            }
            
            event.PathParameters = params
            invokeHandler(event)
            return
        }
    }
    
    // 404
    http.NotFound(w, r)
}
```

**Passo 3: Testar**

```bash
# Rota com pattern
export ROUTES='[{"method":"GET","path":"/users/{id}","handler":"getUser"}]'
./proxy -port 8080

# Testes
curl http://localhost:8080/users/123
# pathParameters: {"id": "123"}

curl http://localhost:8080/users/abc
# pathParameters: {"id": "abc"}

# Com catch-all
export ROUTES='[{"method":"GET","path":"/files/{proxy+}","handler":"getFile"}]'

curl http://localhost:8080/files/documents/2024/report.pdf
# pathParameters: {"proxy": "documents/2024/report.pdf"}
```

### 3. JWT Parsing

**Passo 1: Extrair JWT do header**

```go
func parseAuthorizer(r *http.Request) Authorizer {
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        return Authorizer{Claims: make(map[string]interface{})}
    }
    
    if !strings.HasPrefix(authHeader, "Bearer ") {
        return Authorizer{Claims: make(map[string]interface{})}
    }
    
    token := strings.TrimPrefix(authHeader, "Bearer ")
    claims := decodeJWT(token)
    
    if isDebug() {
        log.Printf("[DEBUG] JWT claims: %v\n", claims)
    }
    
    return Authorizer{Claims: claims}
}

func decodeJWT(token string) map[string]interface{} {
    // JWT tem 3 partes: header.payload.signature
    parts := strings.Split(token, ".")
    if len(parts) != 3 {
        if isDebug() {
            log.Println("[DEBUG] Invalid JWT format")
        }
        return make(map[string]interface{})
    }
    
    // Decode payload (segunda parte)
    payload := parts[1]
    
    // Add padding se necessário
    padding := 4 - (len(payload) % 4)
    if padding != 4 {
        payload += strings.Repeat("=", padding)
    }
    
    decoded, err := base64.RawURLEncoding.DecodeString(payload)
    if err != nil {
        if isDebug() {
            log.Printf("[DEBUG] JWT decode error: %v\n", err)
        }
        return make(map[string]interface{})
    }
    
    var claims map[string]interface{}
    if err := json.Unmarshal(decoded, &claims); err != nil {
        if isDebug() {
            log.Printf("[DEBUG] JWT unmarshal error: %v\n", err)
        }
        return make(map[string]interface{})
    }
    
    return claims
}
```

**Passo 2: Testar JWT**

```bash
# Gerar JWT teste (https://jwt.io)
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEyMyIsImVtYWlsIjoidXNlckBleGFtcGxlLmNvbSIsInJvbGVzIjpbImFkbWluIl19.sig..."

DEBUG=true ./proxy -port 8080

curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/users

# Output:
# [DEBUG] JWT claims: map[sub:user-123 email:user@example.com roles:[admin]]
```

### 4. Build & Docker

**Build local:**

```bash
# Build executável
go build -o proxy main.go

# Build com optimizações
go build -ldflags="-s -w" -o proxy main.go

# Verificar dependencies
go mod tidy
go mod graph  # Mostrar zero dependencies
```

**Docker multi-stage:**

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o proxy main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/proxy /usr/local/bin/
EXPOSE 8080
ENV ROUTES='[]'
ENV DEBUG=false
CMD ["proxy"]
```

**Build & run:**

```bash
# Build image
docker build -t api-gateway-proxy:latest .

# Run container
docker run -d \
  --name proxy \
  -p 8080:8080 \
  -e ROUTES='[{"method":"GET","path":"/health","handler":"health"}]' \
  api-gateway-proxy:latest

# Testar
curl http://localhost:8080/health

# Ver logs
docker logs proxy

# Com debug
docker run -it \
  -p 8080:8080 \
  -e DEBUG=true \
  api-gateway-proxy:latest
```

### 5. Integração com sam local

**Estrutura:**

```
.
├── main.go                  # Proxy
├── Dockerfile
├── routes.json
├── template.yaml            # SAM template
└── src/                      # Lambda functions
    └── handler.go
```

**routes.json:**

```json
[
  {
    "method": "GET",
    "path": "/health",
    "handler": "health"
  },
  {
    "method": "GET",
    "path": "/users/{id}",
    "handler": "getUser"
  },
  {
    "method": "POST",
    "path": "/users",
    "handler": "createUser"
  }
]
```

**template.yaml:**

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31

Globals:
  Function:
    Timeout: 30
    MemorySize: 128

Resources:
  MyAPIFunction:
    Type: AWS::Serverless::Function
    Properties:
      FunctionName: my-api
      CodeUri: src/
      Handler: handler.Handler
      Runtime: provided.al2
      Events:
        ApiEvent:
          Type: Api
          Properties:
            RestApiId: !Ref ApiGateway
            Path: '/{proxy+}'
            Method: ANY

  ApiGateway:
    Type: AWS::ApiGateway::RestApi
    Properties:
      Name: MyAPI
```

**Workflow:**

```bash
# Terminal 1: Iniciar proxy (simula API Gateway)
export ROUTES_FILE=routes.json
DEBUG=true ./proxy -port 8080

# Terminal 2: Iniciar sam local (simula Lambda)
sam local start-api --port 3000 --template template.yaml

# Terminal 3: Testar
curl http://localhost:8080/users/123
curl -X POST http://localhost:8080/users -d '{"name":"John"}'
```

## Checklist Antes de Submeter

- [ ] `go build` sem erros
- [ ] `go test ./...` passa
- [ ] `go mod tidy` (zero dependencies verificado)
- [ ] Feature testada com cURL
- [ ] Debug output correto (`DEBUG=true`)
- [ ] Docker build & run funciona
- [ ] sam local integration testada
- [ ] Sem breaking changes na struct APIGatewayV2Event
- [ ] RFC compliance (HTTP status codes corretos)
- [ ] Edge cases testados (empty body, missing headers, etc)

## Dicas de Debug

```bash
# Verbose debug
DEBUG=true ./proxy -port 8080 2>&1 | tee proxy.log

# Testar route matching
curl -v http://localhost:8080/users/123/profile

# Testar com arquivo de routes
export ROUTES_FILE=routes.json
DEBUG=true ./proxy

# Analisar evento JSON gerado
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"Test"}' \
  -v 2>&1 | grep -A 50 "routeKey"
```

## Links Úteis

- **CLAUDE.md:** Referência de evento
- **AWS API Gateway v2:** https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api.html
- **SAM Local:** https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-sam-cli-using-local-testing.html
