package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO PATH MATCHER
// ════════════════════════════════════════════════════════════════════════════════

func TestPathMatcher_SimpleParam(t *testing.T) {
	pm := NewPathMatcher("/api/users/{userId}")

	params := pm.ExtractParams("/api/users/123")

	if params["userId"] != "123" {
		t.Errorf("Expected userId=123, got %s", params["userId"])
	}
}

func TestPathMatcher_MultipleParams(t *testing.T) {
	pm := NewPathMatcher("/identity/v1/realms/{realm}/protocol/openid-connect/{action}")

	params := pm.ExtractParams("/identity/v1/realms/master/protocol/openid-connect/token")

	if params["realm"] != "master" {
		t.Errorf("Expected realm=master, got %s", params["realm"])
	}
	if params["action"] != "token" {
		t.Errorf("Expected action=token, got %s", params["action"])
	}
}

func TestPathMatcher_ProxyPlus(t *testing.T) {
	pm := NewPathMatcher("/{proxy+}")

	params := pm.ExtractParams("/some/deep/nested/path")

	if params["proxy"] != "some/deep/nested/path" {
		t.Errorf("Expected proxy=some/deep/nested/path, got %s", params["proxy"])
	}
}

func TestPathMatcher_NoParams(t *testing.T) {
	pm := NewPathMatcher("/api/health")

	params := pm.ExtractParams("/api/health")

	if len(params) != 0 {
		t.Errorf("Expected no params, got %d", len(params))
	}
}

func TestPathMatcher_KeecloakRoute(t *testing.T) {
	pm := NewPathMatcher("/identity/v1/realms/{realm}/protocol/openid-connect/token")

	testCases := []struct {
		path          string
		expectedRealm string
	}{
		{"/identity/v1/realms/master/protocol/openid-connect/token", "master"},
		{"/identity/v1/realms/myapp/protocol/openid-connect/token", "myapp"},
		{"/identity/v1/realms/test-realm/protocol/openid-connect/token", "test-realm"},
	}

	for _, tc := range testCases {
		params := pm.ExtractParams(tc.path)
		if params["realm"] != tc.expectedRealm {
			t.Errorf("Path %s: Expected realm=%s, got %s", tc.path, tc.expectedRealm, params["realm"])
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// HELPER: Criar MultiRouteProxy para testes
// ════════════════════════════════════════════════════════════════════════════════

func newTestProxy(routes []RouteConfig) *MultiRouteProxy {
	config := Config{
		Stage:      "$default",
		AccountID:  "123456789012",
		ApiID:      "test-api",
		ListenAddr: ":3000",
		Debug:      false,
		Routes:     routes,
	}
	return NewMultiRouteProxy(config)
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO MULTI-ROUTE PROXY
// ════════════════════════════════════════════════════════════════════════════════

func TestMultiRouteProxy_RouteMatching(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/identity", RouteTemplate: "/identity/{proxy+}", LambdaURL: "http://localhost:9001", Name: "identity"},
		{PathPrefix: "/admin", RouteTemplate: "/admin/{proxy+}", LambdaURL: "http://localhost:9002", Name: "admin"},
		{PathPrefix: "/openid", RouteTemplate: "/openid/{proxy+}", LambdaURL: "http://localhost:9003", Name: "openid"},
		{PathPrefix: "/", RouteTemplate: "/{proxy+}", LambdaURL: "http://localhost:9000", Name: "default"},
	})

	testCases := []struct {
		path         string
		expectedName string
	}{
		{"/identity/v1/realms/master/token", "identity"},
		{"/identity/anything", "identity"},
		{"/admin/users", "admin"},
		{"/admin/v1/roles", "admin"},
		{"/openid/.well-known/openid-configuration", "openid"},
		{"/openid/jwks", "openid"},
		{"/other/path", "default"},
		{"/", "default"},
	}

	for _, tc := range testCases {
		handler := proxy.findRoute(tc.path)
		if handler == nil {
			t.Errorf("Path %s: Expected route %s, got nil", tc.path, tc.expectedName)
			continue
		}
		if handler.route.Name != tc.expectedName {
			t.Errorf("Path %s: Expected route %s, got %s", tc.path, tc.expectedName, handler.route.Name)
		}
	}
}

func TestMultiRouteProxy_RouteSpecificity(t *testing.T) {
	// Rotas mais específicas devem ter prioridade
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/", RouteTemplate: "/{proxy+}", LambdaURL: "http://localhost:9000", Name: "default"},
		{PathPrefix: "/api/v1/users", RouteTemplate: "/api/v1/users/{userId}", LambdaURL: "http://localhost:9002", Name: "users"},
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9001", Name: "api"},
	})

	testCases := []struct {
		path         string
		expectedName string
	}{
		{"/api/v1/users/123", "users"}, // Mais específico
		{"/api/v1/orders", "api"},      // Match geral /api
		{"/api/health", "api"},         // Match geral /api
		{"/other", "default"},          // Fallback
	}

	for _, tc := range testCases {
		handler := proxy.findRoute(tc.path)
		if handler == nil {
			t.Errorf("Path %s: Expected route %s, got nil", tc.path, tc.expectedName)
			continue
		}
		if handler.route.Name != tc.expectedName {
			t.Errorf("Path %s: Expected route %s, got %s", tc.path, tc.expectedName, handler.route.Name)
		}
	}
}

