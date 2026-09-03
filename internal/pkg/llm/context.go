/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/utils"
)

// ContextWindow uses Ollama's model metadata endpoint. The OpenAI-compatible
// API intentionally does not expose a context limit, while Ollama exposes it
// in model_info through /api/show.
func (p OpenAPIProvider) ContextWindow(ctx context.Context) (window int, err error) {
	responseBody := ""
	requestPath := "/models"
	if strings.EqualFold(p.Kind, "ollama") {
		requestPath = "../api/show"
	}
	defer func() {
		if err != nil {
			logx.Error("provider API request failed", "operation", "context-window", "path", requestPath, "response_body", responseBody, "provider_kind", p.Kind, "base_url", p.BaseURL, "model", p.Model, "err", err)
		}
	}()
	method := http.MethodGet
	var body []byte
	if strings.EqualFold(p.Kind, "ollama") {
		method = http.MethodPost
		body, err = json.Marshal(map[string]string{"name": p.Model})
		if err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, p.endpoint(requestPath), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if err := p.waitRateLimit(ctx); err != nil {
		return 0, err
	}
	client, err := p.client()
	if err != nil {
		return 0, err
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	body, readErr := utils.ReadAllResponse(res.Body)
	responseBody = string(body)
	if readErr != nil {
		return 0, readErr
	}
	if res.StatusCode >= 300 {
		return 0, fmt.Errorf("provider returned %s", res.Status)
	}
	var raw map[string]any
	if err = json.Unmarshal(body, &raw); err != nil {
		return 0, err
	}
	if strings.EqualFold(p.Kind, "ollama") {
		return ollamaContextLength(raw), nil
	}
	return findModelContextLength(raw, p.Model), nil
}

func ollamaContextLength(response map[string]any) int {
	info, _ := response["model_info"].(map[string]any)
	for key, value := range info {
		if strings.HasSuffix(key, ".context_length") || key == "context_length" {
			if n := positiveInt(value); n > 0 {
				return n
			}
		}
	}
	return 0
}

func findModelContextLength(response map[string]any, model string) int {
	if matchesModel(response, model) {
		return contextLengthFromMetadata(response)
	}
	for _, key := range []string{"data", "models"} {
		items, _ := response[key].([]any)
		var only map[string]any
		for _, item := range items {
			candidate, ok := item.(map[string]any)
			if !ok {
				continue
			}
			only = candidate
			if matchesModel(candidate, model) {
				return contextLengthFromMetadata(candidate)
			}
		}
		// A provider with exactly one advertised model can safely omit an ID
		// match, but never use metadata from an arbitrary model in a list.
		if len(items) == 1 && only != nil {
			return contextLengthFromMetadata(only)
		}
	}
	return 0
}

func matchesModel(object map[string]any, model string) bool {
	if model == "" {
		return true
	}
	for _, key := range []string{"id", "name"} {
		if value, _ := object[key].(string); value == model {
			return true
		}
	}
	return false
}

func contextLengthFromMetadata(object map[string]any) int {
	for _, source := range []map[string]any{object, childObject(object, "metadata"), childObject(object, "capabilities"), childObject(object, "limits")} {
		for _, key := range []string{"context_length", "contextLength", "context_window", "contextWindow", "max_context_length", "maxContextLength"} {
			if n := positiveInt(source[key]); n > 0 {
				return n
			}
		}
	}
	return 0
}

func childObject(object map[string]any, key string) map[string]any {
	child, _ := object[key].(map[string]any)
	return child
}

func positiveInt(value any) int {
	switch n := value.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		value, err := strconv.Atoi(n)
		if err == nil && value > 0 {
			return value
		}
	}
	return 0
}
