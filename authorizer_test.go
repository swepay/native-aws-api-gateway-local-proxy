package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════════
// TESTES DO LAMBDA AUTHORIZER (native-guard-backend#120)
// ════════════════════════════════════════════════════════════════════════════════

// newAuthorizerMock creates an httptest.Server that mimics a REQUEST-type Lambda
// Authorizer (simple response format) and lets the test assert on the request
// it received before deciding the response.
func newAuthorizerMock(t *testing.T, assertRequest func(req AuthorizerInvokeRequest), isAuthorized bool, context map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST to authorizer, got %s", r.Method)
		}
		var req AuthorizerInvokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode authorizer request: %v", err)
		}
		if assertRequest != nil {
			assertRequest(req)
		}

		resp := AuthorizerInvokeResponse{IsAuthorized: isAuthorized, Context: context}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestAuthorizer_Allowed_InjectsLambdaContextIntoDownstreamEvent(t *testing.T) {
	var sawAuthorizerLambdaContext map[string]interface{}
	var downstreamCalls int32

	authorizerMock := newAuthorizerMock(t, func(req AuthorizerInvokeRequest) {
		if req.Version != "2.0" || req.Type != "REQUEST" {
			t.Errorf("Expected version=2.0 type=REQUEST, got version=%s type=%s", req.Version, req.Type)
		}
		if len(req.IdentitySource) != 1 || req.IdentitySource[0] != "Bearer opaque-token-123" {
			t.Errorf("Expected identitySource=[Bearer opaque-token-123], got %v", req.IdentitySource)
		}
		if req.RawPath != "/admin/v1/realms" {
			t.Errorf("Expected rawPath=/admin/v1/realms, got %s", req.RawPath)
		}
	}, true, map[string]interface{}{
		"realmId": "master",
		"sub":     "root-user",
		"roles":   "platform-admin",
	})
	defer authorizerMock.Close()

	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalls, 1)

		var event APIGatewayV2HTTPRequest
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("Failed to decode downstream event: %v", err)
		}
		if event.RequestContext.Authorizer == nil || event.RequestContext.Authorizer.Lambda == nil {
			t.Fatal("Expected requestContext.authorizer.lambda to be populated")
		}
		sawAuthorizerLambdaContext = event.RequestContext.Authorizer.Lambda
		if event.RequestContext.Authorizer.JWT != nil {
			t.Error("Expected requestContext.authorizer.jwt to stay nil for an authorizerUrl route")
		}

		resp := LambdaResponse{StatusCode: 200, Body: `{"ok":true}`}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer downstreamMock.Close()

	proxy := newTestProxy([]RouteConfig{
		{
			PathPrefix:    "/admin",
			RouteTemplate: "/admin/{proxy+}",
			LambdaURL:     downstreamMock.URL,
			AuthorizerURL: authorizerMock.URL,
			Name:          "admin",
		},
	})

	req := httptest.NewRequest("POST", "/admin/v1/realms", nil)
	req.Header.Set("Authorization", "Bearer opaque-token-123")
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&downstreamCalls) != 1 {
		t.Fatalf("Expected downstream Lambda to be invoked exactly once, got %d", downstreamCalls)
	}
	if sawAuthorizerLambdaContext["realmId"] != "master" {
		t.Errorf("Expected realmId=master in requestContext.authorizer.lambda, got %v", sawAuthorizerLambdaContext["realmId"])
	}
}

// TestAuthorizer_NoAuthorizationHeader_Returns401WithoutInvokingAuthorizer is a
// regression test for AWS's actual Lambda Authorizer contract: API Gateway
// never invokes a REQUEST authorizer when none of its configured identity
// sources (here, the Authorization header) are present on the request — it
// short-circuits with 401 before any Lambda runs. Conflating this with the
// deny path (403) breaks every "no Bearer token" test written against real
// AWS semantics (e.g. native-guard-backend's AdminAuthorizationCodeIssuanceTests).
func TestAuthorizer_NoAuthorizationHeader_Returns401WithoutInvokingAuthorizer(t *testing.T) {
	var authorizerCalls, downstreamCalls int32

	authorizerMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authorizerCalls, 1)
		w.WriteHeader(200)
	}))
	defer authorizerMock.Close()

	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalls, 1)
		w.WriteHeader(200)
	}))
	defer downstreamMock.Close()

	proxy := newTestProxy([]RouteConfig{
		{
			PathPrefix:    "/admin",
			RouteTemplate: "/admin/{proxy+}",
			LambdaURL:     downstreamMock.URL,
			AuthorizerURL: authorizerMock.URL,
			Name:          "admin",
		},
	})

	req := httptest.NewRequest("POST", "/admin/v1/realms/x/authorization-codes", nil)
	// Deliberately no Authorization header.
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Unauthorized") {
		t.Errorf("Expected body to mention Unauthorized, got %s", w.Body.String())
	}
	if atomic.LoadInt32(&authorizerCalls) != 0 {
		t.Errorf("Expected the authorizer Lambda NOT to be invoked without an Authorization header, got %d call(s)", authorizerCalls)
	}
	if atomic.LoadInt32(&downstreamCalls) != 0 {
		t.Errorf("Expected downstream Lambda NOT to be invoked without an Authorization header, got %d call(s)", downstreamCalls)
	}
}

