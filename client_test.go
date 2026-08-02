package anilist

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// listResponse builds a GraphQL response body with the given score format and
// raw entry JSON, so tests only spell out the part they care about.
func listResponse(scoreFormat, entries string) string {
	return `{"data":{"User":{"mediaListOptions":{"scoreFormat":"` + scoreFormat + `"}},
		"MediaListCollection":{"lists":[{"entries":[` + entries + `]}]}}}`
}

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.URL = srv.URL
	return c
}

func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListNormalizesScoresAndPicksTitle(t *testing.T) {
	t.Parallel()
	srv := serve(t, listResponse("POINT_100", `
		{"score":90,"media":{"seasonYear":2019,"title":{"romaji":"Kimetsu","english":"Demon Slayer"}}},
		{"score":0,"media":{"seasonYear":2020,"title":{"romaji":"Unrated","english":null}}}`))

	entries, err := newTestClient(srv).List(t.Context(), "nat")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (unrated must be dropped): %+v", len(entries), entries)
	}
	if entries[0].Title != "Demon Slayer" {
		t.Errorf("Title = %q, want the English title when present", entries[0].Title)
	}
	if entries[0].Year != 2019 {
		t.Errorf("Year = %d, want 2019", entries[0].Year)
	}
	if math.Abs(entries[0].Score-9.0) > 0.01 {
		t.Errorf("Score = %.2f, want 9.0 (POINT_100 90 normalized)", entries[0].Score)
	}
}

// AniList lets each user pick their own score format, so the same raw number
// means different things per account. Getting this wrong silently skews every
// downstream ranking rather than failing.
func TestListNormalizesEveryScoreFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		format string
		raw    string
		want   float64
	}{
		{"POINT_100", "90", 9.0},
		{"POINT_10", "9", 9.0},
		{"POINT_10_DECIMAL", "8.5", 8.5},
		{"POINT_5", "4", 8.0},
		{"POINT_3", "3", 10.0},
		{"POINT_3", "2", 6.667},
		{"", "7", 7.0}, // unknown format falls through as already 0..10
	}
	for _, c := range cases {
		t.Run(c.format+"/"+c.raw, func(t *testing.T) {
			t.Parallel()
			srv := serve(t, listResponse(c.format,
				`{"score":`+c.raw+`,"media":{"seasonYear":2019,"title":{"romaji":"X","english":"X"}}}`))

			entries, err := newTestClient(srv).List(t.Context(), "nat")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(entries))
			}
			if math.Abs(entries[0].Score-c.want) > 0.01 {
				t.Errorf("%s score %s = %.3f, want %.3f", c.format, c.raw, entries[0].Score, c.want)
			}
		})
	}
}

func TestListFallsBackToRomajiTitle(t *testing.T) {
	t.Parallel()
	srv := serve(t, listResponse("POINT_10",
		`{"score":8,"media":{"seasonYear":2016,"title":{"romaji":"Shingeki no Kyojin","english":null}}}`))

	entries, err := newTestClient(srv).List(t.Context(), "nat")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Shingeki no Kyojin" {
		t.Fatalf("entries = %+v, want the romaji title", entries)
	}
}

// An entry with neither title is unusable downstream, so it is dropped rather
// than emitted with an empty Title that would match nothing.
func TestListDropsUntitledEntries(t *testing.T) {
	t.Parallel()
	srv := serve(t, listResponse("POINT_10",
		`{"score":8,"media":{"seasonYear":2016,"title":{"romaji":"","english":""}}}`))

	entries, err := newTestClient(srv).List(t.Context(), "nat")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}

// AniList splits a user's library into several named lists (Watching,
// Completed, Dropped...). All of them carry scores and all must be collected.
func TestListFlattensMultipleLists(t *testing.T) {
	t.Parallel()
	srv := serve(t, `{"data":{"User":{"mediaListOptions":{"scoreFormat":"POINT_10"}},
		"MediaListCollection":{"lists":[
			{"entries":[{"score":9,"media":{"seasonYear":2019,"title":{"romaji":"A","english":"A"}}}]},
			{"entries":[{"score":7,"media":{"seasonYear":2020,"title":{"romaji":"B","english":"B"}}}]}
		]}}}`)

	entries, err := newTestClient(srv).List(t.Context(), "nat")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 across both lists: %+v", len(entries), entries)
	}
}

func TestListEmptyCollection(t *testing.T) {
	t.Parallel()
	srv := serve(t, `{"data":{"User":{"mediaListOptions":{"scoreFormat":"POINT_10"}},
		"MediaListCollection":{"lists":[]}}}`)

	entries, err := newTestClient(srv).List(t.Context(), "nobody")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}

func TestListSendsUsernameAsVariable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body, _ := io.ReadAll(r.Body)
		var sent struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		if sent.Variables["u"] != "nat" {
			t.Errorf("variables = %v, want u=nat", sent.Variables)
		}
		if !strings.Contains(sent.Query, "MediaListCollection") {
			t.Errorf("query does not ask for MediaListCollection: %s", sent.Query)
		}
		_, _ = w.Write([]byte(listResponse("POINT_10", "")))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).List(t.Context(), "nat"); err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestListSurfacesHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Too Many Requests"}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).List(t.Context(), "nat")
	if err == nil {
		t.Fatal("List on a 429 = nil, want an error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q does not mention the status code", err)
	}
}

func TestListRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := serve(t, `not json`)

	if _, err := newTestClient(srv).List(t.Context(), "nat"); err == nil {
		t.Fatal("List on a non-JSON body = nil, want a decode error")
	}
}

// AniList answers an unknown username with HTTP 200 and a null User, which must
// come back as an empty list rather than a crash.
func TestListHandlesUnknownUser(t *testing.T) {
	t.Parallel()
	srv := serve(t, `{"data":{"User":null,"MediaListCollection":null},"errors":[{"message":"Not Found"}]}`)

	entries, err := newTestClient(srv).List(t.Context(), "nosuchuser")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()
	c := NewClient()
	if c.URL != defaultURL {
		t.Errorf("URL = %q, want %q", c.URL, defaultURL)
	}
	if c.httpClient == nil || c.httpClient.Timeout == 0 {
		t.Error("NewClient left the http client without a timeout")
	}
}
