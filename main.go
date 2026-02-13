// AWS API Gateway HTTP API v2 Local Proxy
// Simula fielmente o comportamento do AWS API Gateway HTTP API v2
// Zero dependências externas - somente standard library

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════════
// CONFIGURAÇÃO
// ════════════════════════════════════════════════════════════════════════════════

// Config representa a configuração global do proxy
type Config struct {
	Stage      string
	AccountID  string
	ApiID      string
	ListenAddr string
	Debug      bool
	Routes     []RouteConfig
}

// RouteConfig representa uma rota individual com sua Lambda associada
type RouteConfig struct {
	// PathPrefix é o prefixo do path que esta rota atende (ex: "/identity", "/admin")
	PathPrefix string `json:"pathPrefix"`
	// RouteTemplate é o template completo da rota para extração de path params
	RouteTemplate string `json:"routeTemplate"`
	// LambdaURL é a URL do endpoint de invocação da Lambda
	LambdaURL string `json:"lambdaUrl"`
	// Name é um nome amigável para a rota (opcional, para logs)
	Name string `json:"name,omitempty"`
}

// RoutesConfig é o formato do arquivo JSON de configuração de rotas
type RoutesConfig struct {
	Routes []RouteConfig `json:"routes"`
}

func loadConfig() Config {
	config := Config{
		Stage:      getEnv("STAGE", "$default"),
		AccountID:  getEnv("ACCOUNT_ID", "123456789012"),
		ApiID:      getEnv("API_ID", "local"),
		ListenAddr: getEnv("LISTEN_ADDR", ":3000"),
		Debug:      getEnv("DEBUG", "false") == "true",
		Routes:     []RouteConfig{},
	}

	// Tentar carregar rotas do arquivo JSON
	routesFile := getEnv("ROUTES_FILE", "")
	if routesFile != "" {
		routes, err := loadRoutesFromFile(routesFile)
		if err != nil {
			log.Printf("⚠️  Warning: Failed to load routes from file %s: %v", routesFile, err)
		} else {
			config.Routes = routes
		}
	}

	// Tentar carregar rotas do JSON inline (ROUTES env var)
	routesJSON := getEnv("ROUTES", "")
	if routesJSON != "" {
		routes, err := loadRoutesFromJSON(routesJSON)
		if err != nil {
			log.Printf("⚠️  Warning: Failed to parse ROUTES env var: %v", err)
		} else {
			config.Routes = append(config.Routes, routes...)
		}
	}

	// Fallback: usar LAMBDA_INVOKE_URL e ROUTE_TEMPLATE (modo legado/single route)
	if len(config.Routes) == 0 {
		lambdaURL := getEnv("LAMBDA_INVOKE_URL", "")
		routeTemplate := getEnv("ROUTE_TEMPLATE", "")

		if lambdaURL != "" {
			if routeTemplate == "" {
				routeTemplate = "/{proxy+}"
			}
			config.Routes = append(config.Routes, RouteConfig{
				PathPrefix:    extractPathPrefix(routeTemplate),
				RouteTemplate: routeTemplate,
				LambdaURL:     lambdaURL,
				Name:          "default",
			})
		}
	}

	// Se ainda não tiver rotas, usar default
	if len(config.Routes) == 0 {
		config.Routes = append(config.Routes, RouteConfig{
			PathPrefix:    "/",
			RouteTemplate: "/{proxy+}",
			LambdaURL:     "http://localhost:9000/2015-03-31/functions/function/invocations",
			Name:          "default",
		})
	}

	return config
}

// loadRoutesFromFile carrega rotas de um arquivo JSON
func loadRoutesFromFile(filepath string) ([]RouteConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return loadRoutesFromJSON(string(data))
}

// loadRoutesFromJSON carrega rotas de uma string JSON
func loadRoutesFromJSON(jsonStr string) ([]RouteConfig, error) {
	var routesConfig RoutesConfig
	if err := json.Unmarshal([]byte(jsonStr), &routesConfig); err != nil {
		// Tentar como array direto
		var routes []RouteConfig
		if err2 := json.Unmarshal([]byte(jsonStr), &routes); err2 != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		return routes, nil
	}
	return routesConfig.Routes, nil
}

// extractPathPrefix extrai o prefixo do path de um template de rota
func extractPathPrefix(template string) string {
	// Encontrar a posição do primeiro parâmetro
	paramIdx := strings.Index(template, "{")
	if paramIdx == -1 {
		return template
	}

	prefix := template[:paramIdx]
	// Remover trailing slash se houver
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return "/"
	}
	return prefix
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
	AccountID    string                                    `json:"accountId"`
	ApiID        string                                    `json:"apiId"`
	Authorizer   *APIGatewayV2HTTPRequestContextAuthorizer `json:"authorizer,omitempty"`
	DomainName   string                                    `json:"domainName"`
	DomainPrefix string                                    `json:"domainPrefix"`
	HTTP         APIGatewayV2HTTPRequestContextHTTP        `json:"http"`
	RequestID    string                                    `json:"requestId"`
	RouteKey     string                                    `json:"routeKey"`
	Stage        string                                    `json:"stage"`
	Time         string                                    `json:"time"`
	TimeEpoch    int64                                     `json:"timeEpoch"`
}

