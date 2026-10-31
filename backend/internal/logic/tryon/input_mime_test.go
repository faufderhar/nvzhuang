package tryon

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F-22：向上游声明的输入图片 MIME 必须与真实格式一致
func TestInputImageMIME(t *testing.T) {
	jpeg := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01"))
	webp := base64.StdEncoding.EncodeToString([]byte("RIFF\x04\x00\x00\x00WEBPVP8 \x00\x00\x00\x00"))
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00"))
	cases := map[string]string{jpeg: "image/jpeg", webp: "image/webp", png: "image/png", "!!!": "image/png"}
	for in, want := range cases {
		if got := inputImageMIME(in); got != want {
			t.Errorf("inputImageMIME(%q)=%s want %s", in[:3], got, want)
		}
	}
	if got := inputImageMIME("data:image/png;base64," + jpeg); got != "image/jpeg" {
		t.Errorf("data URL prefix must be stripped and bytes sniffed, got %s", got)
	}
	if !strings.HasPrefix(inputImageDataURL(webp), "data:image/webp;base64,") {
		t.Error("data URL must carry detected MIME")
	}

	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		http.Error(w, "stop", http.StatusBadRequest)
	}))
	defer srv.Close()
	g := NewGemini(&ModelConfig{Name: "gemini", Provider: "gemini", APIKey: "k", BaseURL: srv.URL, Model: "m"})
	_, _ = g.Generate(context.Background(), jpeg, webp, "p", "")
	if !strings.Contains(body, `"mimeType":"image/jpeg"`) || !strings.Contains(body, `"mimeType":"image/webp"`) {
		t.Fatalf("gemini request must declare real MIME types, body=%s", body)
	}
}
