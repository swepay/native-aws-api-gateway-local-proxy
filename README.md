# AWS API Gateway HTTP API v2 - Local Proxy

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](Dockerfile)
[![CI](https://github.com/swepay/native-aws-api-gateway-local-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/swepay/native-aws-api-gateway-local-proxy/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/swepay/native-aws-api-gateway-local-proxy)](https://github.com/swepay/native-aws-api-gateway-local-proxy/releases)

Proxy HTTP local que simula fielmente o comportamento do **AWS API Gateway HTTP API v2**, permitindo desenvolver e testar Lambdas localmente com eventos idênticos aos de produção.

```
┌─────────────────┐      ┌──────────────────────────┐      ┌─────────────────┐
│   HTTP Client   │ ──▶  │  API Gateway Local Proxy │ ──▶  │  Lambda Local   │
│ (curl/browser)  │      │     (this project)       │      │   (RIE/SAM)     │
└─────────────────┘      └──────────────────────────┘      └─────────────────┘
```

## 🚀 Features

- ✅ **Zero dependências externas** - Somente Go standard library
- ✅ **Single binary** - Compilação simples, distribuição fácil
- ✅ **Evento HTTP API v2** - Formato `"version": "2.0"` completo
- ✅ **Multi-route support** - Um proxy para múltiplas Lambdas
- ✅ **Path parameters** - Extração automática via template
- ✅ **Headers lowercase** - Comportamento idêntico ao API Gateway real
- ✅ **Preservação de body** - Sem alteração de encoding
- ✅ **Modo debug** - Visualização do evento gerado
- ✅ **Docker ready** - Imagem mínima (~10MB)
- ✅ **Health check** - Endpoint `/health` incluso
- ✅ **Security middleware** - Limite de payload, CORS whitelist, auth-header gate

## 🛡 Security Middleware

A partir da v1.x (F-SEC-10 do Swepay `GAPS_ROADMAP.md`), todas as requisições passam por um middleware de segurança configurável via variáveis de ambiente. Defaults são permissivos (dev-friendly); produção deve restringir.

| Variável | Default | Efeito |
| --- | --- | --- |
| `MAX_BODY_BYTES` | `6291456` (6 MiB) | Rejeita requests com `Content-Length` acima do valor (HTTP 413). Também corta o body lido em chunks. Alinhado com o limite de payload do AWS API Gateway HTTP API v2. |
| `CORS_ALLOWED_ORIGINS` | vazio (aceita tudo) | Lista CSV de origins permitidas (`https://app.swepay.com.br,https://admin.swepay.com.br`). Se presente, requests com `Origin` fora da lista recebem HTTP 403. Use `*` para aceitar explicitamente qualquer origin. |
| `REQUIRE_AUTH_HEADER` | `false` | Se `true`, qualquer request sem header `Authorization` recebe HTTP 401. Não valida o conteúdo do header — só a presença. |

**Exemplo — modo produção-leaning:**

```bash
docker run -p 3000:3000 \
  -e MAX_BODY_BYTES=1048576 \
  -e CORS_ALLOWED_ORIGINS=https://app.swepay.com.br \
  -e REQUIRE_AUTH_HEADER=true \
  -e ROUTES_FILE=/etc/proxy/routes.json \
  -v $(pwd)/routes.json:/etc/proxy/routes.json \
  ghcr.io/swepay/native-aws-api-gateway-local-proxy:latest
```

**Exemplo — modo dev (defaults):**

```bash
# Sem env vars extras - aceita tudo dentro do limite de 6 MiB.
docker run -p 3000:3000 ghcr.io/swepay/native-aws-api-gateway-local-proxy:latest
```

**Respostas de erro** seguem o formato `application/json`:

```json
{"error": "Content-Length 7000000 exceeds MAX_BODY_BYTES 6291456", "status": 413}
{"error": "origin \"https://evil.example.com\" not in CORS_ALLOWED_ORIGINS", "status": 403}
{"error": "missing Authorization header (REQUIRE_AUTH_HEADER=true)", "status": 401}
```

Ordem de rejeição: **auth-header > origin > body-size**. Testes cobrem cada cenário em `security_test.go`.

## 📋 Requisitos

- Go 1.21+ (para build local)
- Docker (opcional, para containerização)

## ⚡ Quick Start

### Build Local

```bash
# Clone o repositório
git clone https://github.com/swepay/native-aws-api-gateway-local-proxy.git
cd native-aws-api-gateway-local-proxy

# Build
go build -o proxy main.go

# Executar
./proxy
```

### Via Docker

```bash
# Build da imagem
docker build -t api-gateway-proxy .

# Executar
docker run -p 3000:3000 \
  -e LAMBDA_INVOKE_URL=http://host.docker.internal:9000/2015-03-31/functions/function/invocations \
  -e ROUTE_TEMPLATE="/identity/v1/realms/{realm}/protocol/openid-connect/token" \
  -e DEBUG=true \
  api-gateway-proxy
```

### Via GitHub Container Registry (Recomendado)

```bash
# Pull da imagem oficial
docker pull ghcr.io/swepay/native-aws-api-gateway-local-proxy:latest

# Executar
docker run -p 3000:3000 \
  -e LAMBDA_INVOKE_URL=http://host.docker.internal:9000/2015-03-31/functions/function/invocations \
  -e ROUTE_TEMPLATE="/identity/v1/realms/{realm}/protocol/openid-connect/token" \
  -e DEBUG=true \
  ghcr.io/swepay/native-aws-api-gateway-local-proxy:latest

# Ou usar uma versão específica
docker pull ghcr.io/swepay/native-aws-api-gateway-local-proxy:v1.0.0
```

### Via Docker Compose (Stack Completa)

```bash
# Iniciar proxy + lambda mock
docker-compose up -d

# Ver logs
docker-compose logs -f api-gateway-proxy
```

## 🔧 Configuração

Todas as configurações são via variáveis de ambiente:

### Modo Single-Route (Simples)

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `LAMBDA_INVOKE_URL` | URL do endpoint de invocação da Lambda | `http://localhost:9000/2015-03-31/functions/function/invocations` |
| `ROUTE_TEMPLATE` | Template de rota para extrair path parameters | `/{proxy+}` |
| `STAGE` | Stage do API Gateway | `$default` |
| `ACCOUNT_ID` | AWS Account ID simulado | `123456789012` |
| `API_ID` | API Gateway ID simulado | `local` |
| `LISTEN_ADDR` | Endereço de escuta do proxy | `:3000` |
| `DEBUG` | Modo debug (imprime eventos) | `false` |

### Modo Multi-Route (Múltiplas Lambdas)

| Variável | Descrição |
|----------|-----------|
| `ROUTES` | Configuração JSON das rotas (inline) |
| `ROUTES_FILE` | Caminho para arquivo JSON de configuração |

## 🔀 Multi-Route: Um Proxy, Múltiplas Lambdas

O proxy suporta rotear diferentes prefixos de path para diferentes Lambdas, permitindo simular um API Gateway completo com múltiplos backends:

```
┌─────────────────┐      ┌──────────────────────────┐      ┌─────────────────┐
│   HTTP Client   │      │   Multi-Route Proxy      │      │ Identity Lambda │
│                 │ ──▶  │                          │ ──▶  │ (port 9001)     │
│ /identity/...   │      │  /identity → :9001       │      └─────────────────┘
│ /admin/...      │      │  /admin    → :9002       │      ┌─────────────────┐
│ /openid/...     │      │  /openid   → :9003       │ ──▶  │  Admin Lambda   │
└─────────────────┘      └──────────────────────────┘      │ (port 9002)     │
                                                           └─────────────────┘
                                                           ┌─────────────────┐
                                                      ──▶  │  OpenID Lambda  │
                                                           │ (port 9003)     │
                                                           └─────────────────┘
```

### Configuração via JSON (Variável ROUTES)

```bash
export ROUTES='{
  "routes": [
    {
      "pathPrefix": "/identity",
      "routeTemplate": "/identity/v1/realms/{realm}/protocol/openid-connect/{action}",
      "lambdaUrl": "http://localhost:9001/2015-03-31/functions/function/invocations",
      "name": "identity"
    },
    {
      "pathPrefix": "/admin",
      "routeTemplate": "/admin/{proxy+}",
      "lambdaUrl": "http://localhost:9002/2015-03-31/functions/function/invocations",
      "name": "admin"
    },
    {
      "pathPrefix": "/openid",
      "routeTemplate": "/openid/{proxy+}",
      "lambdaUrl": "http://localhost:9003/2015-03-31/functions/function/invocations",
      "name": "openid"
    },
    {
      "pathPrefix": "/",
      "routeTemplate": "/{proxy+}",
      "lambdaUrl": "http://localhost:9000/2015-03-31/functions/function/invocations",
      "name": "default"
    }
  ]
}'

./proxy
```

### Configuração via Arquivo

```bash
# Criar arquivo de configuração
cat > routes.json << 'EOF'
{
  "routes": [
    {"pathPrefix": "/identity", "routeTemplate": "/identity/{proxy+}", "lambdaUrl": "http://localhost:9001/2015-03-31/functions/function/invocations", "name": "identity"},
    {"pathPrefix": "/admin", "routeTemplate": "/admin/{proxy+}", "lambdaUrl": "http://localhost:9002/2015-03-31/functions/function/invocations", "name": "admin"},
    {"pathPrefix": "/", "routeTemplate": "/{proxy+}", "lambdaUrl": "http://localhost:9000/2015-03-31/functions/function/invocations", "name": "default"}
  ]
}
EOF

# Executar com arquivo
ROUTES_FILE=routes.json ./proxy
```

### Ordem de Prioridade das Rotas

As rotas são automaticamente ordenadas por especificidade (prefixo mais longo primeiro):

```
1. /api/v1/users  (mais específico)
2. /api/v1
3. /api
4. /             (fallback)
```

Isso garante que `/api/v1/users/123` seja roteado para a Lambda de users, não para a Lambda genérica de `/api`.

### Docker Compose com Múltiplas Lambdas

```yaml
services:
  api-gateway-proxy:
    image: ghcr.io/swepay/native-aws-api-gateway-local-proxy:latest
    ports:
      - "3000:3000"
    environment:
      DEBUG: "true"
      ROUTES: |
        {
          "routes": [
            {"pathPrefix": "/identity", "routeTemplate": "/identity/{proxy+}", "lambdaUrl": "http://identity-lambda:8080/2015-03-31/functions/function/invocations", "name": "identity"},
            {"pathPrefix": "/admin", "routeTemplate": "/admin/{proxy+}", "lambdaUrl": "http://admin-lambda:8080/2015-03-31/functions/function/invocations", "name": "admin"},
            {"pathPrefix": "/", "routeTemplate": "/{proxy+}", "lambdaUrl": "http://default-lambda:8080/2015-03-31/functions/function/invocations", "name": "default"}
          ]
        }
    depends_on:
      - identity-lambda
      - admin-lambda
      - default-lambda

  identity-lambda:
    image: your-identity-lambda:latest

  admin-lambda:
    image: your-admin-lambda:latest

  default-lambda:
    image: your-default-lambda:latest
```

### Exemplos de ROUTE_TEMPLATE

```bash
# Rota simples
ROUTE_TEMPLATE="/api/users"

# Com path parameter
ROUTE_TEMPLATE="/api/users/{userId}"

# Múltiplos parameters
ROUTE_TEMPLATE="/identity/v1/realms/{realm}/protocol/openid-connect/{action}"

# Proxy catch-all
ROUTE_TEMPLATE="/{proxy+}"
```

## 📡 Evento Gerado

Para uma requisição:

```bash
curl -X POST http://localhost:3000/identity/v1/realms/master/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=myapp"
```

O proxy gera este evento (formato API Gateway HTTP API v2):

```json
{
  "version": "2.0",
  "routeKey": "POST /identity/v1/realms/{realm}/protocol/openid-connect/token",
  "rawPath": "/identity/v1/realms/master/protocol/openid-connect/token",
  "rawQueryString": "",
  "headers": {
    "content-type": "application/x-www-form-urlencoded",
    "host": "localhost:3000",
    "user-agent": "curl/8.0.0"
  },
  "pathParameters": {
    "realm": "master"
  },
  "requestContext": {
    "accountId": "123456789012",
    "apiId": "local",
    "domainName": "localhost:3000",
    "domainPrefix": "localhost",
    "http": {
      "method": "POST",
      "path": "/identity/v1/realms/master/protocol/openid-connect/token",
      "protocol": "HTTP/1.1",
      "sourceIp": "127.0.0.1",
      "userAgent": "curl/8.0.0"
    },
    "requestId": "abc12345-1234-5678-9abc-def012345678",
    "routeKey": "POST /identity/v1/realms/{realm}/protocol/openid-connect/token",
    "stage": "$default",
    "time": "06/Feb/2026:10:00:00 +0000",
    "timeEpoch": 1770134400000
  },
  "body": "grant_type=client_credentials&client_id=myapp",
  "isBase64Encoded": false
}
```

## 🧪 Testando

### Com Lambda Mock (incluída)

```bash
# Iniciar stack
docker-compose up -d

# Testar
curl -v http://localhost:3000/identity/v1/realms/master/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials"

# Ver logs do proxy
docker-compose logs -f api-gateway-proxy
```

### Com AWS SAM Local

```bash
# Em outro terminal, iniciar SAM
sam local start-lambda -p 9000

# Configurar proxy
export LAMBDA_INVOKE_URL="http://localhost:9000/2015-03-31/functions/YourFunction/invocations"
export ROUTE_TEMPLATE="/api/{resource}"
./proxy
```

### Com Lambda Container (Docker)

```bash
# Iniciar Lambda Python
docker run -p 9000:8080 \
  -v $(pwd)/your-lambda:/var/task \
  public.ecr.aws/lambda/python:3.11 \
  app.handler

# Em outro terminal, executar proxy
./proxy
```

## 🏗️ Arquitetura

```
main.go
├── Config              # Configuração via ENV
├── RouteConfig         # Configuração de uma rota
├── APIGatewayV2*       # Structs do evento v2
├── LambdaResponse      # Struct da resposta
├── MultiRouteProxy     # Handler HTTP principal
│   ├── ServeHTTP()     # Entry point
│   ├── findRoute()     # Encontra rota por prefix
│   ├── buildEvent()    # Constrói evento v2
│   ├── invokeLambda()  # Invoca Lambda via HTTP
│   └── writeResponse() # Escreve resposta HTTP
├── RouteHandler        # Handler por rota
│   └── PathMatcher     # Extração de path params
├── PathMatcher         # Extração de path params
│   └── ExtractParams() # Regex-based extraction
└── main()              # Bootstrap
```

### Fluxo de Execução

1. **Recebe requisição HTTP** (curl, browser, etc.)
2. **Extrai componentes** (method, path, headers, body, query)
3. **Converte headers** para lowercase (comportamento AWS)
4. **Extrai path parameters** usando ROUTE_TEMPLATE
5. **Monta evento v2** completo com requestContext
6. **POST para Lambda** no endpoint de invocação
7. **Parse resposta** (statusCode, headers, body)
8. **Retorna ao cliente** HTTP original

## 📦 Build para Produção

### Linux AMD64

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o proxy main.go
```

### Linux ARM64 (Graviton)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o proxy main.go
```

### Windows

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o proxy.exe main.go
```

### Docker (Multi-arch)

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t api-gateway-proxy .
```

## 🔗 Integração com Outros Projetos

### NativeGuard (Lambda Go)

```bash
# Iniciar NativeGuard Lambda
docker run -p 9000:8080 nativeguard:latest

# Configurar proxy para Keycloak-compatible route
LAMBDA_INVOKE_URL=http://localhost:9000/2015-03-31/functions/function/invocations \
ROUTE_TEMPLATE="/identity/v1/realms/{realm}/protocol/openid-connect/token" \
./proxy
```

### LocalStack

```bash
# Lambda no LocalStack usa porta diferente
LAMBDA_INVOKE_URL=http://localhost:4566/2015-03-31/functions/myfunction/invocations \
./proxy
```

## 📝 License

MIT License - veja [LICENSE](LICENSE) para detalhes.

## 🤝 Contributing

1. Fork o repositório
2. Crie uma branch (`git checkout -b feature/awesome`)
3. Commit suas mudanças (`git commit -m 'Add awesome feature'`)
4. Push para a branch (`git push origin feature/awesome`)
5. Abra um Pull Request

---

**Made with ❤️ by SwePay Team**
