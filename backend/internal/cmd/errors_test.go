package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func testPNGB64(w, h int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 120, G: 80, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func postTryon(t *testing.T, url string, payload map[string]interface{}) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url+"/tryon", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+routesTestToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// F-03 / F-24：上游失败返回 502 JSON 固定文案，不回显上游原文；参数错误返回 400
func TestRoutesErrorStatusAndSanitization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom sk-proj-AB12cdEF34ghIJ56klMN78opQR90 at 10.1.2.3"}`))
	}))
	defer upstream.Close()

	adapter, err := gcfg.NewAdapterContent(`
llm:
  models:
    routesleak:
      provider: openai
      apiKey: "server-key-routes-test"
      baseURL: "` + upstream.URL + `"
      model: "m"
`)
	if err != nil {
		t.Fatal(err)
	}
	old := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(old)

	url := newRoutesTestServer(t, "routes-errors", true, 100, 8<<20, nil)
	img := testPNGB64(512, 512)

	status, body := postTryon(t, url, map[string]interface{}{"person_image": img, "garment_image": img, "model": "routesleak"})
	if status != http.StatusBadGateway {
		t.Fatalf("upstream failure: want 502, got %d %s", status, body)
	}
	for _, leak := range []string{"sk-proj", "10.1.2.3", "boom", "127.0.0.1"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "UPSTREAM_FAILED") || !strings.Contains(body, "request_id") {
		t.Fatalf("expected UPSTREAM_FAILED JSON with request_id, got %s", body)
	}

	status, body = postTryon(t, url, map[string]interface{}{"person_image": img})
	if status != http.StatusBadRequest {
		t.Fatalf("missing field: want 400, got %d %s", status, body)
	}

	status, body = postTryon(t, url, map[string]interface{}{"person_image": img, "garment_image": img, "model": "no-such-model"})
	if status != http.StatusBadRequest {
		t.Fatalf("unknown model: want 400, got %d %s", status, body)
	}

	status, body = postTryon(t, url, map[string]interface{}{"person_image": img, "garment_image": img, "model": "x",
		"client_config": map[string]string{"provider": "openai", "base_url": "https://10.0.0.1", "api_key": "k", "model": "m"}})
	if status != http.StatusBadRequest {
		t.Fatalf("forbidden client base url: want 400, got %d %s", status, body)
	}

}