func TestMultiRouteProxy_NoMatch(t *testing.T) {
	// Sem rota default
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9001", Name: "api"},
	})

	handler := proxy.findRoute("/other/path")
	if handler != nil {
		t.Errorf("Expected no match for /other/path, got %s", handler.route.Name)
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO EVENT BUILDER
// ════════════════════════════════════════════════════════════════════════════════

func TestBuildEvent_BasicRequest(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api/users", RouteTemplate: "/api/users/{userId}", LambdaURL: "http://localhost:9000", Name: "users"},
	})

	handler := proxy.findRoute("/api/users/456")

	req := httptest.NewRequest("GET", "/api/users/456?filter=active", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	event := proxy.buildEvent(req, nil, handler)

	// Verificar versão
	if event.Version != "2.0" {
		t.Errorf("Expected version=2.0, got %s", event.Version)
	}

	// Verificar raw path
	if event.RawPath != "/api/users/456" {
		t.Errorf("Expected rawPath=/api/users/456, got %s", event.RawPath)
	}

	// Verificar query string
	if event.RawQueryString != "filter=active" {
		t.Errorf("Expected rawQueryString=filter=active, got %s", event.RawQueryString)
	}

	// Verificar query parameters
	if event.QueryStringParameters["filter"] != "active" {
		t.Errorf("Expected filter=active in query params, got %s", event.QueryStringParameters["filter"])
	}

	// Verificar path parameters
	if event.PathParameters["userId"] != "456" {
		t.Errorf("Expected userId=456 in path params, got %s", event.PathParameters["userId"])
	}

	// Verificar headers lowercase
	if event.Headers["content-type"] != "application/json" {
		t.Errorf("Expected content-type header, got %v", event.Headers)
	}
	if event.Headers["authorization"] != "Bearer token123" {
		t.Errorf("Expected authorization header, got %v", event.Headers)
	}

	// Verificar request context
	if event.RequestContext.HTTP.Method != "GET" {
		t.Errorf("Expected method=GET, got %s", event.RequestContext.HTTP.Method)
	}
	if event.RequestContext.Stage != "$default" {
		t.Errorf("Expected stage=$default, got %s", event.RequestContext.Stage)
	}
	if event.RequestContext.AccountID != "123456789012" {
		t.Errorf("Expected accountId=123456789012, got %s", event.RequestContext.AccountID)
	}
}

func TestBuildEvent_WithBody(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api/users", RouteTemplate: "/api/users", LambdaURL: "http://localhost:9000", Name: "users"},
	})

	handler := proxy.findRoute("/api/users")

	bodyContent := `{"name": "John", "email": "john@example.com"}`
	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(bodyContent))
	req.Header.Set("Content-Type", "application/json")

	event := proxy.buildEvent(req, []byte(bodyContent), handler)

	// Verificar body preservado
	if event.Body != bodyContent {
		t.Errorf("Expected body=%s, got %s", bodyContent, event.Body)
	}

	// Verificar isBase64Encoded
	if event.IsBase64Encoded {
		t.Error("Expected isBase64Encoded=false")
	}
}

func TestBuildEvent_FormURLEncoded(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/token", RouteTemplate: "/token", LambdaURL: "http://localhost:9000", Name: "token"},
	})

	handler := proxy.findRoute("/token")

	bodyContent := "grant_type=client_credentials&client_id=myapp&client_secret=secret"
	req := httptest.NewRequest("POST", "/token", strings.NewReader(bodyContent))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	event := proxy.buildEvent(req, []byte(bodyContent), handler)

	// Verificar body preservado (não parseado)
	if event.Body != bodyContent {
		t.Errorf("Expected body preserved, got %s", event.Body)
	}

	// Verificar content-type header
	if event.Headers["content-type"] != "application/x-www-form-urlencoded" {
		t.Errorf("Expected content-type=application/x-www-form-urlencoded, got %s", event.Headers["content-type"])
	}
}

