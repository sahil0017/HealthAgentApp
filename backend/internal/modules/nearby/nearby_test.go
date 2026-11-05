package nearby

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func testModule(t *testing.T) (*Module, http.Handler) {
	t.Helper()
	who := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":[
			{"Title":"Cholera - Kenya","PublicationDateAndTime":"2026-10-01T00:00:00Z","UrlName":"2026-DON1","Summary":"<p>Cases in Kenya</p>"},
			{"Title":"Measles - Peru","PublicationDateAndTime":"2026-09-01T00:00:00Z","UrlName":"2026-DON2","Summary":"Peru reports"}]}`))
	}))
	osm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"elements":[
			{"type":"node","id":2,"lat":28.70,"lon":77.21,"tags":{"name":"Far Hospital"}},
			{"type":"way","id":1,"center":{"lat":28.61,"lon":77.21},"tags":{"name":"Near Hospital","emergency":"yes"}},
			{"type":"node","id":3,"lat":28.61,"lon":77.2,"tags":{}}]}`))
	}))
	t.Cleanup(who.Close)
	t.Cleanup(osm.Close)
	m := &Module{HTTP: http.DefaultClient, WHOURL: who.URL, OverpassURL: osm.URL, cache: map[string]cached{}}
	r := chi.NewRouter()
	m.Authed(r)
	return m, r
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestAlertsPutTheUsersCountryFirstAndStripMarkup(t *testing.T) {
	_, h := testModule(t)
	rec := get(h, "/nearby/alerts?country=Peru")
	var out struct {
		Data struct {
			Alerts []alert `json:"alerts"`
		} `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Data.Alerts) != 2 {
		t.Fatalf("alerts: %d %s", rec.Code, rec.Body)
	}
	if out.Data.Alerts[0].Title != "Measles - Peru" || !out.Data.Alerts[0].InCountry || out.Data.Alerts[1].InCountry {
		t.Fatalf("country alert must lead: %+v", out.Data.Alerts)
	}
	if strings.Contains(out.Data.Alerts[1].Summary, "<") {
		t.Fatalf("markup must be stripped: %q", out.Data.Alerts[1].Summary)
	}
}

func TestHospitalsAreSortedByDistanceAndUnnamedAreDropped(t *testing.T) {
	_, h := testModule(t)
	rec := get(h, "/nearby/hospitals?lat=28.61&lng=77.21&radius_km=20")
	var out struct {
		Data struct {
			Hospitals []hospital `json:"hospitals"`
		} `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Data.Hospitals) != 2 {
		t.Fatalf("hospitals: %d %s", rec.Code, rec.Body)
	}
	if out.Data.Hospitals[0].Name != "Near Hospital" || !out.Data.Hospitals[0].Emergency || out.Data.Hospitals[0].DistanceKm > out.Data.Hospitals[1].DistanceKm {
		t.Fatalf("order: %+v", out.Data.Hospitals)
	}
}

func TestHospitalsRejectBadAreas(t *testing.T) {
	_, h := testModule(t)
	for _, p := range []string{"", "?lat=x&lng=1", "?lat=95&lng=1", "?lat=1&lng=1&radius_km=500"} {
		if c := get(h, "/nearby/hospitals"+p).Code; c != 400 {
			t.Fatalf("%q: want 400, got %d", p, c)
		}
	}
}

func TestUpstreamFailureIsReportedNotInvented(t *testing.T) {
	m, h := testModule(t)
	m.WHOURL = "http://127.0.0.1:1"
	if c := get(h, "/nearby/alerts").Code; c != 502 {
		t.Fatalf("want 502, got %d", c)
	}
}

func TestNearbyPointsWithinOneHundredMetresShareOneCachedAnswer(t *testing.T) {
	m, h := testModule(t)
	get(h, "/nearby/hospitals?lat=28.6112&lng=77.2091")
	m.OverpassURL = "http://127.0.0.1:1"
	if c := get(h, "/nearby/hospitals?lat=28.6114&lng=77.2089").Code; c != 200 {
		t.Fatalf("want cached 200, got %d", c)
	}
}

