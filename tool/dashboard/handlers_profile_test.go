package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"engine/actor"
)

func setupHotProfiler(t *testing.T) *actor.HotActorProfiler {
	t.Helper()
	p := actor.NewHotActorProfiler(actor.HotActorProfilerConfig{
		WindowSize:      50,
		HotP99Threshold: 30 * time.Millisecond,
		MinSamples:      10,
	})
	p.Enable()
	for i := 0; i < 30; i++ {
		p.Record("pid-cold", time.Millisecond)
	}
	for i := 0; i < 30; i++ {
		p.Record("pid-hot", 100*time.Millisecond)
	}
	return p
}

func TestHandleProfileHotActors_MissingConfig(t *testing.T) {
	h := &handlers{config: Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/profile/hotactors", nil)
	rec := httptest.NewRecorder()
	h.handleProfileHotActors(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 got %d", rec.Code)
	}
}

func TestHandleProfileHotActors_TopN(t *testing.T) {
	p := setupHotProfiler(t)
	h := &handlers{config: Config{HotProfiler: p}}

	req := httptest.NewRequest(http.MethodGet, "/api/profile/hotactors?topn=5", nil)
	rec := httptest.NewRecorder()
	h.handleProfileHotActors(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp hotActorsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("count=%d", resp.Count)
	}
	if resp.Actors[0].PID != "pid-hot" {
		t.Errorf("top actor: %s", resp.Actors[0].PID)
	}
	if resp.Threshold == 0 {
		t.Errorf("threshold missing")
	}
}

func TestHandleProfileHotActors_OnlyHot(t *testing.T) {
	p := setupHotProfiler(t)
	h := &handlers{config: Config{HotProfiler: p}}
	req := httptest.NewRequest(http.MethodGet, "/api/profile/hotactors?only_hot=true", nil)
	rec := httptest.NewRecorder()
	h.handleProfileHotActors(rec, req)
	var resp hotActorsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Count != 1 || resp.Actors[0].PID != "pid-hot" {
		t.Errorf("only_hot filter failed: %+v", resp)
	}
}

func TestHandleProfileCandidates(t *testing.T) {
	p := setupHotProfiler(t)
	h := &handlers{config: Config{HotProfiler: p}}
	req := httptest.NewRequest(http.MethodGet, "/api/profile/candidates", nil)
	rec := httptest.NewRecorder()
	h.handleProfileCandidates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", rec.Code)
	}
	var resp migrationCandidatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("candidates=%d", resp.Count)
	}
	if len(resp.Advice) != 1 || !strings.Contains(resp.Advice[0], "pid-hot") {
		t.Errorf("advice missing: %+v", resp.Advice)
	}
}

func TestFormatFloat(t *testing.T) {
	cases := map[string]string{
		formatFloat(100.55, 1):   "100.6",
		formatFloat(0.0, 2):      "0.00",
		formatFloat(-3.14, 2):    "-3.14",
		formatFloat(1234.5, 0):   "1235",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	if formatInt(0) != "0" || formatInt(-42) != "-42" || formatInt(12345) != "12345" {
		t.Errorf("formatInt broken")
	}
}
