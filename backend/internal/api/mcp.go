package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	mcpProtocolVersion = "2025-06-18"
	mcpScopeRecords    = "records:read"
	mcpTokenTTL        = 10 * time.Minute
)

var mcpSigningKey = loadMCPSigningKey()

func loadMCPSigningKey() []byte {
	if v := strings.TrimSpace(os.Getenv("CORTEX_MCP_SIGNING_KEY")); len(v) >= 32 {
		return []byte(v)
	}
	log.Print("CORTEX_MCP_SIGNING_KEY is unset or shorter than 32 bytes; using an ephemeral key, MCP tokens will not survive a restart")
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

type mcpClaims struct {
	Owner   string `json:"sub"`
	Profile string `json:"profile"`
	Scope   string `json:"scope"`
	Expires int64  `json:"exp"`
}

func signMCPToken(c mcpClaims) string {
	payload, _ := json.Marshal(c)
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, mcpSigningKey)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyMCPToken(token string, now time.Time) (mcpClaims, error) {
	var c mcpClaims
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return c, errors.New("malformed token")
	}
	given, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return c, errors.New("malformed token")
	}
	mac := hmac.New(sha256.New, mcpSigningKey)
	mac.Write([]byte(body))
	if !hmac.Equal(given, mac.Sum(nil)) {
		return c, errors.New("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return c, errors.New("malformed token")
	}
	if c.Owner == "" || c.Profile == "" || c.Scope != mcpScopeRecords || now.Unix() >= c.Expires {
		return c, errors.New("token not valid")
	}
	return c, nil
}

// createMCPToken mints a short-lived token bound to one owner and one profile.
func (a *API) createMCPToken(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	expires := time.Now().Add(mcpTokenTTL)
	token := signMCPToken(mcpClaims{Owner: owner(r), Profile: profileID, Scope: mcpScopeRecords, Expires: expires.Unix()})
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires_at": expires, "scope": mcpScopeRecords, "endpoint": "/api/v1/mcp"})
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func rpcError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

var mcpTools = []map[string]any{
	{
		"name":        "search_records",
		"description": "Semantic search over the profile's own verified lab observations.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "maxLength": 500}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}},
	},
	{
		"name":        "list_observations",
		"description": "List the profile's verified lab observations, newest first.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}},
	},
	{
		"name":        "biomarker_history",
		"description": "History of one biomarker code (for example HBA1C) for the profile.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string", "maxLength": 40}}, "required": []string{"code"}},
	},
}

// mcpEndpoint implements the MCP streamable-HTTP JSON-RPC subset needed for read-only tools.
func (a *API) mcpEndpoint(w http.ResponseWriter, r *http.Request) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "A scoped MCP token is required.", nil)
		return
	}
	claims, err := verifyMCPToken(parts[1], time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "The MCP token is invalid or expired.", nil)
		return
	}
	var req rpcRequest
	if decodeJSON(r, 64<<10, &req) != nil || req.JSONRPC != "2.0" {
		rpcError(w, nil, -32700, "invalid JSON-RPC request")
		return
	}
	switch req.Method {
	case "initialize":
		rpcResult(w, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "cortex-records", "version": "0.1.0"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		rpcResult(w, req.ID, map[string]any{})
	case "tools/list":
		rpcResult(w, req.ID, map[string]any{"tools": mcpTools})
	case "tools/call":
		a.mcpCallTool(w, r, req, claims)
	default:
		rpcError(w, req.ID, -32601, "method not found")
	}
}

func (a *API) mcpCallTool(w http.ResponseWriter, r *http.Request, req rpcRequest, claims mcpClaims) {
	ctx := r.Context()
	var params struct {
		Name      string `json:"name"`
		Arguments struct {
			Query string `json:"query"`
			Code  string `json:"code"`
			Limit int    `json:"limit"`
		} `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		rpcError(w, req.ID, -32602, "invalid params")
		return
	}
	if !a.hasConsent(ctx, claims.Profile, "ai_processing") || !(a.hasConsent(ctx, claims.Profile, "health_data") || a.hasConsent(ctx, claims.Profile, "medical_record_storage")) {
		toolResult(w, req.ID, nil, "Consent for AI processing of health data is not active for this profile.")
		return
	}
	var data any
	var err error
	switch params.Name {
	case "search_records":
		query := strings.TrimSpace(params.Arguments.Query)
		if query == "" || len(query) > 500 {
			toolResult(w, req.ID, nil, "query is required and must be at most 500 characters")
			return
		}
		if err = a.indexProfileRecords(ctx, claims.Owner, claims.Profile); err == nil {
			data, err = a.searchRecords(ctx, claims.Owner, claims.Profile, query, params.Arguments.Limit)
		}
	case "list_observations":
		data, err = a.mcpObservations(ctx, claims, "", params.Arguments.Limit)
	case "biomarker_history":
		code := strings.ToUpper(strings.TrimSpace(params.Arguments.Code))
		if code == "" || len(code) > 40 {
			toolResult(w, req.ID, nil, "code is required")
			return
		}
		data, err = a.mcpObservations(ctx, claims, code, params.Arguments.Limit)
	default:
		rpcError(w, req.ID, -32602, "unknown tool")
		return
	}
	if err != nil {
		log.Printf("mcp tool %s failed: %v", params.Name, err)
		toolResult(w, req.ID, nil, "The records tool is temporarily unavailable.")
		return
	}
	_, _ = a.db.Pool.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,details) VALUES($1,$2::uuid,'mcp.tool_called','agent_tool',jsonb_build_object('tool',$3::text))`, claims.Owner, claims.Profile, params.Name)
	toolResult(w, req.ID, data, "")
}

func toolResult(w http.ResponseWriter, id json.RawMessage, data any, failure string) {
	if failure != "" {
		rpcResult(w, id, map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": failure}}})
		return
	}
	text, _ := json.Marshal(data)
	rpcResult(w, id, map[string]any{"content": []map[string]string{{"type": "text", "text": string(text)}}})
}

func (a *API) mcpObservations(ctx context.Context, claims mcpClaims, code string, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := a.db.Pool.Query(ctx, `SELECT id::text,COALESCE(biomarker_code,''),name,value::float8,unit,observed_at,COALESCE(reference_range,'') FROM observations WHERE profile_id=$1 AND owner_id=$2 AND verification_status='VERIFIED' AND ($3::text='' OR biomarker_code=$3::text) ORDER BY observed_at DESC NULLS LAST,created_at DESC LIMIT $4`, claims.Profile, claims.Owner, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, bc, name, unit, rng string
		var value float64
		var at *time.Time
		if err := rows.Scan(&id, &bc, &name, &value, &unit, &at, &rng); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"observation_id": id, "biomarker_code": bc, "name": name, "value": value, "unit": unit, "observed_at": at, "reference_range": rng})
	}
	return items, rows.Err()
}
