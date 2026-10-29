package openclawworker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
	"github.com/gorilla/websocket"
)

const enrollmentWebSocketReadLimit = 256 << 10

var enrollmentWebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 << 10,
	WriteBufferSize: 4 << 10,
	CheckOrigin:     func(*http.Request) bool { return true },
}

type enrollmentObservation struct {
	mu       sync.Mutex
	enrollID string
	binding  openclawgateway.EnrollmentBinding
	haveKey  bool
	haveReq  bool
}

func websocketUpgradeRequested(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// Experimental observer only: native admission still requires a version-pinned
// authentication adapter. Official clients carry tokens inside signed frames,
// not the HTTP bearer used by this draft entry seam. Keep rollout disabled.
func (w *Worker) proxyEnrollmentWebSocket(rw http.ResponseWriter, r *http.Request, tenant, gatewayPath string, origin *url.URL, target gatewayCellTarget, enrollmentID, ticket string) {
	rw.Header().Del("Content-Type")
	upstreamQuery := r.URL.Query()
	upstreamQuery.Del("clawhub_enrollment")
	upstreamURL := url.URL{Scheme: "ws", Host: target.Address, Path: "/" + strings.TrimPrefix(gatewayPath, "/"), RawQuery: upstreamQuery.Encode()}
	upstreamHeader := http.Header{}
	upstreamHeader.Set("X-ClawHub-Enrollment-ID", enrollmentID)
	upstreamHeader.Set("Origin", origin.String())
	upstreamHeader.Set("X-Forwarded-For", "198.18.0.1")
	upstreamHeader.Set("X-Real-IP", "198.18.0.1")
	upstreamHeader.Set("X-Forwarded-Host", origin.Host)
	upstreamHeader.Set("X-Forwarded-Proto", "https")
	upstream, resp, err := websocket.DefaultDialer.Dial(upstreamURL.String(), upstreamHeader)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		http.Error(rw, "Workspace connection unavailable", http.StatusBadGateway)
		return
	}
	verifyCtx, cancelVerify := context.WithTimeout(r.Context(), 3*time.Second)
	current, verifyErr := w.gatewayCell(verifyCtx, tenant)
	cancelVerify()
	if verifyErr != nil || current != target {
		_ = upstream.Close()
		http.Error(rw, "Workspace connection unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstream.Close() }()
	client, err := enrollmentWebSocketUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		_ = upstream.Close()
		return
	}
	defer func() { _ = client.Close() }()
	client.SetReadLimit(enrollmentWebSocketReadLimit)
	upstream.SetReadLimit(enrollmentWebSocketReadLimit)

	obs := &enrollmentObservation{enrollID: enrollmentID}
	closed := make(chan struct{})
	var closeOnce sync.Once
	closeBoth := func() { closeOnce.Do(func() { close(closed); _ = client.Close(); _ = upstream.Close() }) }
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(openclawgateway.EnrollmentTTL)
		defer timer.Stop()
		for {
			select {
			case <-closed:
				return
			case <-timer.C:
				closeBoth()
				return
			case <-ticker.C:
				checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
				current, err := w.gatewayCell(checkCtx, tenant)
				cancel()
				if err != nil || current != target {
					closeBoth()
					return
				}
			}
		}
	}()

	go func() {
		defer closeBoth()
		for {
			kind, payload, err := client.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage || kind == websocket.BinaryMessage {
				if first, ok := observeEnrollmentConnectAdmission(payload, tenant, w.EnrollmentSecret, ticket, obs); ok {
					payload = first
				}
				w.bindEnrollmentObservation(tenant, obs)
			}
			if err := upstream.WriteMessage(kind, payload); err != nil {
				return
			}
		}
	}()
	go func() {
		defer closeBoth()
		for {
			kind, payload, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage || kind == websocket.BinaryMessage {
				observeEnrollmentPairEvent(payload, obs)
				w.bindEnrollmentObservation(tenant, obs)
			}
			if err := client.WriteMessage(kind, payload); err != nil {
				return
			}
		}
	}()
	<-closed
	w.bindEnrollmentObservation(tenant, obs)
	_ = origin // origin is checked by the caller and retained in the signature for auditability.
}

func observeEnrollmentConnectAdmission(payload []byte, tenant string, secret []byte, expectedID string, obs *enrollmentObservation) ([]byte, bool) {
	if expectedID == "" {
		return payload, false
	}
	var root map[string]any
	if json.Unmarshal(payload, &root) != nil || strings.TrimSpace(stringValue(root["method"])) != "connect" {
		return payload, false
	}
	params, _ := root["params"].(map[string]any)
	auth, _ := params["auth"].(map[string]any)
	ticket := stringValue(auth["token"])
	if ticket == "" {
		ticket = stringValue(auth["bootstrapToken"])
	}
	if id, ok := openclawgateway.VerifyEnrollmentAdmissionTicket(ticket, tenant, time.Now(), secret); !ok || id != expectedID {
		return payload, false
	}
	return observeEnrollmentConnect(payload, ticket, obs)
}