func TestBuildEvent_RouteKey(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api/users", RouteTemplate: "/api/users/{userId}", LambdaURL: "http://localhost:9000", Name: "users"},
	})

	handler := proxy.findRoute("/api/users/123")

	testCases := []struct {
		method           string
		expectedRouteKey string
	}{
		{"GET", "GET /api/users/{userId}"},
		{"POST", "POST /api/users/{userId}"},
		{"PUT", "PUT /api/users/{userId}"},
		{"DELETE", "DELETE /api/users/{userId}"},
		{"PATCH", "PATCH /api/users/{userId}"},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(tc.method, "/api/users/123", nil)
		event := proxy.buildEvent(req, nil, handler)

		if event.RouteKey != tc.expectedRouteKey {
			t.Errorf("Method %s: Expected routeKey=%s, got %s", tc.method, tc.expectedRouteKey, event.RouteKey)
		}
		if event.RequestContext.RouteKey != tc.expectedRouteKey {
			t.Errorf("Method %s: Expected requestContext.routeKey=%s, got %s", tc.method, tc.expectedRouteKey, event.RequestContext.RouteKey)
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO HELPER FUNCTIONS
// ════════════════════════════════════════════════════════════════════════════════

func TestExtractSourceIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 150.172.238.178")

	ip := extractSourceIP(req)

	if ip != "203.0.113.50" {
		t.Errorf("Expected first IP from X-Forwarded-For, got %s", ip)
	}
}

func TestExtractSourceIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "192.168.1.100")

	ip := extractSourceIP(req)

	if ip != "192.168.1.100" {
		t.Errorf("Expected X-Real-IP value, got %s", ip)
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	// IDs devem ser únicos
	if id1 == id2 {
		t.Error("Request IDs should be unique")
	}

	// ID deve ter formato UUID-like
	if len(id1) < 30 {
		t.Errorf("Request ID seems too short: %s", id1)
	}
}

func TestExtractPathPrefix(t *testing.T) {
	testCases := []struct {
		template string
		expected string
	}{
		{"/api/users/{userId}", "/api/users"},
		{"/identity/v1/realms/{realm}/token", "/identity/v1/realms"},
		{"/{proxy+}", "/"},
		{"/api/{proxy+}", "/api"},
		{"/health", "/health"},
		{"/", "/"},
	}

	for _, tc := range testCases {
		result := extractPathPrefix(tc.template)
		if result != tc.expected {
			t.Errorf("Template %s: Expected prefix=%s, got %s", tc.template, tc.expected, result)
		}
	}
}

func TestLoadRoutesFromJSON(t *testing.T) {
	jsonStr := `{
		"routes": [
			{"pathPrefix": "/identity", "routeTemplate": "/identity/{proxy+}", "lambdaUrl": "http://localhost:9001", "name": "identity"},
			{"pathPrefix": "/admin", "routeTemplate": "/admin/{proxy+}", "lambdaUrl": "http://localhost:9002", "name": "admin"}
		]
	}`

	routes, err := loadRoutesFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(routes) != 2 {
		t.Errorf("Expected 2 routes, got %d", len(routes))
	}

	if routes[0].Name != "identity" {
		t.Errorf("Expected first route name=identity, got %s", routes[0].Name)
	}

	if routes[1].LambdaURL != "http://localhost:9002" {
		t.Errorf("Expected second route lambdaUrl=http://localhost:9002, got %s", routes[1].LambdaURL)
	}
}

func TestLoadRoutesFromJSON_Array(t *testing.T) {
	// Formato de array direto (sem wrapper "routes")
	jsonStr := `[
		{"pathPrefix": "/api", "routeTemplate": "/api/{proxy+}", "lambdaUrl": "http://localhost:9000", "name": "api"}
	]`

	routes, err := loadRoutesFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("Failed to parse JSON array: %v", err)
	}

	if len(routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(routes))
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTE DE INTEGRAÇÃO (MOCK LAMBDA)
// ════════════════════════════════════════════════════════════════════════════════

