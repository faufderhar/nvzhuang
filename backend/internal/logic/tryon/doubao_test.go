package tryon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeDoubaoSizeWithinSeedreamLimits(t *testing.T) {
	cases := map[string]string{
		"": "1664x2496", "auto": "1664x2496", "1024x1536": "1664x2496", "1024x1792": "1664x2496", "999x1": "1664x2496",
		"1536x1024": "2496x1664", "1792x1024": "2496x1664",
		"1024x1024": "2048x2048", "4096x4096": "2048x2048",
	}
	for in, want := range cases {
		got := normalizeDoubaoSize(in)
		if got != want {
			t.Errorf("normalizeDoubaoSize(%q)=%q want %q", in, got, want)
		}
		var w, h int
		if _, err := fmtSscan(got, &w, &h); err != nil {
			t.Fatal(err)
		}
		// 必须同时落在 5.0 flash/pro [921600,4624220] 与 4.5/5.0 lite [3686400,16777216] 的总像素区间
		if px := w * h; px < 3686400 || px > 4624220 {
			t.Errorf("%s 总像素 %d 不在 Seedream 4.x/5.x 交集内", got, px)
		}
	}
}

func TestDoubaoRequestCarriesReferenceImages(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	gen := NewDoubao(&ModelConfig{Name: "doubao", APIKey: "k", BaseURL: srv.URL, Model: DefaultDoubaoModel})
	_, _ = gen.Generate(context.Background(), png, png, "p", "auto")
	if body["model"] != DefaultDoubaoModel || body["size"] != "1664x2496" || body["response_format"] != "url" {
		t.Fatalf("unexpected body fields: %v %v %v", body["model"], body["size"], body["response_format"])
	}
	imgs, _ := body["image"].([]any)
	if len(imgs) != 2 {
		t.Fatalf("want 2 reference images, got %d", len(imgs))
	}
	for _, v := range imgs {
		if s, _ := v.(string); !strings.HasPrefix(s, "data:image/png;base64,iVBOR") {
			t.Errorf("reference image must be lowercase data URI, got %.40s", s)
		}
	}
	if w, ok := body["watermark"]; !ok || w != false {
		t.Errorf("watermark must be sent as false, got %v (present=%v)", w, ok)
	}
	// 5.0 flash/pro 不支持组图参数，不应发送
	if _, ok := body["sequential_image_generation"]; ok {
		t.Error("sequential_image_generation should not be sent")
	}
}

func TestDoubaoTextOnlyModelDetection(t *testing.T) {
	if !isDoubaoTextOnlyModel("doubao-seedream-3-0-t2i-250415") || isDoubaoTextOnlyModel(DefaultDoubaoModel) || isDoubaoTextOnlyModel("doubao-seedream-4-0-250828") {
		t.Fatal("t2i detection wrong")
	}
}

func fmtSscan(s string, w, h *int) (int, error) { return fmt.Sscanf(s, "%dx%d", w, h) }