type APIGatewayV2HTTPRequestContextAuthorizer struct {
	JWT *APIGatewayV2HTTPRequestContextJWT `json:"jwt,omitempty"`
}

type APIGatewayV2HTTPRequestContextJWT struct {
	Claims map[string]string `json:"claims,omitempty"`
	Scopes []string          `json:"scopes,omitempty"`
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
// MULTI-ROUTE PROXY HANDLER
// ════════════════════════════════════════════════════════════════════════════════

// RouteHandler representa um handler para uma rota específica
type RouteHandler struct {
	route       RouteConfig
	pathMatcher *PathMatcher
}

// MultiRouteProxy é o handler principal que roteia para múltiplas Lambdas
type MultiRouteProxy struct {
	config     Config
	httpClient *http.Client
	routes     []*RouteHandler
}

func NewMultiRouteProxy(config Config) *MultiRouteProxy {
	proxy := &MultiRouteProxy{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		routes: make([]*RouteHandler, 0, len(config.Routes)),
	}

	// Criar handlers para cada rota
	for _, route := range config.Routes {
		handler := &RouteHandler{
			route:       route,
			pathMatcher: NewPathMatcher(route.RouteTemplate),
		}
		proxy.routes = append(proxy.routes, handler)
	}

	// Ordenar rotas por especificidade (mais específicas primeiro)
	// Rotas com prefixos mais longos têm prioridade
	sort.Slice(proxy.routes, func(i, j int) bool {
		return len(proxy.routes[i].route.PathPrefix) > len(proxy.routes[j].route.PathPrefix)
	})

	return proxy
}

// findRoute encontra a rota que melhor corresponde ao path da requisição
func (p *MultiRouteProxy) findRoute(path string) *RouteHandler {
	for _, handler := range p.routes {
		prefix := handler.route.PathPrefix
		// Verificar se o path começa com o prefixo da rota
		if prefix == "/" || strings.HasPrefix(path, prefix) {
			// Para prefixos não-root, verificar se é um match de segmento completo
			if prefix != "/" && len(path) > len(prefix) {
				// Garantir que o próximo caractere é '/' ou fim da string
				nextChar := path[len(prefix)]
				if nextChar != '/' {
					continue
				}
			}
			return handler
		}
	}
	return nil
}

func (p *MultiRouteProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Encontrar a rota correspondente
	handler := p.findRoute(r.URL.Path)
	if handler == nil {
		p.writeError(w, http.StatusNotFound, "No route matched for path: "+r.URL.Path)
		log.Printf("❌ No route matched: %s %s\n", r.Method, r.URL.Path)
		return
	}

	// Ler body da requisição
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	// Construir evento API Gateway v2
	event := p.buildEvent(r, bodyBytes, handler)

	// Debug: imprimir evento gerado
	if p.config.Debug {
		eventJSON, _ := json.MarshalIndent(event, "", "  ")
		log.Printf("═══════════════════════════════════════════════════════════════\n")
		log.Printf("📥 INCOMING REQUEST: %s %s\n", r.Method, r.URL.String())
		log.Printf("🎯 MATCHED ROUTE: %s → %s\n", handler.route.Name, handler.route.LambdaURL)
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
	lambdaResp, err := p.invokeLambda(handler.route.LambdaURL, eventJSON)
	if err != nil {
		log.Printf("❌ Lambda invocation failed [%s]: %v", handler.route.Name, err)
		p.writeError(w, http.StatusBadGateway, fmt.Sprintf("Lambda invocation failed: %v", err))
		return
	}

	// Debug: imprimir resposta da Lambda
	if p.config.Debug {
		log.Printf("📨 LAMBDA RESPONSE [%s]: StatusCode=%d, Body=%s\n",
			handler.route.Name, lambdaResp.StatusCode, lambdaResp.Body)
	}

	// Escrever resposta para o cliente
	p.writeResponse(w, lambdaResp)

	// Log de acesso
	duration := time.Since(startTime)
	routeName := handler.route.Name
	if routeName == "" {
		routeName = handler.route.PathPrefix
	}
	log.Printf("✅ %s %s → %d [%s] (%v)\n", r.Method, r.URL.Path, lambdaResp.StatusCode, routeName, duration)
}

func (p *MultiRouteProxy) buildEvent(r *http.Request, body []byte, handler *RouteHandler) APIGatewayV2HTTPRequest {
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

	// Extrair path parameters usando o template da rota correspondente
	pathParams := handler.pathMatcher.ExtractParams(r.URL.Path)

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

	// Construir routeKey usando o template da rota correspondente
	routeKey := fmt.Sprintf("%s %s", r.Method, handler.route.RouteTemplate)

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

	// Extract JWT Authorizer claims from Authorization header (simulates AWS API Gateway JWT Authorizer)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if authorizer := extractJWTClaims(authHeader); authorizer != nil {
			event.RequestContext.Authorizer = authorizer
			if p.config.Debug {
				log.Printf("🔑 JWT claims extracted: %d claims, %d scopes\n",
					len(authorizer.JWT.Claims), len(authorizer.JWT.Scopes))
			}
		}
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

func (p *MultiRouteProxy) invokeLambda(lambdaURL string, eventJSON []byte) (*LambdaResponse, error) {
	req, err := http.NewRequest("POST", lambdaURL, bytes.NewReader(eventJSON))
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

func (p *MultiRouteProxy) writeResponse(w http.ResponseWriter, resp *LambdaResponse) {
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
		_, _ = w.Write([]byte(resp.Body))
	}
}

func (p *MultiRouteProxy) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{
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

// extractJWTClaims extracts JWT claims from an Authorization header.
// Simulates AWS API Gateway JWT Authorizer behavior:
// - Decodes the JWT payload (no signature validation — local proxy trusts the token)
// - Converts all claim values to strings
// - Extracts scopes from "scope" or "scp" claim (space-delimited)
// Returns nil silently if anything fails (missing header, bad format, invalid base64/JSON).
func extractJWTClaims(authHeader string) *APIGatewayV2HTTPRequestContextAuthorizer {
	// Verificar se começa com "Bearer " (case-insensitive)
	if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "bearer ") {
		return nil
	}

	token := authHeader[7:]

	// JWT deve ter exatamente 3 segmentos: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}

	// Decodificar payload (segundo segmento) com base64url sem padding
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	// Parsear JSON do payload
	var rawClaims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &rawClaims); err != nil {
		return nil
	}

	// Converter todos os valores para string
	claims := make(map[string]string, len(rawClaims))
	for key, value := range rawClaims {
		switch v := value.(type) {
		case string:
			claims[key] = v
		default:
			// Numbers, bools, arrays, objects → serialize to JSON/fmt
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				claims[key] = fmt.Sprintf("%v", v)
			} else {
				claims[key] = string(jsonBytes)
			}
		}
	}

	// Extrair scopes da claim "scope" ou "scp" (split por espaço)
	var scopes []string
	scopeValue := ""
	if s, ok := rawClaims["scope"]; ok {
		if str, ok := s.(string); ok {
			scopeValue = str
		}
	} else if s, ok := rawClaims["scp"]; ok {
		if str, ok := s.(string); ok {
			scopeValue = str
		}
	}
	if scopeValue != "" {
		scopes = strings.Split(scopeValue, " ")
	}

	return &APIGatewayV2HTTPRequestContextAuthorizer{
		JWT: &APIGatewayV2HTTPRequestContextJWT{
			Claims: claims,
			Scopes: scopes,
		},
	}
}

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
║     AWS API Gateway HTTP API v2 - Local Proxy (Multi-Route)                   ║
║     🚀 Zero Dependencies | Pure Go | Production Ready                         ║
╚═══════════════════════════════════════════════════════════════════════════════╝`)

	fmt.Printf(`