func TestMultiRouteProxy_Integration(t *testing.T) {
	// Criar mock Lambda server
	mockLambda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verificar que é POST
		if r.Method != "POST" {
			t.Errorf("Expected POST to Lambda, got %s", r.Method)
		}

		// Verificar content-type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Decodificar evento
		var event APIGatewayV2HTTPRequest
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("Failed to decode event: %v", err)
		}

		// Verificar evento
		if event.Version != "2.0" {
			t.Errorf("Expected version=2.0, got %s", event.Version)
		}

		// Retornar resposta Lambda
		resp := LambdaResponse{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"message": "success"}`,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockLambda.Close()

	// Configurar proxy com múltiplas rotas
	config := Config{
		Stage:      "$default",
		AccountID:  "123456789012",
		ApiID:      "test-api",
		ListenAddr: ":3000",
		Debug:      false,
		Routes: []RouteConfig{
			{PathPrefix: "/api/test", RouteTemplate: "/api/test", LambdaURL: mockLambda.URL, Name: "test"},
		},
	}

	proxy := NewMultiRouteProxy(config)

	// Criar request
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	// Executar handler
	proxy.ServeHTTP(w, req)

	// Verificar resposta
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type: application/json, got %s", w.Header().Get("Content-Type"))
	}

	if !strings.Contains(w.Body.String(), "success") {
		t.Errorf("Expected success message in body, got %s", w.Body.String())
	}
}

func TestMultiRouteProxy_MultiLambdaIntegration(t *testing.T) {
	// Criar dois mock Lambda servers diferentes
	identityLambda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := LambdaResponse{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"service": "identity"}`,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer identityLambda.Close()

	adminLambda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := LambdaResponse{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"service": "admin"}`,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer adminLambda.Close()

	// Configurar proxy com múltiplas Lambdas
	config := Config{
		Stage:      "$default",
		AccountID:  "123456789012",
		ApiID:      "test-api",
		ListenAddr: ":3000",
		Debug:      false,
		Routes: []RouteConfig{
			{PathPrefix: "/identity", RouteTemplate: "/identity/{proxy+}", LambdaURL: identityLambda.URL, Name: "identity"},
			{PathPrefix: "/admin", RouteTemplate: "/admin/{proxy+}", LambdaURL: adminLambda.URL, Name: "admin"},
		},
	}

	proxy := NewMultiRouteProxy(config)

	// Testar rota /identity
	req1 := httptest.NewRequest("GET", "/identity/v1/token", nil)
	w1 := httptest.NewRecorder()
	proxy.ServeHTTP(w1, req1)

	if !strings.Contains(w1.Body.String(), `"service": "identity"`) {
		t.Errorf("Expected identity service response, got %s", w1.Body.String())
	}

	// Testar rota /admin
	req2 := httptest.NewRequest("GET", "/admin/users", nil)
	w2 := httptest.NewRecorder()
	proxy.ServeHTTP(w2, req2)

	if !strings.Contains(w2.Body.String(), `"service": "admin"`) {
		t.Errorf("Expected admin service response, got %s", w2.Body.String())
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// HELPER: Gerar JWT de teste (sem assinatura válida — apenas para testes)
// ════════════════════════════════════════════════════════════════════════════════

func buildTestJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO JWT CLAIMS EXTRACTION
// ════════════════════════════════════════════════════════════════════════════════

func TestExtractJWTClaims_ValidBearerToken(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"sub":   "user1",
		"iss":   "https://auth.example.com",
		"exp":   9999999999,
		"roles": []string{"Admin", "RealmAdmin"},
	})

	authorizer := extractJWTClaims("Bearer " + token)

	if authorizer == nil {
		t.Fatal("Expected authorizer, got nil")
	}
	if authorizer.JWT == nil {
		t.Fatal("Expected JWT, got nil")
	}

	claims := authorizer.JWT.Claims

	if claims["sub"] != "user1" {
		t.Errorf("Expected sub=user1, got %s", claims["sub"])
	}
	if claims["iss"] != "https://auth.example.com" {
		t.Errorf("Expected iss=https://auth.example.com, got %s", claims["iss"])
	}
	// exp é number → deve ser serializado como JSON
	if claims["exp"] != "9999999999" {
		t.Errorf("Expected exp=9999999999, got %s", claims["exp"])
	}
	// roles é array → deve ser serializado como JSON string
	if claims["roles"] != `["Admin","RealmAdmin"]` {
		t.Errorf("Expected roles as JSON array string, got %s", claims["roles"])
	}
}

func TestExtractJWTClaims_NoBearerPrefix(t *testing.T) {
	authorizer := extractJWTClaims("Basic abc123")

	if authorizer != nil {
		t.Error("Expected nil for non-Bearer auth, got authorizer")
	}
}

func TestExtractJWTClaims_EmptyHeader(t *testing.T) {
	authorizer := extractJWTClaims("")

	if authorizer != nil {
		t.Error("Expected nil for empty header, got authorizer")
	}
}

func TestExtractJWTClaims_InvalidJWTFormat(t *testing.T) {
	// Apenas 2 segmentos (falta a assinatura)
	authorizer := extractJWTClaims("Bearer header.payload")

	if authorizer != nil {
		t.Error("Expected nil for invalid JWT format (2 segments), got authorizer")
	}
}

func TestExtractJWTClaims_InvalidBase64(t *testing.T) {
	// Payload com base64 inválido
	authorizer := extractJWTClaims("Bearer aaa.!!!invalid-base64!!!.ccc")

	if authorizer != nil {
		t.Error("Expected nil for invalid base64 payload, got authorizer")
	}
}

func TestExtractJWTClaims_InvalidJSON(t *testing.T) {
	// Payload decodifica mas não é JSON válido
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("this is not json"))
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))

	authorizer := extractJWTClaims("Bearer " + header + "." + notJSON + "." + sig)

	if authorizer != nil {
		t.Error("Expected nil for invalid JSON payload, got authorizer")
	}
}

func TestExtractJWTClaims_WithScopes(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"sub":   "user1",
		"scope": "openid profile email",
	})

	authorizer := extractJWTClaims("Bearer " + token)

	if authorizer == nil || authorizer.JWT == nil {
		t.Fatal("Expected authorizer with JWT, got nil")
	}

	scopes := authorizer.JWT.Scopes
	expectedScopes := []string{"openid", "profile", "email"}

	if len(scopes) != len(expectedScopes) {
		t.Fatalf("Expected %d scopes, got %d: %v", len(expectedScopes), len(scopes), scopes)
	}
	for i, s := range expectedScopes {
		if scopes[i] != s {
			t.Errorf("Expected scope[%d]=%s, got %s", i, s, scopes[i])
		}
	}
}

func TestExtractJWTClaims_WithScpClaim(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"sub": "user1",
		"scp": "read write",
	})

	authorizer := extractJWTClaims("Bearer " + token)

	if authorizer == nil || authorizer.JWT == nil {
		t.Fatal("Expected authorizer with JWT, got nil")
	}

	scopes := authorizer.JWT.Scopes
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("Expected scopes [read, write], got %v", scopes)
	}
}

func TestExtractJWTClaims_CaseInsensitiveBearer(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"sub": "user1",
	})

	// Testar lowercase "bearer"
	authorizer := extractJWTClaims("bearer " + token)
	if authorizer == nil {
		t.Error("Expected authorizer for lowercase 'bearer', got nil")
	}

	// Testar mixed case "BEARER"
	authorizer = extractJWTClaims("BEARER " + token)
	if authorizer == nil {
		t.Error("Expected authorizer for uppercase 'BEARER', got nil")
	}

	// Testar mixed case "BeArEr"
	authorizer = extractJWTClaims("BeArEr " + token)
	if authorizer == nil {
		t.Error("Expected authorizer for mixed case 'BeArEr', got nil")
	}
}

func TestExtractJWTClaims_NumericAndBoolClaims(t *testing.T) {
	token := buildTestJWT(map[string]interface{}{
		"sub":            "user1",
		"exp":            1234567890,
		"iat":            1234567800,
		"email_verified": true,
		"login_count":    42,
	})

	authorizer := extractJWTClaims("Bearer " + token)

	if authorizer == nil || authorizer.JWT == nil {
		t.Fatal("Expected authorizer with JWT, got nil")
	}

	claims := authorizer.JWT.Claims

	// Numbers → string
	if claims["exp"] != "1234567890" {
		t.Errorf("Expected exp=1234567890, got %s", claims["exp"])
	}
	if claims["iat"] != "1234567800" {
		t.Errorf("Expected iat=1234567800, got %s", claims["iat"])
	}
	// Bool → string
	if claims["email_verified"] != "true" {
		t.Errorf("Expected email_verified=true, got %s", claims["email_verified"])
	}
	if claims["login_count"] != "42" {
		t.Errorf("Expected login_count=42, got %s", claims["login_count"])
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DE INTEGRAÇÃO DO JWT NO BUILD EVENT
// ════════════════════════════════════════════════════════════════════════════════

func TestBuildEvent_WithAuthorizationHeader(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9000", Name: "api"},
	})
	handler := proxy.findRoute("/api/test")

	token := buildTestJWT(map[string]interface{}{
		"sub":   "user123",
		"scope": "openid profile",
		"iss":   "https://auth.example.com",
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	event := proxy.buildEvent(req, nil, handler)

	if event.RequestContext.Authorizer == nil {
		t.Fatal("Expected Authorizer in requestContext, got nil")
	}
	if event.RequestContext.Authorizer.JWT == nil {
		t.Fatal("Expected JWT in Authorizer, got nil")
	}

	claims := event.RequestContext.Authorizer.JWT.Claims
	if claims["sub"] != "user123" {
		t.Errorf("Expected sub=user123, got %s", claims["sub"])
	}
	if claims["iss"] != "https://auth.example.com" {
		t.Errorf("Expected iss=https://auth.example.com, got %s", claims["iss"])
	}

	scopes := event.RequestContext.Authorizer.JWT.Scopes
	if len(scopes) != 2 || scopes[0] != "openid" || scopes[1] != "profile" {
		t.Errorf("Expected scopes [openid, profile], got %v", scopes)
	}
}

func TestBuildEvent_WithoutAuthorizationHeader(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9000", Name: "api"},
	})
	handler := proxy.findRoute("/api/test")

	req := httptest.NewRequest("GET", "/api/test", nil)
	// Sem header Authorization

	event := proxy.buildEvent(req, nil, handler)

	if event.RequestContext.Authorizer != nil {
		t.Error("Expected nil Authorizer when no Authorization header, got non-nil")
	}
}

func TestBuildEvent_WithInvalidToken(t *testing.T) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9000", Name: "api"},
	})
	handler := proxy.findRoute("/api/test")

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid")

	event := proxy.buildEvent(req, nil, handler)

	if event.RequestContext.Authorizer != nil {
		t.Error("Expected nil Authorizer for invalid token, got non-nil")
	}
}

// ════════════════════════════════════════════════════════════════════════════════
// BENCHMARK
// ════════════════════════════════════════════════════════════════════════════════

func BenchmarkBuildEvent(b *testing.B) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/identity/v1/realms", RouteTemplate: "/identity/v1/realms/{realm}/protocol/openid-connect/token", LambdaURL: "http://localhost:9000", Name: "identity"},
	})
	handler := proxy.findRoute("/identity/v1/realms/master/protocol/openid-connect/token")
	body := []byte("grant_type=client_credentials&client_id=myapp")

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/identity/v1/realms/master/protocol/openid-connect/token", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
		proxy.buildEvent(req, body, handler)
	}
}

func BenchmarkPathMatcher_ExtractParams(b *testing.B) {
	pm := NewPathMatcher("/identity/v1/realms/{realm}/protocol/openid-connect/{action}")
	path := "/identity/v1/realms/master/protocol/openid-connect/token"

	for i := 0; i < b.N; i++ {
		pm.ExtractParams(path)
	}
}

func BenchmarkMultiRouteProxy_RouteMatching(b *testing.B) {
	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/identity", RouteTemplate: "/identity/{proxy+}", LambdaURL: "http://localhost:9001", Name: "identity"},
		{PathPrefix: "/admin", RouteTemplate: "/admin/{proxy+}", LambdaURL: "http://localhost:9002", Name: "admin"},
		{PathPrefix: "/openid", RouteTemplate: "/openid/{proxy+}", LambdaURL: "http://localhost:9003", Name: "openid"},
		{PathPrefix: "/api", RouteTemplate: "/api/{proxy+}", LambdaURL: "http://localhost:9004", Name: "api"},
		{PathPrefix: "/", RouteTemplate: "/{proxy+}", LambdaURL: "http://localhost:9000", Name: "default"},
	})

	paths := []string{
		"/identity/v1/token",
		"/admin/users",
		"/openid/.well-known",
		"/api/v1/orders",
		"/other/path",
	}

	for i := 0; i < b.N; i++ {
		proxy.findRoute(paths[i%len(paths)])
	}
}

func BenchmarkExtractJWTClaims(b *testing.B) {
	token := buildTestJWT(map[string]interface{}{
		"sub":   "user1",
		"iss":   "https://auth.example.com",
		"exp":   9999999999,
		"iat":   1234567890,
		"scope": "openid profile email",
		"roles": []string{"Admin", "RealmAdmin"},
	})

	header := "Bearer " + token

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractJWTClaims(header)
	}
}
