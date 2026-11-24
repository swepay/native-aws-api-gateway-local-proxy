// Lambda Authorizer client for the AWS API Gateway HTTP API v2 Local Proxy.
//
// Implements the "proxy calls the Lambda Authorizer" behavior requested in
// native-guard-backend#120: for routes with AuthorizerURL set (opt-in,
// RouteConfig.AuthorizerURL), the proxy invokes the configured Lambda — a
// REQUEST type authorizer, payload format version 2.0, simple response
// format — exactly as AWS API Gateway does for a route with
// AuthorizationType: CUSTOM, instead of decoding the JWT inline.
//
// Request/response shapes mirror:
// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-lambda-authorizer.html
//
// Routes without AuthorizerURL are untouched — extractJWTClaims in main.go
// keeps running for them, so other products consuming this proxy (which
// don't set AuthorizerURL) see no behavior change.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// AuthorizerInvokeRequest is the payload sent to a REQUEST-type Lambda
// Authorizer, payload format version 2.0. Field set intentionally mirrors
// native-guard-backend's own AuthorizerRequest.cs so the authorizer function
// sees the same shape locally as it does on real AWS API Gateway.
type AuthorizerInvokeRequest struct {
	Version               string                   `json:"version"`
	Type                  string                   `json:"type"`
	RouteArn              string                   `json:"routeArn"`
	IdentitySource        []string                 `json:"identitySource,omitempty"`
	RouteKey              string                   `json:"routeKey"`
	RawPath               string                   `json:"rawPath"`
	RawQueryString        string                   `json:"rawQueryString"`
	Headers               map[string]string        `json:"headers"`
	QueryStringParameters map[string]string        `json:"queryStringParameters,omitempty"`
	PathParameters        map[string]string        `json:"pathParameters,omitempty"`
	StageVariables        map[string]string        `json:"stageVariables,omitempty"`
	RequestContext        AuthorizerRequestContext `json:"requestContext"`
}

// AuthorizerRequestContext is the requestContext sub-object of
// AuthorizerInvokeRequest. Deliberately narrower than
// APIGatewayV2HTTPRequestContext (no authorizer/accountId duplication
// concerns) — it only carries what a REQUEST authorizer actually receives.
type AuthorizerRequestContext struct {
	AccountID    string                             `json:"accountId"`
	ApiID        string                             `json:"apiId"`
	DomainName   string                             `json:"domainName"`
	DomainPrefix string                             `json:"domainPrefix"`
	HTTP         APIGatewayV2HTTPRequestContextHTTP `json:"http"`
	RequestID    string                             `json:"requestId"`
	RouteKey     string                             `json:"routeKey"`
	Stage        string                             `json:"stage"`
	Time         string                             `json:"time"`
	TimeEpoch    int64                              `json:"timeEpoch"`
}

// AuthorizerInvokeResponse is the Lambda Authorizer simple response format
// (EnableSimpleResponses: true). Context values can be string, number or
// boolean per AWS docs, hence map[string]interface{} rather than
// map[string]string — unmarshaling preserves whatever JSON type the
// authorizer returned, same as what NativeLambdaRouter would see from a real
// API Gateway requestContext.authorizer.lambda.
type AuthorizerInvokeResponse struct {
	IsAuthorized bool                   `json:"isAuthorized"`
	Context      map[string]interface{} `json:"context,omitempty"`
}

// authorize invokes handler.route.AuthorizerURL for the given request/event,
// writes the appropriate HTTP response and returns false when the caller
// must stop (no identity source, denied, or authorizer unavailable/malformed)
// — mirroring how API Gateway itself never reaches the downstream Lambda in
// those cases. On success, it injects the authorizer's context into event's
// requestContext.authorizer.lambda (what NativeLambdaRouter reads as a
// fallback to requestContext.authorizer.jwt) and returns true.
func (p *MultiRouteProxy) authorize(w http.ResponseWriter, handler *RouteHandler, event *APIGatewayV2HTTPRequest) bool {
	// API Gateway never even invokes a REQUEST Lambda Authorizer when none of
	// its configured identity sources are present on the request — it
	// short-circuits with 401 Unauthorized before any Lambda runs. Our only
	// identity source, mirroring the authorizer's AWS::ApiGatewayV2::Authorizer
	// config (IdentitySource: ["$request.header.Authorization"]), is the
	// Authorization header. Skipping this check would turn "no token at all"
	// into a 403 (the deny path below), which is wrong on real AWS and breaks
	// any test asserting 401 for an anonymous caller.
	if authHeader, ok := event.Headers["authorization"]; !ok || authHeader == "" {
		if p.config.Debug {
			log.Printf("🔒 No identitySource (Authorization header) [%s]: %s %s\n", handler.route.Name, event.RequestContext.HTTP.Method, event.RawPath)
		}
		writeAuthorizerResult(w, http.StatusUnauthorized, "Unauthorized")
		return false
	}

	authResp, err := p.invokeAuthorizer(handler.route.AuthorizerURL, *event)
	if err != nil {
		log.Printf("❌ Authorizer invocation failed [%s]: %v", handler.route.Name, err)
		// API Gateway returns 500 both when the Lambda Authorizer errors/times
		// out and when it returns a malformed response — it never guesses.
		writeAuthorizerResult(w, http.StatusInternalServerError, "Internal Server Error")
		return false
	}

	if !authResp.IsAuthorized {
		if p.config.Debug {
			log.Printf("🚫 Authorizer denied [%s]: %s %s\n", handler.route.Name, event.RequestContext.HTTP.Method, event.RawPath)
		}
		writeAuthorizerResult(w, http.StatusForbidden, "Forbidden")
		return false
	}

	if p.config.Debug {
		log.Printf("✅ Authorizer allowed [%s]: %d context value(s)\n", handler.route.Name, len(authResp.Context))
	}

	event.RequestContext.Authorizer = &APIGatewayV2HTTPRequestContextAuthorizer{
		Lambda: authResp.Context,
	}
	return true
}