func observeEnrollmentConnect(payload []byte, ticket string, obs *enrollmentObservation) ([]byte, bool) {
	if len(payload) == 0 || len(payload) > enrollmentWebSocketReadLimit {
		return payload, false
	}
	var root map[string]any
	if json.Unmarshal(payload, &root) != nil || strings.TrimSpace(stringValue(root["method"])) != "connect" {
		return payload, false
	}
	params, _ := root["params"].(map[string]any)
	identity := nestedMap(params, "device")
	if identity == nil {
		identity = nestedMap(params, "client")
	}
	publicKey := stringValue(identity["publicKey"])
	deviceID := stringValue(identity["id"])
	if publicKey == "" {
		publicKey = stringValue(params["publicKey"])
	}
	if deviceID == "" {
		deviceID = stringValue(params["deviceId"])
	}
	if !validEnrollmentIdentity(deviceID, publicKey) {
		return payload, false
	}
	role := stringValue(params["role"])
	if role != "node" && role != "operator" {
		return payload, false
	}
	roles := []string{role}
	scopes := stringSlice(params["scopes"])
	auth, _ := params["auth"].(map[string]any)
	// Official iOS uses the bootstrapToken field for the one-time admission
	// ticket; macOS uses token. Both values are the same signed ticket and must
	// be compared against the original bytes before observing the binding.
	providedTicket := stringValue(auth["token"])
	if providedTicket == "" {
		providedTicket = stringValue(auth["bootstrapToken"])
	}
	if ticket == "" || providedTicket != ticket {
		return payload, false
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if obs.haveKey {
		return payload, false
	}
	obs.binding.DeviceID, obs.binding.PublicKey, obs.binding.Roles, obs.binding.Scopes = deviceID, publicKey, roles, scopes
	obs.haveKey = true
	// The native signature covers the token, role and ordered scopes. Keep
	// these bytes intact; admission requires a native authentication adapter.
	return payload, true
}

func observeEnrollmentPairEvent(payload []byte, obs *enrollmentObservation) {
	var root map[string]any
	if json.Unmarshal(payload, &root) != nil || stringValue(root["event"]) != "device.pair.requested" {
		return
	}
	payloadMap, _ := root["payload"].(map[string]any)
	requestID := stringValue(payloadMap["requestId"])
	if requestID == "" {
		requestID = stringValue(payloadMap["request_id"])
	}
	if requestID == "" {
		return
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if !obs.haveKey || stringValue(payloadMap["deviceId"]) != obs.binding.DeviceID || stringValue(payloadMap["publicKey"]) != obs.binding.PublicKey || (obs.haveReq && requestID != obs.binding.RequestID) {
		return
	}
	obs.binding.RequestID = requestID
	obs.haveReq = true
}

func (w *Worker) bindEnrollmentObservation(tenant string, obs *enrollmentObservation) {
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if !obs.haveKey || !obs.haveReq || !validEnrollmentIdentity(obs.binding.DeviceID, obs.binding.PublicKey) {
		return
	}
	binding := obs.binding
	binding.EnrollmentID = obs.enrollID
	w.enrollmentMu.Lock()
	if w.enrollmentByDevice == nil {
		w.enrollmentByDevice = make(map[string]openclawgateway.EnrollmentBinding)
	}
	key := tenant + "/" + binding.DeviceID
	if previous, exists := w.enrollmentByDevice[key]; !exists || (previous.EnrollmentID == binding.EnrollmentID && previous.RequestID == binding.RequestID) {
		w.enrollmentByDevice[key] = binding
	}
	w.enrollmentMu.Unlock()
}

func nestedMap(root map[string]any, key string) map[string]any {
	value, _ := root[key].(map[string]any)
	return value
}
func stringValue(value any) string { s, _ := value.(string); return strings.TrimSpace(s) }
func stringSlice(value any) []string {
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s := stringValue(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func deviceIDForPublicKey(publicKey string) string {
	key, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:])
}

func validEnrollmentIdentity(deviceID, publicKey string) bool {
	key, err := base64.RawURLEncoding.DecodeString(publicKey)
	return err == nil && len(key) == 32 && base64.RawURLEncoding.EncodeToString(key) == publicKey && deviceID == deviceIDForPublicKey(publicKey)
}