func TestHospitalsFallBackToAMirrorWhenThePrimaryIsDown(t *testing.T) {
	m, h := testModule(t)
	m.Mirrors = []string{m.OverpassURL}
	m.OverpassURL = "http://127.0.0.1:1"
	if c := get(h, "/nearby/hospitals?lat=28.61&lng=77.21").Code; c != 200 {
		t.Fatalf("want 200 via the mirror, got %d", c)
	}
}

func TestOlderAnswersAreServedWhenEveryMapServerIsDown(t *testing.T) {
	m, h := testModule(t)
	get(h, "/nearby/hospitals?lat=28.61&lng=77.21")
	m.mu.Lock()
	for k, c := range m.cache {
		c.at = c.at.Add(-2 * cacheTTL)
		m.cache[k] = c
	}
	m.mu.Unlock()
	m.OverpassURL = "http://127.0.0.1:1"
	if c := get(h, "/nearby/hospitals?lat=28.61&lng=77.21").Code; c != 200 {
		t.Fatalf("want the older answer, got %d", c)
	}
	if c := get(h, "/nearby/hospitals?lat=19.07&lng=72.87").Code; c != 502 {
		t.Fatalf("an uncached area must still report 502, got %d", c)
	}
}

func TestSourcesSayDoctorListingsAreNotVerified(t *testing.T) {
	_, h := testModule(t)
	body := get(h, "/nearby/sources").Body.String()
	if !strings.Contains(body, `"category":"doctors"`) || !strings.Contains(body, `"verified":false`) {
		t.Fatal(body)
	}
}

func TestPlacesAreTaggedUnverifiedSortedAndValidated(t *testing.T) {
	_, h := testModule(t)
	rec := get(h, "/nearby/places?kind=doctor&lat=28.61&lng=77.21&radius_km=10")
	var out struct {
		Data struct {
			Places   []place `json:"places"`
			Verified bool    `json:"verified"`
			Note     string  `json:"note"`
		} `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Data.Places) != 2 {
		t.Fatalf("places: %d %s", rec.Code, rec.Body)
	}
	if out.Data.Verified || out.Data.Note == "" || out.Data.Places[0].Name != "Near Hospital" || out.Data.Places[0].Kind != "doctor" {
		t.Fatalf("unexpected: %+v", out.Data)
	}
	for _, p := range []string{"", "?kind=spa&lat=1&lng=1", "?kind=lab&lat=999&lng=1", "?kind=lab&lat=1&lng=1&radius_km=500"} {
		if c := get(h, "/nearby/places"+p).Code; c != 400 {
			t.Fatalf("%q: want 400, got %d", p, c)
		}
	}
}

func TestListsComeFromTheSearchServiceWhenNoOverpassServerAnswers(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"osm_type":"node","osm_id":9,"lat":"28.62","lon":"77.21","name":"City Clinic","extratags":{"phone":"+91 11 5550"}}]`))
	}))
	t.Cleanup(search.Close)
	m, h := testModule(t)
	m.OverpassURL = "http://127.0.0.1:1"
	m.SearchURL = search.URL
	for _, p := range []string{"/nearby/hospitals?lat=28.61&lng=77.21", "/nearby/places?kind=lab&lat=28.61&lng=77.21"} {
		rec := get(h, p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "City Clinic") {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestNewsListsLocalStoriesFirstAndNamesTheSource(t *testing.T) {
	rss := func(items string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<rss><channel>` + items + `</channel></rss>`))
		}))
	}
	who := rss(`<item><title>WHO update</title><link>https://who.int/a</link><pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate></item>`)
	local := rss(`<item><title>Dengue cases rise - The Hindu</title><link>https://news.example/b</link><pubDate>Tue, 03 Nov 2026 10:00:00 GMT</pubDate><source>The Hindu</source></item>`)
	t.Cleanup(who.Close)
	t.Cleanup(local.Close)
	m, h := testModule(t)
	m.NewsURL, m.LocalNewsURL = who.URL, local.URL
	rec := get(h, "/nearby/news?city=Chennai&country_code=IN")
	body := rec.Body.String()
	if rec.Code != 200 || strings.Index(body, "Dengue cases rise") > strings.Index(body, "WHO update") || !strings.Contains(body, `"source":"The Hindu"`) || strings.Contains(body, "- The Hindu\"") {
		t.Fatalf("news: %d %s", rec.Code, body)
	}
}
