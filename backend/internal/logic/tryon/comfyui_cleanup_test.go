package tryon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func writeTestWorkflow(t *testing.T, outputClass string) string {
	t.Helper()
	wf := map[string]interface{}{
		"1": map[string]interface{}{"class_type": "LoadImage", "inputs": map[string]interface{}{"image": "person"}},
		"2": map[string]interface{}{"class_type": "LoadImage", "inputs": map[string]interface{}{"image": "garment"}},
		"3": map[string]interface{}{"class_type": "PromptNode", "inputs": map[string]interface{}{"prompt": "p"}},
		"5": map[string]interface{}{"class_type": outputClass, "inputs": map[string]interface{}{"images": []interface{}{"3", 0}}},
	}
	data, _ := json.Marshal(wf)
	path := filepath.Join(t.TempDir(), "wf.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// F-09 / F-19：客户端取消或超时后，删除排队任务并仅中断本任务；PreviewImage 输出不注入 filename_prefix
func TestComfyUICancelsPromptOnTimeout(t *testing.T) {
	var queueDeletes, interrupts, historyDeletes atomic.Int32
	var interruptBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/system_stats":
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/object_info":
			_, _ = io.WriteString(w, `{"LoadImage":{},"PromptNode":{},"PreviewImage":{}}`)
		case r.URL.Path == "/upload/image":
			_ = r.ParseMultipartForm(1 << 20)
			_, _ = io.WriteString(w, `{"name":"x.png","subfolder":"tryon","type":"temp"}`)
		case r.URL.Path == "/prompt":
			var req comfyPromptRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			out := req.Prompt["5"].(map[string]interface{})["inputs"].(map[string]interface{})
			if _, ok := out["filename_prefix"]; ok {
				t.Error("PreviewImage output must not get filename_prefix")
			}
			_, _ = io.WriteString(w, `{"prompt_id":"p-slow","node_errors":{}}`)
		case r.URL.Path == "/history/p-slow":
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/queue" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"queue_running":[[0,"p-slow",{},{},[]]],"queue_pending":[]}`)
		case r.URL.Path == "/queue" && r.Method == http.MethodPost:
			queueDeletes.Add(1)
		case r.URL.Path == "/interrupt":
			b, _ := io.ReadAll(r.Body)
			interruptBody.Store(string(b))
			interrupts.Add(1)
		case r.URL.Path == "/history" && r.Method == http.MethodPost:
			historyDeletes.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewComfyUI(&ModelConfig{
		Name: "comfyui", Provider: "comfyui", BaseURL: server.URL, Model: "wf",
		WorkflowPath: writeTestWorkflow(t, "PreviewImage"),
		PersonNode:   "1", GarmentNode: "2", PromptNode: "3", OutputNode: "5",
		PollInterval: 5 * time.Millisecond, RequestTimeout: 50 * time.Millisecond,
	})
	in := base64.StdEncoding.EncodeToString([]byte("img"))
	if _, err := adapter.Generate(context.Background(), in, in, "p", ""); err == nil {
		t.Fatal("expected timeout error")
	}
	if queueDeletes.Load() != 1 || interrupts.Load() != 1 || historyDeletes.Load() != 1 {
		t.Fatalf("cleanup calls queue=%d interrupt=%d history=%d", queueDeletes.Load(), interrupts.Load(), historyDeletes.Load())
	}
	if body, _ := interruptBody.Load().(string); body != `{"prompt_id":"p-slow"}` {
		t.Fatalf("interrupt must target own prompt, got %q", body)
	}
}

// F-12：健康检查成功结果在 TTL 内复用，不重复拉取 /object_info
func TestComfyUIHealthIsCached(t *testing.T) {
	var objectInfo atomic.Int32
	healthy := atomic.Bool{}
	healthy.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			if !healthy.Load() {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		case "/object_info":
			objectInfo.Add(1)
			_, _ = io.WriteString(w, `{"LoadImage":{},"PromptNode":{},"SaveImage":{}}`)
		}
	}))
	defer server.Close()
	cfg := &ModelConfig{Name: "comfyui", Provider: "comfyui", BaseURL: server.URL,
		WorkflowPath: writeTestWorkflow(t, "SaveImage"), PersonNode: "1", GarmentNode: "2", PromptNode: "3", OutputNode: "5"}

	for i := 0; i < 3; i++ {
		if err := NewComfyUI(cfg).Health(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if objectInfo.Load() != 1 {
		t.Fatalf("expected 1 object_info fetch within TTL, got %d", objectInfo.Load())
	}

	old := comfyHealthTTL
	comfyHealthTTL = 0
	defer func() { comfyHealthTTL = old }()
	healthy.Store(false)
	if err := NewComfyUI(cfg).Health(context.Background()); err == nil {
		t.Fatal("expected failure when cache disabled and ComfyUI down")
	}
}
