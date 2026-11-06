package nearby

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"
)

const (
	whoNewsURL  = "https://www.who.int/rss-feeds/news-english.xml"
	localNewsFn = "https://news.google.com/rss/search"
	newsTTL     = 15 * time.Minute
)

type newsItem struct {
	Title       string `json:"title"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
	Scope       string `json:"scope"`
}

type rssFeed struct {
	Items []struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		PubDate string `xml:"pubDate"`
		Source  string `xml:"source"`
	} `xml:"channel>item"`
}

// news returns health headlines: the WHO newsroom worldwide, and local stories for the caller's city
// and country. Only the city and country names are sent upstream, never coordinates.
func (m *Module) news(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	city, country := clip(strings.TrimSpace(q.Get("city")), 60), strings.ToUpper(strings.TrimSpace(q.Get("country_code")))
	if len(country) != 2 {
		country = ""
	}
	key := "news:" + strings.ToLower(city) + ":" + country
	if v, ok := m.fromCache(key); ok {
		httpx.WriteJSON(w, 200, v)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	type result struct {
		items []newsItem
		err   error
	}
	global, google, local := make(chan result, 1), make(chan result, 1), make(chan result, 1)
	go func() {
		i, err := m.feed(ctx, m.NewsURL, "WHO", "global", "")
		global <- result{i, err}
	}()
	go func() {
		params := url.Values{"q": {"public health when:7d"}, "hl": {"en-US"}, "gl": {"US"}, "ceid": {"US:en"}}
		i, err := m.feed(ctx, m.LocalNewsURL+"?"+params.Encode(), "", "global", "")
		google <- result{i, err}
	}()
	go func() {
		if city == "" && country == "" {
			local <- result{}
			return
		}
		search := func(term string) ([]newsItem, error) {
			params := url.Values{"q": {term + " when:7d"}, "hl": {"en-" + orDefault(country, "IN")}, "gl": {orDefault(country, "IN")}, "ceid": {orDefault(country, "IN") + ":en"}}
			return m.feed(ctx, m.LocalNewsURL+"?"+params.Encode(), "", "local", "")
		}
		term := strings.TrimSpace(city + " health")
		if city == "" {
			term = "health " + country
		}
		i, err := search(term)
		// A small town may have no stories this week, so widen to the country rather than show an empty tab.
		if err == nil && len(i) == 0 && city != "" && country != "" {
			i, err = search("health " + country)
		}
		local <- result{i, err}
	}()
	g, gn, l := <-global, <-google, <-local
	if g.err != nil && gn.err != nil && (l.err != nil || city == "" && country == "") {
		httpx.WriteError(w, 502, "NEWS_UNAVAILABLE", "Health news could not be loaded right now.", nil)
		return
	}
	out := append(firstN(l.items, 12), firstN(gn.items, 12)...)
	out = append(out, firstN(g.items, 12)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Scope == "local" && out[j].Scope != "local" })
	response := map[string]any{"news": out, "local_unavailable": l.err != nil, "global_unavailable": g.err != nil && gn.err != nil}
	if g.err == nil && gn.err == nil && l.err == nil {
		m.toCache(key, response)
	}
	httpx.WriteJSON(w, 200, response)
}

func firstN(in []newsItem, n int) []newsItem {
	if len(in) > n {
		return in[:n]
	}
	return in
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (m *Module) feed(ctx context.Context, u, source, scope, _ string) ([]newsItem, error) {
	body, err := m.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var f rssFeed
	if err := xml.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("feed: %w", err)
	}
	out := []newsItem{}
	for _, it := range f.Items {
		title, link := plain(it.Title), strings.TrimSpace(it.Link)
		if title == "" || !strings.HasPrefix(link, "http") {
			continue
		}
		src := source
		if src == "" {
			src = plain(it.Source)
			title = strings.TrimSuffix(title, " - "+src)
		}
		when := ""
		for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
			if t, err := time.Parse(layout, strings.TrimSpace(it.PubDate)); err == nil {
				when = t.UTC().Format(time.RFC3339)
				break
			}
		}
		out = append(out, newsItem{Title: title, Source: orDefault(src, "News"), URL: link, PublishedAt: when, Scope: scope})
	}
	return out, nil
}
