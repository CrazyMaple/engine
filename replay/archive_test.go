package replay

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// memObjectStore 内存实现的 ObjectStoreClient，用于测试 S3/OSS Sink
type memObjectStore struct {
	mu   sync.Mutex
	data map[string][]byte // bucket/key -> bytes
}

func newMemObjectStore() *memObjectStore {
	return &memObjectStore{data: make(map[string][]byte)}
}

func (m *memObjectStore) PutObject(bucket, key string, r io.Reader) (int64, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.data[bucket+"/"+key] = raw
	m.mu.Unlock()
	return int64(len(raw)), nil
}

func (m *memObjectStore) GetObject(bucket, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	raw, ok := m.data[bucket+"/"+key]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func (m *memObjectStore) DeleteObject(bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[bucket+"/"+key]; !ok {
		return errors.New("not found")
	}
	delete(m.data, bucket+"/"+key)
	return nil
}

// writeReplayFile 生成一个有效的回放二进制到指定路径
func writeReplayFile(t *testing.T, path, roomID string, events int) {
	t.Helper()
	rec := NewRecorder(roomID)
	for i := 0; i < events; i++ {
		rec.Record(uint16(i%5), []byte{byte(i), byte(i + 1)})
	}
	data, err := Encode(rec.Finish())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLocalArchive_PutGetDelete(t *testing.T) {
	root := t.TempDir()
	sink, err := NewLocalArchive(root)
	if err != nil {
		t.Fatalf("new local: %v", err)
	}
	payload := []byte("hello archive")
	n, err := sink.Put("foo.bin", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("n=%d want %d", n, len(payload))
	}
	rc, err := sink.Get("foo.bin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != string(payload) {
		t.Errorf("got %q want %q", got, payload)
	}
	if err := sink.Delete("foo.bin"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := sink.Get("foo.bin"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestArchiver_RunByMinSize(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	indexFile := filepath.Join(t.TempDir(), "index.json")

	writeReplayFile(t, filepath.Join(srcDir, "room1.replay"), "room1", 200)
	writeReplayFile(t, filepath.Join(srcDir, "room2.replay"), "room2", 2)

	sink, _ := NewLocalArchive(archiveDir)
	arc, err := NewArchiver(srcDir, indexFile, sink, ArchivePolicy{
		MinSize:     1024, // room1 (大) 满足; room2 小
		Compression: "gzip",
	})
	if err != nil {
		t.Fatalf("new archiver: %v", err)
	}

	result, err := arc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Archived) != 1 || result.Archived[0] != "room1.replay" {
		t.Errorf("expected [room1.replay] archived, got %v", result.Archived)
	}

	// 归档后本地 room1 被删除，room2 仍在
	if _, err := os.Stat(filepath.Join(srcDir, "room1.replay")); !os.IsNotExist(err) {
		t.Errorf("expected room1.replay removed locally, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "room2.replay")); err != nil {
		t.Errorf("room2.replay should remain: %v", err)
	}

	// 索引文件被写入
	if _, err := os.Stat(indexFile); err != nil {
		t.Errorf("index not written: %v", err)
	}

	// Fetch 回原数据并验证可解码
	raw, err := arc.Fetch("room1.replay")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.RoomID != "room1" {
		t.Errorf("roomID=%s want room1", decoded.RoomID)
	}
	if len(decoded.Events) != 200 {
		t.Errorf("events=%d want 200", len(decoded.Events))
	}

	// 条目可查询
	entry, ok := arc.Lookup("room1.replay")
	if !ok {
		t.Fatal("entry missing")
	}
	if entry.Compression != "gzip" {
		t.Errorf("compression=%s want gzip", entry.Compression)
	}
	if entry.ArchivedSize >= entry.OriginalSize {
		t.Errorf("gzip should shrink: archived=%d original=%d", entry.ArchivedSize, entry.OriginalSize)
	}
}

func TestArchiver_RunByMaxAge(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()

	name := "old.replay"
	writeReplayFile(t, filepath.Join(srcDir, name), "roomOld", 3)
	// 将文件 mtime 改到 2 小时前
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(srcDir, name), past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	sink, _ := NewLocalArchive(archiveDir)
	arc, err := NewArchiver(srcDir, "", sink, ArchivePolicy{
		MaxAge: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	result, err := arc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Archived) != 1 {
		t.Errorf("expected 1 archived, got %v", result.Archived)
	}
}

func TestArchiver_PersistedIndexReload(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	indexFile := filepath.Join(t.TempDir(), "index.json")

	writeReplayFile(t, filepath.Join(srcDir, "r.replay"), "r1", 100)

	sink, _ := NewLocalArchive(archiveDir)
	arc, err := NewArchiver(srcDir, indexFile, sink, ArchivePolicy{MinSize: 1})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := arc.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	// 重新构造 Archiver，应从索引恢复条目
	arc2, err := NewArchiver(srcDir, indexFile, sink, ArchivePolicy{MinSize: 1})
	if err != nil {
		t.Fatalf("new2: %v", err)
	}
	list := arc2.List()
	if len(list) != 1 || list[0].Name != "r.replay" {
		t.Errorf("index not restored: %v", list)
	}
}

func TestArchiver_ObjectStoreSink(t *testing.T) {
	srcDir := t.TempDir()
	writeReplayFile(t, filepath.Join(srcDir, "a.replay"), "roomA", 10)

	store := newMemObjectStore()
	sink := NewS3Archive(store, "mybucket", "replays")
	arc, err := NewArchiver(srcDir, "", sink, ArchivePolicy{MinSize: 1})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := arc.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	list := arc.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].Location != "s3://mybucket/replays/a.replay" {
		t.Errorf("unexpected location: %s", list[0].Location)
	}

	raw, err := arc.Fetch("a.replay")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if decoded, err := Decode(raw); err != nil || decoded.RoomID != "roomA" {
		t.Errorf("decode failed: %v %v", decoded, err)
	}

	if err := arc.Remove("a.replay"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := arc.Lookup("a.replay"); ok {
		t.Error("entry should be removed")
	}
}

func TestArchiver_SkippedWhenAlreadyArchived(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	writeReplayFile(t, filepath.Join(srcDir, "x.replay"), "rx", 5)

	sink, _ := NewLocalArchive(archiveDir)
	arc, _ := NewArchiver(srcDir, "", sink, ArchivePolicy{MinSize: 1})
	if _, err := arc.Run(); err != nil {
		t.Fatalf("run1: %v", err)
	}
	// 再写一个同名文件（归档过的条目应被跳过）
	writeReplayFile(t, filepath.Join(srcDir, "x.replay"), "rx", 5)
	result, err := arc.Run()
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	skipped := false
	for _, n := range result.Skipped {
		if n == "x.replay" {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("expected x.replay in skipped, got %+v", result)
	}
}

func TestArchivePolicy_ShouldArchiveDefault(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	writeReplayFile(t, filepath.Join(srcDir, "p.replay"), "rp", 10)

	sink, _ := NewLocalArchive(archiveDir)
	// 空策略不应归档任何文件
	arc, _ := NewArchiver(srcDir, "", sink, ArchivePolicy{})
	result, _ := arc.Run()
	if len(result.Archived) != 0 {
		t.Errorf("empty policy should archive nothing, got %v", result.Archived)
	}
}
