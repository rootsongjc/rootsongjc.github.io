package httpcontext

import (
	"cmp"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
)

// HTTPContext corresponds to each gRPC stream, i.e. each request.
//
// This also must behave as a wrapper around proxy-wasm's host calls.
type HTTPContext struct {
	// tx is the transaction associated with the current HTTP request.
	tx ctypes.Transaction
	// TODO: add comments on wtf this is
	httpProtocol string
	// requestBodyProcessed is set to true if the request body has been processed. The processing can happen in the
	// request body phase or in the response headers phase depending on the rules.
	requestBodyProcessed bool
	// reqBody and resBody are the request and response bodies respectively. Set when they are available.
	// To reduce the memory footprint, reqBody will be set to nil after the request body is processed.
	reqBody, resBody []byte
	// stringAttributes stores the available string attributes.
	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes#arch-overview-attributes
	stringAttributes map[string]string
	// intAttributes stores the available int attributes.
	intAttributes map[string]int

	// These are the special headers that are set in the request/response headers phase.
	// They are exposed for testing purposes.
	AuthorityHeader, MethodHeader, PathHeader,
	StatusHeader string
}

// NewHTTPContext creates a new HTTPContext.
func NewHTTPContext(waf coraza.WAF) *HTTPContext {
	return &HTTPContext{tx: waf.NewTransaction()}
}

// SetRequestHeader sets a request header.
func (ctx *HTTPContext) SetRequestHeader(key, value string) {
	ctx.tx.AddRequestHeader(key, value)
	switch key {
	case ":authority":
		ctx.AuthorityHeader = value
	case ":method":
		ctx.MethodHeader = value
	case ":path":
		ctx.PathHeader = value
	}
}

// SetResponseHeader sets a response header.
func (ctx *HTTPContext) SetResponseHeader(key, value string) {
	ctx.tx.AddResponseHeader(key, value)
	if key == ":status" {
		ctx.StatusHeader = value
	}
}

// SetReqBody sets the request headers.
func (ctx *HTTPContext) SetReqBody(body []byte) {
	ctx.reqBody = body
}

// SetResBody sets the response headers.
func (ctx *HTTPContext) SetResBody(body []byte) {
	ctx.resBody = body
}

// SetStringAttribute sets the attributes.
func (ctx *HTTPContext) SetStringAttribute(path, value string) {
	if ctx.stringAttributes == nil {
		ctx.stringAttributes = make(map[string]string)
	}
	ctx.stringAttributes[path] = value
}

// GetStringAttribute returns the string attribute at the given path.
//
// This is exported for testing purposes.
func (ctx *HTTPContext) GetStringAttribute(path string) (string, error) {
	if val, ok := ctx.stringAttributes[path]; ok {
		return val, nil
	}
	return "", fmt.Errorf("attribute %q not found", path)
}

// SetIntAttribute sets the attributes.
func (ctx *HTTPContext) SetIntAttribute(path string, value int) {
	if ctx.intAttributes == nil {
		ctx.intAttributes = make(map[string]int)
	}
	ctx.intAttributes[path] = value
}

// GetIntAttribute returns the int attribute at the given path.
//
// This is exported for testing purposes.
func (ctx *HTTPContext) GetIntAttribute(path string) (int, error) {
	if val, ok := ctx.intAttributes[path]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("attribute %q not found", path)
}

// Transaction returns the transaction associated with the current HTTP request.
func (ctx *HTTPContext) Transaction() ctypes.Transaction {
	return ctx.tx
}

// OnRequestHeaders is called when the request headers are received. Returns the immediate response status code if interrupted.
func (ctx *HTTPContext) OnRequestHeaders() (immediateResponseStatus int, err error) {
	authority := ctx.AuthorityHeader
	if authority == "" {
		var err error
		authority, err = ctx.GetStringAttribute("request.host")
		if err != nil {
			// TODO: investigate why :authority ends up being empty.
			return 0, fmt.Errorf("failed to get authority: %w", err)
		}
	}

	// CRS rules tend to expect Host even with HTTP/2
	ctx.tx.AddRequestHeader("Host", authority)
	ctx.tx.SetServerName(parseServerName(authority))

	tx := ctx.tx
	if tx.IsRuleEngineOff() {
		return
	}

	srcIP, srcPort, err := ctx.retrieveSourceAddressInfo()
	if err != nil {
		return
	}

	// The values for "server" are dummies as we don't have the destination address info.
	tx.ProcessConnection(srcIP, srcPort, "127.0.0.1", 80)

	method := ctx.MethodHeader
	if method == "" {
		method, err = ctx.GetStringAttribute("request.method")
		if err != nil {
			return
		}
	}

	uri := authority
	if method != http.MethodConnect { // CONNECT requests does not have a path, see https://httpwg.org/specs/rfc9110#CONNECT
		// Note the pseudo-header :path includes the query.
		// See https://httpwg.org/specs/rfc9113.html#rfc.section.8.3.1
		uri = ctx.PathHeader
		if uri == "" {
			uri, err = ctx.GetStringAttribute("request.path")
			if err != nil {
				return
			}
		}
	}

	// This currently relies on Envoy's behavior of mapping all requests to HTTP/2 semantics
	// and its request properties, but they may not be true of other proxies implementing
	// proxy-wasm.
	ctx.httpProtocol, _ = ctx.GetStringAttribute("request.protocol")
	if ctx.httpProtocol == "" {
		// TODO(anuraaga): HTTP protocol is commonly required in WAF rules, we should probably
		// fail fast here, but proxytest does not support properties yet.
		ctx.httpProtocol = "HTTP/2.0"
	}

	tx.ProcessURI(uri, method, ctx.httpProtocol)

	interruption := tx.ProcessRequestHeaders()
	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}
	if !tx.IsRequestBodyAccessible() {
		ctx.requestBodyProcessed = true

		// ProcessRequestBody is still performed for phase 2 rules, checking already populated variables.
		interruption, err = tx.ProcessRequestBody()
		if err != nil {
			return
		}

		if interruption != nil {
			return getInterruptionStatusOrDefault(interruption), nil
		}
	}
	return
}

