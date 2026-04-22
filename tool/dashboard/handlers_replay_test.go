package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gamelib/replay"
)

func writeReplayFile(t *testing.T, dir, name string) {
	t.Helper()
	rec := replay.NewRecorder("room-1")
	rec.Record(1, []byte("evt"))
	data, err := replay.Encode(rec.Finish())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleReplayList(t *testing.T) {
	dir := t.TempDir()
	writeReplayFile(t, dir, "a.replay")
	writeReplayFile(t, dir, "b.rpl")
	_ = os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0644)

	h := &handlers{config: Config{ReplayDir: dir}}
	req := httptest.NewRequest(http.MethodGet, "/api/replay/list", nil)
	w := httptest.NewRecorder()
	h.handleReplayList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var got []replayFileInfo
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("want 2 files, got %d", len(got))
	}
}

func TestHandleReplayGetMeta(t *testing.T) {
	dir := t.TempDir()
	writeReplayFile(t, dir, "x.replay")

	h := &handlers{config: Config{ReplayDir: dir}}
	req := httptest.NewRequest(http.MethodGet, "/api/replay/get?name=x.replay&meta=1", nil)
	w := httptest.NewRecorder()
	h.handleReplayGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var meta replayFileInfo
	_ = json.Unmarshal(w.Body.Bytes(), &meta)
	if meta.RoomID != "room-1" || meta.Events != 1 {
		t.Fatalf("metadata mismatch: %+v", meta)
	}
}

func TestHandleReplayDelete(t *testing.T) {
	dir := t.TempDir()
	writeReplayFile(t, dir, "del.replay")

	h := &handlers{config: Config{ReplayDir: dir}}
	req := httptest.NewRequest(http.MethodDelete, "/api/replay/delete?name=del.replay", nil)
	w := httptest.NewRecorder()
	h.handleReplayDelete(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "del.replay")); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
}

func TestSafeReplayPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if _, err := safeReplayPath(dir, "../etc/passwd"); err == nil {
		t.Fatal("expected error for traversal")
	}
	if _, err := safeReplayPath(dir, "ok.replay"); err != nil {
		t.Fatalf("unexpected error for safe name: %v", err)
	}
}