📋 CONFIGURATION:
   ├─ Listen Address:    %s
   ├─ Stage:             %s
   ├─ Account ID:        %s
   ├─ API ID:            %s
   ├─ Debug Mode:        %v
   └─ Routes:            %d configured

`, config.ListenAddr, config.Stage, config.AccountID, config.ApiID, config.Debug, len(config.Routes))

	// Imprimir rotas configuradas
	fmt.Println("📍 CONFIGURED ROUTES:")
	for i, route := range config.Routes {
		name := route.Name
		if name == "" {
			name = fmt.Sprintf("route-%d", i+1)
		}
		fmt.Printf("   %d. [%s]\n", i+1, name)
		fmt.Printf("      ├─ Path Prefix:    %s\n", route.PathPrefix)
		fmt.Printf("      ├─ Route Template: %s\n", route.RouteTemplate)
		fmt.Printf("      └─ Lambda URL:     %s\n", route.LambdaURL)
	}
	fmt.Println()

	// Criar handler multi-rota
	proxy := NewMultiRouteProxy(config)

	// Health check endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		routeNames := make([]string, len(config.Routes))
		for i, route := range config.Routes {
			if route.Name != "" {
				routeNames[i] = route.Name
			} else {
				routeNames[i] = route.PathPrefix
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "healthy",
			"proxy":      "aws-api-gateway-v2-local",
			"routeCount": len(config.Routes),
			"routes":     routeNames,
		})
	})

	// Proxy para todas as outras rotas
	mux.Handle("/", proxy)

	// Iniciar servidor
	log.Printf("🟢 Proxy listening on %s\n", config.ListenAddr)
	log.Printf("📡 Routing to %d Lambda function(s)\n", len(config.Routes))
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