// OnRequestBody is called when the request body is received. Returns the immediate response status code if interrupted.
func (ctx *HTTPContext) OnRequestBody() (immediateResponseStatus int, err error) {
	tx := ctx.tx
	if !tx.IsRequestBodyAccessible() || ctx.requestBodyProcessed {
		panic("BUG: OnRequestBody should not be called if request body is not accessible")
	}

	if tx.IsRuleEngineOff() {
		return
	}

	interruption, _, err := tx.WriteRequestBody(ctx.reqBody)
	if err != nil {
		return 0, err
	}
	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}

	ctx.requestBodyProcessed = true

	interruption, err = tx.ProcessRequestBody()
	if err != nil {
		return
	}
	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}
	return
}

// OnResponseHeaders is called when the response headers are received.  Returns the immediate response status code if interrupted.
func (ctx *HTTPContext) OnResponseHeaders(endOfStream bool) (immediateResponseStatus int, err error) {
	tx := ctx.tx
	if tx.IsRuleEngineOff() {
		return
	}

	// Requests without body won't call OnHttpRequestBody, but there are rules in the request body
	// phase that still need to be executed. If they haven't been executed yet, now is the time.
	if !ctx.requestBodyProcessed {
		ctx.requestBodyProcessed = true
		var interruption *ctypes.Interruption
		interruption, err = tx.ProcessRequestBody()
		if err != nil {
			return
		}
		if interruption != nil {
			return getInterruptionStatusOrDefault(interruption), nil
		}
	}

	var code int
	if status := ctx.StatusHeader; status != "" {
		code, err = strconv.Atoi(status)
		if err != nil {
			return
		}
	} else {
		code, err = ctx.GetIntAttribute("response.code")
		if err != nil {
			return
		}
	}

	interruption := tx.ProcessResponseHeaders(code, ctx.httpProtocol)
	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}

	if endOfStream || !(tx.IsResponseBodyAccessible() && tx.IsResponseBodyProcessable()) {
		// ProcessRequestBody is still performed for phase 4 rules, checking already populated variables.
		// Notably, if early CRS early blocking feature is disabled, only at phase 4 the outbound anomaly score is calculated
		// and the interruption is eventually raised.
		interruption, err = tx.ProcessResponseBody()
		if err != nil {
			return
		}
		if interruption != nil {
			return getInterruptionStatusOrDefault(interruption), nil
		}
	}
	return
}

// OnResponseBody is called when the response body is received. Returns the immediate response status code if interrupted.
func (ctx *HTTPContext) OnResponseBody() (immediateResponseStatus int, err error) {
	tx := ctx.tx
	if !(tx.IsResponseBodyAccessible() && tx.IsResponseBodyProcessable()) {
		panic("BUG: OnResponseBody should not be called if request body is not accessible")
	}

	if tx.IsRuleEngineOff() {
		return
	}

	interruption, _, err := tx.WriteResponseBody(ctx.resBody)
	if err != nil {
		return
	}

	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}

	interruption, err = tx.ProcessResponseBody()
	if err != nil {
		return
	}
	if interruption != nil {
		return getInterruptionStatusOrDefault(interruption), nil
	}
	return
}

// OnStreamDone is called as last step, when the response has already been sent back downstream. It is used to process log phase.
func (ctx *HTTPContext) OnStreamDone() error {
	ctx.tx.ProcessLogging()
	return ctx.tx.Close()
}

// parseServerName parses :authority pseudo-header in order to retrieve the
// virtual host.
//
// https://github.com/tetrateio/coraza-proxy-wasm/blob/6418631ba3f2b1fa5ce0865edbf534bdf2247106/wasmplugin/plugin.go#L804-L816
func parseServerName(authority string) string {
	host, _, _ := net.SplitHostPort(authority)
	return cmp.Or(host, authority)
}

// retrieveAddressInfo retrieves address properties from the proxy
// Expected targets are "source" or "destination"
// Envoy ref: https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes#connection-attributes
func (ctx *HTTPContext) retrieveSourceAddressInfo() (string, int, error) {
	targetAddressRaw, err := ctx.GetStringAttribute("source.address")
	if err != nil {
		// This should not happen, but if it does, we log it and return an error
		return "", 0, fmt.Errorf("failed to get source.address: %w", err)
	}

	// Try to parse the address - sometimes it's just an IP, sometimes it's an IP:port
	targetIP, targetPortStr, err := net.SplitHostPort(targetAddressRaw)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse source.address: %w", err)
	}

	// Ok this is an IP:port, we need to parse the port.
	targetPort, err := strconv.Atoi(targetPortStr)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse the port from source.address=\"%s\": %w", targetAddressRaw, err)
	}
	return targetIP, targetPort, nil
}

// getInterruptionStatusOrDefault returns the status code from the interruption or 403 if it's not set.
func getInterruptionStatusOrDefault(interruption *ctypes.Interruption) int {
	return cmp.Or(interruption.Status, 403)
}
