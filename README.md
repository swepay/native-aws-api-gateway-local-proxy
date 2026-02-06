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
- ✅ **Path parameters** - Extração automática via template
- ✅ **Headers lowercase** - Comportamento idêntico ao API Gateway real
- ✅ **Preservação de body** - Sem alteração de encoding
- ✅ **Modo debug** - Visualização do evento gerado
- ✅ **Docker ready** - Imagem mínima (~10MB)
- ✅ **Health check** - Endpoint `/health` incluso

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

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `LAMBDA_INVOKE_URL` | URL do endpoint de invocação da Lambda | `http://localhost:9000/2015-03-31/functions/function/invocations` |
| `ROUTE_TEMPLATE` | Template de rota para extrair path parameters | `/{proxy+}` |
| `STAGE` | Stage do API Gateway | `$default` |
| `ACCOUNT_ID` | AWS Account ID simulado | `123456789012` |
| `API_ID` | API Gateway ID simulado | `local` |
| `LISTEN_ADDR` | Endereço de escuta do proxy | `:3000` |
| `DEBUG` | Modo debug (imprime eventos) | `false` |

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
├── APIGatewayV2*       # Structs do evento v2
├── LambdaResponse      # Struct da resposta
├── ProxyHandler        # Handler HTTP principal
│   ├── ServeHTTP()     # Entry point
│   ├── buildEvent()    # Constrói evento v2
│   ├── invokeLambda()  # Invoca Lambda via HTTP
│   └── writeResponse() # Escreve resposta HTTP
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