// invokeAuthorizer builds the AuthorizerInvokeRequest from event and POSTs it
// to authorizerURL, the same invoke-URL convention used for LambdaURL. Any
// non-nil error (network failure, non-200 HTTP, malformed JSON body) must be
// treated as "authorizer unavailable" by the caller.
func (p *MultiRouteProxy) invokeAuthorizer(authorizerURL string, event APIGatewayV2HTTPRequest) (*AuthorizerInvokeResponse, error) {
	var identitySource []string
	if authHeader, ok := event.Headers["authorization"]; ok && authHeader != "" {
		identitySource = []string{authHeader}
	}

	reqPayload := AuthorizerInvokeRequest{
		Version:               "2.0",
		Type:                  "REQUEST",
		RouteArn:              buildRouteArn(p.config, event),
		IdentitySource:        identitySource,
		RouteKey:              event.RouteKey,
		RawPath:               event.RawPath,
		RawQueryString:        event.RawQueryString,
		Headers:               event.Headers,
		QueryStringParameters: event.QueryStringParameters,
		PathParameters:        event.PathParameters,
		RequestContext: AuthorizerRequestContext{
			AccountID:    event.RequestContext.AccountID,
			ApiID:        event.RequestContext.ApiID,
			DomainName:   event.RequestContext.DomainName,
			DomainPrefix: event.RequestContext.DomainPrefix,
			HTTP:         event.RequestContext.HTTP,
			RequestID:    event.RequestContext.RequestID,
			RouteKey:     event.RequestContext.RouteKey,
			Stage:        event.RequestContext.Stage,
			Time:         event.RequestContext.Time,
			TimeEpoch:    event.RequestContext.TimeEpoch,
		},
	}

	payloadJSON, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize authorizer request: %w", err)
	}

	if p.config.Debug {
		log.Printf("🔐 INVOKING AUTHORIZER: %s\n%s\n", authorizerURL, string(payloadJSON))
	}

	httpReq, err := http.NewRequest("POST", authorizerURL, bytes.NewReader(payloadJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create authorizer request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke authorizer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read authorizer response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorizer returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var authResp AuthorizerInvokeResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return nil, fmt.Errorf("malformed authorizer response: %w", err)
	}

	return &authResp, nil
}

// buildRouteArn approximates the methodArn AWS API Gateway passes to a
// REQUEST authorizer: arn:aws:execute-api:{region}:{accountId}:{apiId}/{stage}/{method}{path}.
// Low-fidelity on purpose — native-guard-backend's authorizer (like most
// REQUEST authorizers) never inspects routeArn, it only validates the JWT —
// but a plausible value keeps the payload shape honest for anyone logging it.
func buildRouteArn(cfg Config, event APIGatewayV2HTTPRequest) string {
	return fmt.Sprintf("arn:aws:execute-api:%s:%s:%s/%s/%s%s",
		cfg.Region, cfg.AccountID, cfg.ApiID, cfg.Stage, event.RequestContext.HTTP.Method, event.RawPath)
}

// writeAuthorizerResult writes a response shaped like what API Gateway itself
// returns when it short-circuits a request because of its Lambda Authorizer
// (deny or authorizer error) — {"message": "..."} — not this proxy's own
// {"error": "..."} shape (writeError/writeJSONError), so client behavior
// written against real AWS stays correct against the local proxy too.
func writeAuthorizerResult(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