func TestAuthorizer_Denied_DoesNotInvokeDownstreamLambda(t *testing.T) {
	var downstreamCalls int32

	authorizerMock := newAuthorizerMock(t, nil, false, nil)
	defer authorizerMock.Close()

	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalls, 1)
		w.WriteHeader(200)
	}))
	defer downstreamMock.Close()

	proxy := newTestProxy([]RouteConfig{
		{
			PathPrefix:    "/admin",
			RouteTemplate: "/admin/{proxy+}",
			LambdaURL:     downstreamMock.URL,
			AuthorizerURL: authorizerMock.URL,
			Name:          "admin",
		},
	})

	req := httptest.NewRequest("POST", "/admin/v1/realms", nil)
	req.Header.Set("Authorization", "Bearer some-other-realm-token")
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Forbidden") {
		t.Errorf("Expected body to mention Forbidden, got %s", w.Body.String())
	}
	if atomic.LoadInt32(&downstreamCalls) != 0 {
		t.Errorf("Expected downstream Lambda NOT to be invoked on deny, got %d call(s)", downstreamCalls)
	}
}

func TestAuthorizer_Unavailable_Returns500AndSkipsDownstream(t *testing.T) {
	var downstreamCalls int32

	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalls, 1)
		w.WriteHeader(200)
	}))
	defer downstreamMock.Close()

	// Point AuthorizerURL at a server that is immediately closed, so the
	// connection is refused — simulates "lambda-authorizer container down".
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	authorizerURL := unreachable.URL
	unreachable.Close()

	proxy := newTestProxy([]RouteConfig{
		{
			PathPrefix:    "/admin",
			RouteTemplate: "/admin/{proxy+}",
			LambdaURL:     downstreamMock.URL,
			AuthorizerURL: authorizerURL,
			Name:          "admin",
		},
	})

	req := httptest.NewRequest("POST", "/admin/v1/realms", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Errorf("Expected body to mention Internal Server Error, got %s", w.Body.String())
	}
	if atomic.LoadInt32(&downstreamCalls) != 0 {
		t.Errorf("Expected downstream Lambda NOT to be invoked when authorizer is unavailable, got %d call(s)", downstreamCalls)
	}
}

func TestAuthorizer_MalformedResponse_Returns500(t *testing.T) {
	var downstreamCalls int32

	authorizerMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not json"))
	}))
	defer authorizerMock.Close()

	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalls, 1)
		w.WriteHeader(200)
	}))
	defer downstreamMock.Close()

	proxy := newTestProxy([]RouteConfig{
		{
			PathPrefix:    "/admin",
			RouteTemplate: "/admin/{proxy+}",
			LambdaURL:     downstreamMock.URL,
			AuthorizerURL: authorizerMock.URL,
			Name:          "admin",
		},
	})

	req := httptest.NewRequest("GET", "/admin/v1/realms", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for a malformed authorizer response, got %d", w.Code)
	}
	if atomic.LoadInt32(&downstreamCalls) != 0 {
		t.Errorf("Expected downstream Lambda NOT to be invoked for a malformed authorizer response, got %d call(s)", downstreamCalls)
	}
}

// TestAuthorizer_RouteWithoutAuthorizerURL_KeepsLegacyInlineJwtBehavior is a
// regression test for backward compatibility: routes that don't set
// AuthorizerURL (every other product's docker-compose today) must keep
// decoding the JWT inline, never calling any authorizer endpoint.
func TestAuthorizer_RouteWithoutAuthorizerURL_KeepsLegacyInlineJwtBehavior(t *testing.T) {
	downstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event APIGatewayV2HTTPRequest
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("Failed to decode downstream event: %v", err)
		}
		if event.RequestContext.Authorizer == nil || event.RequestContext.Authorizer.JWT == nil {
			t.Fatal("Expected requestContext.authorizer.jwt to be populated by the legacy inline decode")
		}
		if event.RequestContext.Authorizer.Lambda != nil {
			t.Error("Expected requestContext.authorizer.lambda to stay nil without an AuthorizerURL")
		}
		if event.RequestContext.Authorizer.JWT.Claims["sub"] != "legacy-user" {
			t.Errorf("Expected sub=legacy-user, got %s", event.RequestContext.Authorizer.JWT.Claims["sub"])
		}

		resp := LambdaResponse{StatusCode: 200, Body: `{"ok":true}`}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer downstreamMock.Close()

	proxy := newTestProxy([]RouteConfig{
		{PathPrefix: "/identity", RouteTemplate: "/identity/{proxy+}", LambdaURL: downstreamMock.URL, Name: "identity"},
	})

	token := buildTestJWT(map[string]interface{}{"sub": "legacy-user"})
	req := httptest.NewRequest("GET", "/identity/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestBuildRouteArn(t *testing.T) {
	cfg := Config{Region: "sa-east-1", AccountID: "123456789012", ApiID: "test-api", Stage: "hml"}
	event := APIGatewayV2HTTPRequest{
		RawPath: "/admin/v1/realms",
		RequestContext: APIGatewayV2HTTPRequestContext{
			HTTP: APIGatewayV2HTTPRequestContextHTTP{Method: "POST"},
		},
	}

	got := buildRouteArn(cfg, event)
	want := "arn:aws:execute-api:sa-east-1:123456789012:test-api/hml/POST/admin/v1/realms"
	if got != want {
		t.Errorf("Expected routeArn=%s, got %s", want, got)
	}
}
