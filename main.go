// AWS API Gateway HTTP API v2 Local Proxy
// Simula fielmente o comportamento do AWS API Gateway HTTP API v2
// Zero dependências externas - somente standard library

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════════
// CONFIGURAÇÃO
// ════════════════════════════════════════════════════════════════════════════════

type Config struct {
	LambdaInvokeURL string
	RouteTemplate   string
	Stage           string
	AccountID       string
	ApiID           string
	ListenAddr      string
	Debug           bool
}

func loadConfig() Config {
	return Config{
		LambdaInvokeURL: getEnv("LAMBDA_INVOKE_URL", "http://localhost:9000/2015-03-31/functions/function/invocations"),
		RouteTemplate:   getEnv("ROUTE_TEMPLATE", "/{proxy+}"),
		Stage:           getEnv("STAGE", "$default"),
		AccountID:       getEnv("ACCOUNT_ID", "123456789012"),
		ApiID:           getEnv("API_ID", "local"),
		ListenAddr:      getEnv("LISTEN_ADDR", ":3000"),
		Debug:           getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ════════════════════════════════════════════════════════════════════════════════
// ESTRUTURAS DO EVENTO API GATEWAY HTTP API v2
// ════════════════════════════════════════════════════════════════════════════════

type APIGatewayV2HTTPRequest struct {
	Version               string                         `json:"version"`
	RouteKey              string                         `json:"routeKey"`
	RawPath               string                         `json:"rawPath"`
	RawQueryString        string                         `json:"rawQueryString"`
	Cookies               []string                       `json:"cookies,omitempty"`
	Headers               map[string]string              `json:"headers"`
	QueryStringParameters map[string]string              `json:"queryStringParameters,omitempty"`
	PathParameters        map[string]string              `json:"pathParameters,omitempty"`
	RequestContext        APIGatewayV2HTTPRequestContext `json:"requestContext"`
	Body                  string                         `json:"body,omitempty"`
	IsBase64Encoded       bool                           `json:"isBase64Encoded"`
}

type APIGatewayV2HTTPRequestContext struct {
	AccountID    string                                `json:"accountId"`
	ApiID        string                                `json:"apiId"`
	DomainName   string                                `json:"domainName"`
	DomainPrefix string                                `json:"domainPrefix"`
	HTTP         APIGatewayV2HTTPRequestContextHTTP    `json:"http"`
	RequestID    string                                `json:"requestId"`
	RouteKey     string                                `json:"routeKey"`
	Stage        string                                `json:"stage"`
	Time         string                                `json:"time"`
	TimeEpoch    int64                                 `json:"timeEpoch"`
}

type APIGatewayV2HTTPRequestContextHTTP struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	SourceIP  string `json:"sourceIp"`
	UserAgent string `json:"userAgent"`
}

// ════════════════════════════════════════════════════════════════════════════════
// RESPOSTA DA LAMBDA
// ════════════════════════════════════════════════════════════════════════════════

type LambdaResponse struct {
	StatusCode      int               `json:"statusCode"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	IsBase64Encoded bool              `json:"isBase64Encoded,omitempty"`
	Cookies         []string          `json:"cookies,omitempty"`
}

// ════════════════════════════════════════════════════════════════════════════════
// PROXY HANDLER
// ════════════════════════════════════════════════════════════════════════════════

type ProxyHandler struct {
	config       Config
	httpClient   *http.Client
	pathMatcher  *PathMatcher
}

func NewProxyHandler(config Config) *ProxyHandler {
	return &ProxyHandler{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		pathMatcher: NewPathMatcher(config.RouteTemplate),
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Ler body da requisição
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	// Construir evento API Gateway v2
	event := p.buildEvent(r, bodyBytes)

	// Debug: imprimir evento gerado
	if p.config.Debug {
		eventJSON, _ := json.MarshalIndent(event, "", "  ")
		log.Printf("═══════════════════════════════════════════════════════════════\n")
		log.Printf("📥 INCOMING REQUEST: %s %s\n", r.Method, r.URL.String())
		log.Printf("📤 GENERATED EVENT:\n%s\n", string(eventJSON))
		log.Printf("═══════════════════════════════════════════════════════════════\n")
	}

	// Serializar evento para JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "Failed to serialize event")
		return
	}

	// Invocar Lambda
	lambdaResp, err := p.invokeLambda(eventJSON)
	if err != nil {
		log.Printf("❌ Lambda invocation failed: %v", err)
		p.writeError(w, http.StatusBadGateway, fmt.Sprintf("Lambda invocation failed: %v", err))
		return
	}

	// Debug: imprimir resposta da Lambda
	if p.config.Debug {
		log.Printf("📨 LAMBDA RESPONSE: StatusCode=%d, Body=%s\n", lambdaResp.StatusCode, lambdaResp.Body)
	}

	// Escrever resposta para o cliente
	p.writeResponse(w, lambdaResp)

	// Log de acesso
	duration := time.Since(startTime)
	log.Printf("✅ %s %s → %d (%v)\n", r.Method, r.URL.Path, lambdaResp.StatusCode, duration)
}

func (p *ProxyHandler) buildEvent(r *http.Request, body []byte) APIGatewayV2HTTPRequest {
	now := time.Now().UTC()

	// Extrair headers (lowercase)
	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[strings.ToLower(key)] = strings.Join(values, ",")
	}

	// Adicionar host header se não presente
	if _, ok := headers["host"]; !ok {
		headers["host"] = r.Host
	}

	// Extrair cookies
	var cookies []string
	if cookieHeader := r.Header.Get("Cookie"); cookieHeader != "" {
		cookies = strings.Split(cookieHeader, "; ")
	}

	// Extrair query parameters
	queryParams := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			queryParams[key] = values[0]
		} else {
			queryParams[key] = strings.Join(values, ",")
		}
	}

	// Extrair path parameters usando o template
	pathParams := p.pathMatcher.ExtractParams(r.URL.Path)

	// Extrair source IP
	sourceIP := extractSourceIP(r)

	// Extrair user agent
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "unknown"
	}

	// Extrair domínio
	domainName := r.Host
	domainPrefix := strings.Split(domainName, ".")[0]
	if colonIdx := strings.Index(domainPrefix, ":"); colonIdx != -1 {
		domainPrefix = domainPrefix[:colonIdx]
	}

	// Construir routeKey
	routeKey := fmt.Sprintf("%s %s", r.Method, p.config.RouteTemplate)

	// Construir evento
	event := APIGatewayV2HTTPRequest{
		Version:        "2.0",
		RouteKey:       routeKey,
		RawPath:        r.URL.Path,
		RawQueryString: r.URL.RawQuery,
		Headers:        headers,
		RequestContext: APIGatewayV2HTTPRequestContext{
			AccountID:    p.config.AccountID,
			ApiID:        p.config.ApiID,
			DomainName:   domainName,
			DomainPrefix: domainPrefix,
			HTTP: APIGatewayV2HTTPRequestContextHTTP{
				Method:    r.Method,
				Path:      r.URL.Path,
				Protocol:  r.Proto,
				SourceIP:  sourceIP,
				UserAgent: userAgent,
			},
			RequestID: generateRequestID(),
			RouteKey:  routeKey,
			Stage:     p.config.Stage,
			Time:      now.Format("02/Jan/2006:15:04:05 +0000"),
			TimeEpoch: now.UnixMilli(),
		},
		IsBase64Encoded: false,
	}

	// Adicionar cookies se existirem
	if len(cookies) > 0 {
		event.Cookies = cookies
	}

	// Adicionar query parameters se existirem
	if len(queryParams) > 0 {
		event.QueryStringParameters = queryParams
	}

	// Adicionar path parameters se existirem
	if len(pathParams) > 0 {
		event.PathParameters = pathParams
	}

	// Adicionar body se existir
	if len(body) > 0 {
		event.Body = string(body)
	}

	return event
}

func (p *ProxyHandler) invokeLambda(eventJSON []byte) (*LambdaResponse, error) {
	req, err := http.NewRequest("POST", p.config.LambdaInvokeURL, bytes.NewReader(eventJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke lambda: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Verificar se houve erro HTTP na invocação
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lambda returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var lambdaResp LambdaResponse
	if err := json.Unmarshal(respBody, &lambdaResp); err != nil {
		// Se não conseguir fazer parse como LambdaResponse, retornar como body direto
		return &LambdaResponse{
			StatusCode: http.StatusOK,
			Body:       string(respBody),
			Headers:    map[string]string{"content-type": "application/json"},
		}, nil
	}

	return &lambdaResp, nil
}

func (p *ProxyHandler) writeResponse(w http.ResponseWriter, resp *LambdaResponse) {
	// Definir headers da resposta
	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}

	// Definir cookies
	for _, cookie := range resp.Cookies {
		w.Header().Add("Set-Cookie", cookie)
	}

	// Definir status code
	statusCode := resp.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)

	// Escrever body
	if resp.Body != "" {
		w.Write([]byte(resp.Body))
	}
}

func (p *ProxyHandler) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// ════════════════════════════════════════════════════════════════════════════════
// PATH MATCHER - Extração de Path Parameters
// ════════════════════════════════════════════════════════════════════════════════

type PathMatcher struct {
	template   string
	regex      *regexp.Regexp
	paramNames []string
}

func NewPathMatcher(template string) *PathMatcher {
	pm := &PathMatcher{
		template:   template,
		paramNames: []string{},
	}

	// Converter template para regex
	// {param} -> captura nomeada
	// {proxy+} -> captura greedy

	regexPattern := template

	// Encontrar todos os parâmetros no template
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(template, -1)

	for _, match := range matches {
		paramName := match[1]

		// Verificar se é proxy+ (greedy)
		if strings.HasSuffix(paramName, "+") {
			cleanName := strings.TrimSuffix(paramName, "+")
			pm.paramNames = append(pm.paramNames, cleanName)
			// Substituir {proxy+} por captura greedy
			regexPattern = strings.Replace(regexPattern, match[0], "(.+)", 1)
		} else {
			pm.paramNames = append(pm.paramNames, paramName)
			// Substituir {param} por captura de segmento
			regexPattern = strings.Replace(regexPattern, match[0], "([^/]+)", 1)
		}
	}

	// Escapar caracteres especiais que não são parte da nossa substituição
	regexPattern = "^" + regexPattern + "$"

	pm.regex = regexp.MustCompile(regexPattern)
	return pm
}

func (pm *PathMatcher) ExtractParams(path string) map[string]string {
	params := make(map[string]string)

	if pm.regex == nil || len(pm.paramNames) == 0 {
		return params
	}

	matches := pm.regex.FindStringSubmatch(path)
	if len(matches) > 1 {
		for i, name := range pm.paramNames {
			if i+1 < len(matches) {
				params[name] = matches[i+1]
			}
		}
	}

	return params
}

// ════════════════════════════════════════════════════════════════════════════════
// FUNÇÕES AUXILIARES
// ════════════════════════════════════════════════════════════════════════════════

func extractSourceIP(r *http.Request) string {
	// Verificar X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	// Verificar X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Extrair do RemoteAddr
	remoteAddr := r.RemoteAddr
	if colonIdx := strings.LastIndex(remoteAddr, ":"); colonIdx != -1 {
		return remoteAddr[:colonIdx]
	}
	return remoteAddr
}

// Contador global para garantir unicidade
var requestCounter uint64 = 0

func generateRequestID() string {
	// Gerar um ID único baseado em timestamp + contador
	// Formato similar ao AWS: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	now := time.Now().UnixNano()
	requestCounter++
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(now>>32),
		uint16(now>>16),
		uint16(requestCounter),
		uint16(now>>48),
		uint64(now)&0xffffffffffff,
	)
}

// ════════════════════════════════════════════════════════════════════════════════
// MAIN
// ════════════════════════════════════════════════════════════════════════════════

func main() {
	config := loadConfig()

	// Banner
	fmt.Println(`
╔═══════════════════════════════════════════════════════════════════════════════╗
║     AWS API Gateway HTTP API v2 - Local Proxy                                 ║
║     🚀 Zero Dependencies | Pure Go | Production Ready                         ║
╚═══════════════════════════════════════════════════════════════════════════════╝`)

	fmt.Printf(`
📋 CONFIGURATION:
   ├─ Listen Address:    %s
   ├─ Lambda URL:        %s
   ├─ Route Template:    %s
   ├─ Stage:             %s
   ├─ Account ID:        %s
   ├─ API ID:            %s
   └─ Debug Mode:        %v

`, config.ListenAddr, config.LambdaInvokeURL, config.RouteTemplate, config.Stage, config.AccountID, config.ApiID, config.Debug)

	// Criar handler
	handler := NewProxyHandler(config)

	// Health check endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
			"proxy":  "aws-api-gateway-v2-local",
		})
	})

	// Proxy para todas as outras rotas
	mux.Handle("/", handler)

	// Iniciar servidor
	log.Printf("🟢 Proxy listening on %s\n", config.ListenAddr)
	log.Printf("📡 Forwarding to Lambda at %s\n", config.LambdaInvokeURL)
	log.Println("════════════════════════════════════════════════════════════════")

	server := &http.Server{
		Addr:         config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
