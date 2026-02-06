package main

import (
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
// TESTES DO EVENT BUILDER
// ════════════════════════════════════════════════════════════════════════════════

func TestBuildEvent_BasicRequest(t *testing.T) {
	config := Config{
		RouteTemplate: "/api/users/{userId}",
		Stage:         "$default",
		AccountID:     "123456789012",
		ApiID:         "test-api",
	}
	
	handler := NewProxyHandler(config)
	
	req := httptest.NewRequest("GET", "/api/users/456?filter=active", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")
	
	event := handler.buildEvent(req, nil)
	
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
	config := Config{
		RouteTemplate: "/api/users",
		Stage:         "$default",
		AccountID:     "123456789012",
		ApiID:         "test-api",
	}
	
	handler := NewProxyHandler(config)
	
	bodyContent := `{"name": "John", "email": "john@example.com"}`
	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(bodyContent))
	req.Header.Set("Content-Type", "application/json")
	
	event := handler.buildEvent(req, []byte(bodyContent))
	
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
	config := Config{
		RouteTemplate: "/token",
		Stage:         "$default",
		AccountID:     "123456789012",
		ApiID:         "test-api",
	}
	
	handler := NewProxyHandler(config)
	
	bodyContent := "grant_type=client_credentials&client_id=myapp&client_secret=secret"
	req := httptest.NewRequest("POST", "/token", strings.NewReader(bodyContent))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	event := handler.buildEvent(req, []byte(bodyContent))
	
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
	config := Config{
		RouteTemplate: "/api/users/{userId}",
		Stage:         "$default",
		AccountID:     "123456789012",
		ApiID:         "test-api",
	}
	
	handler := NewProxyHandler(config)
	
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
		event := handler.buildEvent(req, nil)
		
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

// ════════════════════════════════════════════════════════════════════════════════
// TESTE DE INTEGRAÇÃO (MOCK LAMBDA)
// ════════════════════════════════════════════════════════════════════════════════

func TestProxyHandler_Integration(t *testing.T) {
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLambda.Close()
	
	// Configurar proxy
	config := Config{
		LambdaInvokeURL: mockLambda.URL,
		RouteTemplate:   "/api/test",
		Stage:           "$default",
		AccountID:       "123456789012",
		ApiID:           "test-api",
	}
	
	handler := NewProxyHandler(config)
	
	// Criar request
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	
	// Executar handler
	handler.ServeHTTP(w, req)
	
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

// ════════════════════════════════════════════════════════════════════════════════
// BENCHMARK
// ════════════════════════════════════════════════════════════════════════════════

func BenchmarkBuildEvent(b *testing.B) {
	config := Config{
		RouteTemplate: "/identity/v1/realms/{realm}/protocol/openid-connect/token",
		Stage:         "$default",
		AccountID:     "123456789012",
		ApiID:         "test-api",
	}
	
	handler := NewProxyHandler(config)
	body := []byte("grant_type=client_credentials&client_id=myapp")
	
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/identity/v1/realms/master/protocol/openid-connect/token", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
		handler.buildEvent(req, body)
	}
}

func BenchmarkPathMatcher_ExtractParams(b *testing.B) {
	pm := NewPathMatcher("/identity/v1/realms/{realm}/protocol/openid-connect/{action}")
	path := "/identity/v1/realms/master/protocol/openid-connect/token"
	
	for i := 0; i < b.N; i++ {
		pm.ExtractParams(path)
	}
}
