package tryon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 超过上限的 ComfyUI 结果图片/状态 JSON 必须报错，而不是被静默截断后当作有效结果返回（截断图片仍能通过文件头嗅探）
func TestComfyUIOversizedResponsesRejected(t *testing.T) {
	oversized := func(w http.ResponseWriter) {
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		_, _ = io.CopyN(w, zeroReader{}, modelMaxResponseBytes)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oversized(w)
	}))
	defer server.Close()
	c := NewComfyUI(&ModelConfig{Name: "comfyui", Provider: "comfyui", BaseURL: server.URL, OutputNode: "5"})

	if data, err := c.downloadImage(context.Background(), comfyImageRef{Filename: "r.png"}); err == nil {
		t.Fatalf("oversized image accepted (%d bytes)", len(data))
	}
	if _, err := c.getJSON(context.Background(), "/history/x"); err == nil {
		t.Fatal("oversized JSON accepted")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
