package nearby

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewsReportsPartialFeedFailureAndIncludesGoogleWorld(t *testing.T) {
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<rss><channel><item><title>World health - Journal</title><link>https://news.example/a</link><source>Journal</source></item></channel></rss>`))
	}))
	defer google.Close()
	m, h := testModule(t)
	m.NewsURL, m.LocalNewsURL = "http://127.0.0.1:1", google.URL
	rec := get(h, "/nearby/news")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "World health") || !strings.Contains(rec.Body.String(), `"scope":"global"`) {
		t.Fatalf("Google world fallback: %d %s", rec.Code, rec.Body)
	}

	m, h = testModule(t)
	m.NewsURL, m.LocalNewsURL = google.URL, "http://127.0.0.1:1"
	rec = get(h, "/nearby/news?city=London&country_code=GB")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"local_unavailable":true`) {
		t.Fatalf("local failure: %d %s", rec.Code, rec.Body)
	}
}
