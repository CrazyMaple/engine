package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type ItemConfig struct {
	ID    int    `rf:"index"`
	Name  string
	Price int
	Rare  bool
}

func TestRecordFileLoad(t *testing.T) {
	// 创建临时 TSV 文件
	dir := t.TempDir()
	path := filepath.Join(dir, "items.tsv")
	content := "ID\tName\tPrice\tRare\n1001\t铁剑\t100\ttrue\n1002\t木盾\t50\tfalse\n1003\t药水\t25\ttrue\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rf, err := NewRecordFile(ItemConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if err := rf.Read(path); err != nil {
		t.Fatal(err)
	}

	if rf.NumRecord() != 3 {
		t.Errorf("expected 3 records, got %d", rf.NumRecord())
	}

	item := rf.Record(0).(*ItemConfig)
	if item.ID != 1001 || item.Name != "铁剑" || item.Price != 100 || !item.Rare {
		t.Errorf("unexpected first record: %+v", item)
	}
}

func TestRecordFileIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.tsv")
	content := "ID\tName\tPrice\tRare\n1001\t铁剑\t100\ttrue\n1002\t木盾\t50\tfalse\n"
	os.WriteFile(path, []byte(content), 0644)

	rf, err := NewRecordFile(ItemConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.Read(path); err != nil {
		t.Fatal(err)
	}

	// 通过 ID 索引查找
	item := rf.Index(1002).(*ItemConfig)
	if item.Name != "木盾" {
		t.Errorf("expected 木盾, got %s", item.Name)
	}
}

func TestRecordFileCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.csv")
	content := "ID,Name,Price,Rare\n1,Sword,100,true\n"
	os.WriteFile(path, []byte(content), 0644)

	rf, err := NewRecordFile(ItemConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rf.Comma = ','
	if err := rf.Read(path); err != nil {
		t.Fatal(err)
	}

	if rf.NumRecord() != 1 {
		t.Errorf("expected 1 record, got %d", rf.NumRecord())
	}
}

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func TestJSONConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")

	// Save
	src := &ServerConfig{Host: "0.0.0.0", Port: 8080}
	if err := SaveJSON(path, src); err != nil {
		t.Fatal(err)
	}

	// Load
	dst := &ServerConfig{}
	if err := LoadJSON(path, dst); err != nil {
		t.Fatal(err)
	}

	if dst.Host != "0.0.0.0" || dst.Port != 8080 {
		t.Errorf("unexpected config: %+v", dst)
	}
}

func TestManagerLoadAll(t *testing.T) {
	dir := t.TempDir()

	// 创建 TSV
	tsvPath := filepath.Join(dir, "items.tsv")
	os.WriteFile(tsvPath, []byte("ID\tName\tPrice\tRare\n1\tSword\t100\ttrue\n"), 0644)

	// 创建 JSON
	jsonPath := filepath.Join(dir, "server.json")
	SaveJSON(jsonPath, &ServerConfig{Host: "localhost", Port: 9090})

	mgr := NewManager()
	if err := mgr.RegisterRecordFile(tsvPath, ItemConfig{}, nil); err != nil {
		t.Fatal(err)
	}

	cfg := &ServerConfig{}
	mgr.RegisterJSON(jsonPath, cfg, nil)

	if err := mgr.LoadAll(); err != nil {
		t.Fatal(err)
	}

	// 验证 RecordFile
	entry := mgr.Get(tsvPath)
	if entry.RecordFile.NumRecord() != 1 {
		t.Errorf("expected 1 record, got %d", entry.RecordFile.NumRecord())
	}

	// 验证 JSON
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
}

func TestManagerHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.tsv")
	os.WriteFile(path, []byte("ID\tName\tPrice\tRare\n1\tSword\t100\ttrue\n"), 0644)

	reloaded := make(chan struct{}, 1)
	mgr := NewManager()
	mgr.RegisterRecordFile(path, ItemConfig{}, func() {
		reloaded <- struct{}{}
	})
	mgr.LoadAll()

	mgr.StartWatch(100 * time.Millisecond)
	defer mgr.StopWatch()

	// 等文件系统时间精度
	time.Sleep(200 * time.Millisecond)

	// 修改文件
	os.WriteFile(path, []byte("ID\tName\tPrice\tRare\n1\tSword\t100\ttrue\n2\tShield\t50\tfalse\n"), 0644)

	select {
	case <-reloaded:
		entry := mgr.Get(path)
		if entry.RecordFile.NumRecord() != 2 {
			t.Errorf("expected 2 records after reload, got %d", entry.RecordFile.NumRecord())
		}
	case <-time.After(2 * time.Second):
		t.Error("hot reload timeout")
	}
}
