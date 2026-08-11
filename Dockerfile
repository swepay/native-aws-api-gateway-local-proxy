# ═══════════════════════════════════════════════════════════════════════════════
# AWS API Gateway HTTP API v2 - Local Proxy
# Multi-stage Dockerfile for minimal production image
# ═══════════════════════════════════════════════════════════════════════════════

# ─────────────────────────────────────────────────────────────────────────────────
# Stage 1: Build
# ─────────────────────────────────────────────────────────────────────────────────
FROM golang:1.21-alpine AS builder

# Instalar certificados CA para HTTPS
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copiar arquivos de dependência primeiro (melhor cache)
COPY go.mod ./

# Download de dependências (nenhuma neste caso, mas boa prática)
RUN go mod download

# Copiar código fonte
COPY *.go ./

# Build do binário
# CGO_ENABLED=0 para binário estático
# -ldflags para reduzir tamanho
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=1.0.0" \
    -o /app/proxy \
    .

# ─────────────────────────────────────────────────────────────────────────────────
# Stage 2: Runtime (imagem mínima)
# ─────────────────────────────────────────────────────────────────────────────────
FROM scratch

# Copiar certificados CA do builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copiar binário compilado
COPY --from=builder /app/proxy /proxy

# Definir variáveis de ambiente padrão
ENV LAMBDA_INVOKE_URL=http://localhost:9000/2015-03-31/functions/function/invocations
ENV ROUTE_TEMPLATE=/{proxy+}
ENV STAGE=$default
ENV ACCOUNT_ID=123456789012
ENV API_ID=local
ENV LISTEN_ADDR=:3000
ENV DEBUG=false

# Expor porta
EXPOSE 3000

# Executar proxy
ENTRYPOINT ["/proxy"]
