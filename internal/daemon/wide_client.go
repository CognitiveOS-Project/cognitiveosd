package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type WideModelClient struct {
	daemon       *Daemon
	client       *http.Client
	loaded       bool
	modelName    string
	modelID      string
	systemPrompt string
	mu           sync.RWMutex
}

func NewWideModelClient(d *Daemon) *WideModelClient {
	return &WideModelClient{
		daemon: d,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

func (w *WideModelClient) Generate(prompt string) (string, error) {
	w.mu.RLock()
	sysPrompt := w.systemPrompt
	w.mu.RUnlock()

	fullPrompt := prompt
	if sysPrompt != "" {
		fullPrompt = sysPrompt + "\n\n" + prompt
	}

	body := map[string]interface{}{
		"model":  "wide-model",
		"prompt": fullPrompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"num_predict": 512,
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	resp, err := w.client.Post(w.daemon.Config.InferenceURL+"/api/generate", "application/json", &buf)
	if err != nil {
		return "", fmt.Errorf("inference request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("inference error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	w.mu.Lock()
	w.loaded = true
	w.modelName = "wide-model"
	w.mu.Unlock()

	return result.Response, nil
}

func (w *WideModelClient) SetSystemPrompt(prompt string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.systemPrompt = prompt
	w.daemon.Log.Printf("system prompt set (%d bytes)", len(prompt))
}

func (w *WideModelClient) SystemPrompt() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.systemPrompt
}

func (w *WideModelClient) Load(modelPath string) error {
	return w.LoadWithID(modelPath, "")
}

func (w *WideModelClient) LoadWithID(modelPath, modelID string) error {
	body := map[string]interface{}{
		"model": "wide-model",
		"path":  modelPath,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("encode load request: %w", err)
	}

	resp, err := w.client.Post(w.daemon.Config.InferenceURL+"/api/pull", "application/json", &buf)
	if err != nil {
		return fmt.Errorf("load request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("load failed: HTTP %d", resp.StatusCode)
	}

	w.mu.Lock()
	w.loaded = true
	w.modelName = modelPath
	w.modelID = modelID
	w.mu.Unlock()

	return nil
}

func (w *WideModelClient) Unload(reason string) error {
	body := map[string]interface{}{
		"reason": reason,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("encode unload request: %w", err)
	}

	req, err := http.NewRequest("DELETE", w.daemon.Config.InferenceURL+"/api/delete", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("unload request: %w", err)
	}
	defer resp.Body.Close()

	w.mu.Lock()
	w.loaded = false
	w.modelName = ""
	w.modelID = ""
	w.mu.Unlock()

	return nil
}

func (w *WideModelClient) IsLoaded() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.loaded
}

func (w *WideModelClient) LoadedModelName() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.loaded {
		return ""
	}
	return w.modelName
}

func (w *WideModelClient) LoadedModelID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.loaded {
		return ""
	}
	return w.modelID
}

func (w *WideModelClient) Health() bool {
	resp, err := w.client.Get(w.daemon.Config.InferenceURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}
