package tryon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsGrokImagineImageName(t *testing.T) {
	cases := map[string]bool{
		"grok-imagine-image-2.0": true,
		"Grok-Imagine-Image":     true,
		"grok_imagine_image_2":   true,
		"grok-imagine-video":     false,
		"grok-2-image":           false,
		"gpt-image-2":            false,
		"":                       false,
	}
	for in, want := range cases {
		if got := isGrokImagineImageName(in); got != want {
			t.Errorf("isGrokImagineImageName(%q)=%v, want %v", in, got, want)
		}
	}

	grok := NewOpenAIImages(&ModelConfig{Name: "custom", Model: "grok-imagine-image-2.0"})
	if !isGrokImagineImage(grok) {
		t.Fatal("upstream model id should select grok imagine")
	}
	// 别名碰巧包含该字符串时，仍以上游模型 ID 为准。
	aliasOnly := NewOpenAIImages(&ModelConfig{Name: "grok-imagine-image", Model: "gpt-image-2"})
	if isGrokImagineImage(aliasOnly) {
		t.Fatal("alias must not override a non-grok upstream model")
	}
}

func TestBuildGrokKeepsPersonAndGarmentRoles(t *testing.T) {
	en := BuildGrok("en")
	if !strings.Contains(en, "Image 1 is the person") || !strings.Contains(en, "Image 2 is the clothing reference only") || !strings.Contains(en, "Do not copy the model") {
		t.Fatalf("english grok prompt missing role constraints: %s", en)
	}
	zh := BuildGrok("zh")
	if !strings.Contains(zh, "第 1 张是人物") || !strings.Contains(zh, "第 2 张只作为服装参考") || !strings.Contains(zh, "不得复制第 2 张里的模特") {
		t.Fatalf("chinese grok prompt missing role constraints: %s", zh)
	}
}

func TestGrokAspectRatio(t *testing.T) {
	cases := map[string]string{
		"": "2:3", "auto": "auto", "1024x1536": "2:3", "1024x1792": "2:3",
		"1024x1024": "1:1", "1536x1024": "3:2", "1792x1024": "3:2",
	}
	for in, want := range cases {
		if got := grokAspectRatio(in); got != want {
			t.Errorf("grokAspectRatio(%q)=%q want %q", in, got, want)
		}
	}
}

func TestGrokImagineSendsPersonThenGarmentToEdits(t *testing.T) {
	personB64 := encodeB64([]byte("\x89PNG\r\n\x1a\nPERSON"))
	garmentB64 := encodeB64([]byte("\xff\xd8\xffGARMENT"))

	var (
		gotPath string
		body    grokEditRequest
		rawBody string
	)
	const resultB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		_ = json.Unmarshal(b, &body)
		if r.URL.Path != "/images/edits" {
			http.Error(w, "wrong endpoint", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + resultB64 + `"}]}`))
	}))
	defer srv.Close()

	prompt := BuildGrok("zh")
	gen := NewOpenAIImages(&ModelConfig{
		Name:    "browser-custom",
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "grok-imagine-image-2.0",
	})
	got, err := gen.Generate(context.Background(), personB64, garmentB64, prompt, "1024x1536")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got != resultB64 {
		t.Fatalf("result b64 mismatch")
	}
	if gotPath != "/images/edits" {
		t.Fatalf("path=%s body=%s", gotPath, rawBody)
	}
	if strings.Contains(rawBody, `"image":[`) || strings.Contains(rawBody, `"quality":"high"`) || strings.Contains(rawBody, `"size"`) {
		t.Fatalf("grok edit must not use the text-to-image image[] body: %s", rawBody)
	}
	if body.Model != "grok-imagine-image-2.0" || body.AspectRatio != "2:3" || body.ResponseFormat != "b64_json" || body.Quality != "medium" {
		t.Fatalf("unexpected edit fields: %+v", body)
	}
	if body.Prompt != prompt {
		t.Fatal("prompt was rewritten")
	}
	if len(body.Images) != 2 || body.Images[0].Type != "image_url" || body.Images[1].Type != "image_url" {
		t.Fatalf("images=%+v", body.Images)
	}
	if !strings.Contains(body.Images[0].URL, personB64) || strings.Contains(body.Images[0].URL, garmentB64) {
		t.Fatal("image 1 must be the person")
	}
	if !strings.HasPrefix(body.Images[0].URL, "data:image/png;base64,") {
		t.Fatalf("person data url mime=%s", body.Images[0].URL[:24])
	}
	if !strings.Contains(body.Images[1].URL, garmentB64) || strings.Contains(body.Images[1].URL, personB64) {
		t.Fatal("image 2 must be the garment")
	}
	if !strings.HasPrefix(body.Images[1].URL, "data:image/jpeg;base64,") {
		t.Fatalf("garment data url mime=%s", body.Images[1].URL[:24])
	}
}

func TestNonGrokOpenAIStillUsesGenerations(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/chat/completions" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[]}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	png := encodeB64([]byte("\x89PNG\r\n\x1a\n"))
	// 别名含 grok-imagine-image，但上游模型是 gpt-image-2，必须仍走原协议。
	gen := NewOpenAIImages(&ModelConfig{Name: "grok-imagine-image", APIKey: "k", BaseURL: srv.URL, Model: "gpt-image-2"})
	_, _ = gen.Generate(context.Background(), png, png, "p", "1024x1536")
	if len(paths) != 2 || paths[0] != "/chat/completions" || paths[1] != "/images/generations" {
		t.Fatalf("gpt-image paths=%v", paths)
	}
}
