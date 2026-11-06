package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
)

func TestAdvancedPlanValidatesAndEscapesEnvironment(t *testing.T) {
	p := testPlan()
	tokens := 256
	p.Advanced = &domain.AdvancedSettings{MaxTokens: &tokens, Environment: map[string]string{
		"LLM_USER_LABEL": "${HF_TOKEN}", "OLLAMA_KEEP_ALIVE": "12m",
	}}
	if err := Validate(p); err == nil {
		t.Fatal("schema v1 accepted advanced settings")
	}
	p.SchemaVersion = 2
	rendered, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`OLLAMA_KEEP_ALIVE: "12m"`, `LLM_USER_LABEL: "$${HF_TOKEN}"`} {
		if !strings.Contains(string(rendered), want) {
			t.Errorf("Compose missing %s", want)
		}
	}
	p.Advanced.Environment["OLLAMA_HOST"] = "0.0.0.0"
	if err := Validate(p); err == nil {
		t.Fatal("advanced settings overrode a launcher-owned endpoint")
	}
}

func TestOllamaCreatesModelWithDefaults(t *testing.T) {
	p := testPlan()
	p.SchemaVersion = 2
	temp, tokens := 0.0, 16
	p.Advanced = &domain.AdvancedSettings{Temperature: &temp, MaxTokens: &tokens}
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = fmt.Fprint(w, `{"models":[]}`)
		case "/api/create":
			var req struct {
				From       string             `json:"from"`
				Model      string             `json:"model"`
				Parameters map[string]float64 `json:"parameters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.From != p.Model.Ref ||
				req.Model != APIModel(p) || req.Parameters["num_predict"] != 16 {
				t.Errorf("unexpected create request: %+v err=%v", req, err)
			}
			if v, ok := req.Parameters["temperature"]; !ok || v != 0 {
				t.Errorf("zero temperature was not explicitly passed: %+v", req.Parameters)
			}
			created = true
			_, _ = fmt.Fprint(w, `{"status":"success"}`)
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	p.Port, _ = strconv.Atoi(port)
	m := Manager{client: server.Client()}
	if err := m.applyOllamaDefaults(context.Background(), p); err != nil || !created {
		t.Fatalf("Ollama defaults not applied: %v", err)
	}
}
