package replay

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ArchiveEntry 归档索引条目
type ArchiveEntry struct {
	// Name 原始文件名（如 room_123.replay）
	Name string `json:"name"`
	// RoomID 房间 ID（从回放元数据中解析，缺失时为空）
	RoomID string `json:"roomId,omitempty"`
	// OriginalSize 原始未压缩字节数
	OriginalSize int64 `json:"originalSize"`
	// ArchivedSize 归档后字节数（含压缩）
	ArchivedSize int64 `json:"archivedSize"`
	// ArchivedAt 归档时间
	ArchivedAt time.Time `json:"archivedAt"`
	// Location 归档位置（由 Sink 决定含义：LocalArchive 为相对 root 的路径；S3 为 s3://bucket/key）
	Location string `json:"location"`
	// Compression 压缩算法（"gzip" / ""）
	Compression string `json:"compression"`
}

// ArchivePolicy 归档触发策略
type ArchivePolicy struct {
	// MaxAge 超过此时长的文件会被归档（0 表示不按时间触发）
	MaxAge time.Duration
	// MinSize 超过此大小的文件会被归档（0 表示不按大小触发）
	MinSize int64
	// Compression 归档时使用的压缩算法（"gzip" 或空）
	Compression string
}

// ArchiveSink 冷数据归档目标抽象
type ArchiveSink interface {
	// Name 返回 Sink 名称（用于日志和索引标注）
	Name() string
	// Put 将 reader 内容写入目标位置 key，返回实际写入字节数
	Put(key string, r io.Reader) (int64, error)
	// Get 按 key 读取内容
	Get(key string) (io.ReadCloser, error)
	// Delete 删除归档
	Delete(key string) error
}

// LocalArchive 将归档写入本地目录（用于测试/小规模部署）
type LocalArchive struct {
	Root string
}

// NewLocalArchive 创建本地归档 Sink
func NewLocalArchive(root string) (*LocalArchive, error) {
	if root == "" {
		return nil, errors.New("root required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalArchive{Root: root}, nil
}

// Name 返回 Sink 名称
func (l *LocalArchive) Name() string { return "local" }

// Put 写入本地文件
func (l *LocalArchive) Put(key string, r io.Reader) (int64, error) {
	dst := filepath.Join(l.Root, key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, r)
}

// Get 读取归档文件
func (l *LocalArchive) Get(key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(l.Root, key))
}

// Delete 删除归档文件
func (l *LocalArchive) Delete(key string) error {
	return os.Remove(filepath.Join(l.Root, key))
}

// ObjectStoreClient S3 / OSS 等对象存储通用客户端抽象
// 由调用方注入以避免对具体 SDK 的直接依赖。
type ObjectStoreClient interface {
	PutObject(bucket, key string, r io.Reader) (int64, error)
	GetObject(bucket, key string) (io.ReadCloser, error)
	DeleteObject(bucket, key string) error
}

// ObjectArchive 基于 ObjectStoreClient 的归档 Sink（S3/OSS 共用）
type ObjectArchive struct {
	Client ObjectStoreClient
	Bucket string
	Prefix string // key 前缀（可选）
	Scheme string // "s3" 或 "oss"，用于构造 Location URI
}

// NewS3Archive 创建 S3 归档（Scheme=s3）
func NewS3Archive(client ObjectStoreClient, bucket, prefix string) *ObjectArchive {
	return &ObjectArchive{Client: client, Bucket: bucket, Prefix: prefix, Scheme: "s3"}
}

// NewOSSArchive 创建阿里云 OSS 归档（Scheme=oss）
func NewOSSArchive(client ObjectStoreClient, bucket, prefix string) *ObjectArchive {
	return &ObjectArchive{Client: client, Bucket: bucket, Prefix: prefix, Scheme: "oss"}
}

// Name 返回归档 Sink 名称
func (o *ObjectArchive) Name() string { return o.Scheme }

// fullKey 拼接 prefix + key
func (o *ObjectArchive) fullKey(key string) string {
	if o.Prefix == "" {
		return key
	}
	return strings.TrimRight(o.Prefix, "/") + "/" + key
}

// Put 上传对象
func (o *ObjectArchive) Put(key string, r io.Reader) (int64, error) {
	if o.Client == nil {
		return 0, errors.New("object store client not configured")
	}
	return o.Client.PutObject(o.Bucket, o.fullKey(key), r)
}

// Get 下载对象
func (o *ObjectArchive) Get(key string) (io.ReadCloser, error) {
	if o.Client == nil {
		return nil, errors.New("object store client not configured")
	}
	return o.Client.GetObject(o.Bucket, o.fullKey(key))
}

// Delete 删除对象
func (o *ObjectArchive) Delete(key string) error {
	if o.Client == nil {
		return errors.New("object store client not configured")
	}
	return o.Client.DeleteObject(o.Bucket, o.fullKey(key))
}

// Archiver 归档协调器：扫描本地目录 + 按策略上传至 Sink + 维护索引
type Archiver struct {
	mu        sync.RWMutex
	LocalDir  string
	IndexFile string
	Sink      ArchiveSink
	Policy    ArchivePolicy
	entries   map[string]ArchiveEntry // name -> entry
}

// NewArchiver 创建归档器
// localDir 为待归档的回放文件目录；indexFile 为索引 JSON 路径；sink 为归档目标。
func NewArchiver(localDir, indexFile string, sink ArchiveSink, policy ArchivePolicy) (*Archiver, error) {
	if localDir == "" {
		return nil, errors.New("localDir required")
	}
	if sink == nil {
		return nil, errors.New("sink required")
	}
	a := &Archiver{
		LocalDir:  localDir,
		IndexFile: indexFile,
		Sink:      sink,
		Policy:    policy,
		entries:   make(map[string]ArchiveEntry),
	}
	if err := a.loadIndex(); err != nil {
		return nil, err
	}
	return a, nil
}

// loadIndex 加载磁盘索引
func (a *Archiver) loadIndex() error {
	if a.IndexFile == "" {
		return nil
	}
	raw, err := os.ReadFile(a.IndexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []ArchiveEntry
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, e := range list {
		a.entries[e.Name] = e
	}
	return nil
}

// saveIndex 持久化索引
func (a *Archiver) saveIndex() error {
	if a.IndexFile == "" {
		return nil
	}
	list := make([]ArchiveEntry, 0, len(a.entries))
	for _, e := range a.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ArchivedAt.After(list[j].ArchivedAt)
	})
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.IndexFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.IndexFile, raw, 0o644)
}

// shouldArchive 判断某文件是否触发归档策略
func (a *Archiver) shouldArchive(info os.FileInfo) bool {
	if a.Policy.MaxAge > 0 && time.Since(info.ModTime()) > a.Policy.MaxAge {
		return true
	}
	if a.Policy.MinSize > 0 && info.Size() >= a.Policy.MinSize {
		return true
	}
	return false
}

// ArchiveResult 单次归档结果汇总
type ArchiveResult struct {
	Archived []string `json:"archived"`
	Skipped  []string `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
}

// Run 按策略扫描本地目录并归档符合条件的文件
// 归档成功后删除本地文件并更新索引。
func (a *Archiver) Run() (ArchiveResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := ArchiveResult{}
	entries, err := os.ReadDir(a.LocalDir)
	if err != nil {
		return result, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isArchivableReplay(name) {
			continue
		}
		if _, ok := a.entries[name]; ok {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		info, err := e.Info()
		if err != nil {
			result.Errors = append(result.Errors, name+": "+err.Error())
			continue
		}
		if !a.shouldArchive(info) {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		entry, err := a.archiveOne(name, info)
		if err != nil {
			result.Errors = append(result.Errors, name+": "+err.Error())
			continue
		}
		a.entries[name] = entry
		result.Archived = append(result.Archived, name)
	}
	if err := a.saveIndex(); err != nil {
		result.Errors = append(result.Errors, "save index: "+err.Error())
	}
	return result, nil
}

// ArchiveFile 手动归档指定文件
func (a *Archiver) ArchiveFile(name string) (ArchiveEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing, ok := a.entries[name]; ok {
		return existing, nil
	}
	info, err := os.Stat(filepath.Join(a.LocalDir, name))
	if err != nil {
		return ArchiveEntry{}, err
	}
	entry, err := a.archiveOne(name, info)
	if err != nil {
		return ArchiveEntry{}, err
	}
	a.entries[name] = entry
	if err := a.saveIndex(); err != nil {
		return entry, err
	}
	return entry, nil
}

// archiveOne 上传单个文件并返回索引条目
func (a *Archiver) archiveOne(name string, info os.FileInfo) (ArchiveEntry, error) {
	src, err := os.Open(filepath.Join(a.LocalDir, name))
	if err != nil {
		return ArchiveEntry{}, err
	}
	defer src.Close()

	// 优先读到内存以便计算压缩后大小；回放文件通常不大
	raw, err := io.ReadAll(src)
	if err != nil {
		return ArchiveEntry{}, err
	}

	var payload []byte
	compression := ""
	key := name
	switch strings.ToLower(a.Policy.Compression) {
	case "", "none":
		payload = raw
	case "gzip":
		buf := &gzipBuffer{}
		gw := gzip.NewWriter(buf)
		if _, err := gw.Write(raw); err != nil {
			return ArchiveEntry{}, err
		}
		if err := gw.Close(); err != nil {
			return ArchiveEntry{}, err
		}
		payload = buf.data
		compression = "gzip"
		key = name + ".gz"
	default:
		return ArchiveEntry{}, fmt.Errorf("unsupported compression: %s", a.Policy.Compression)
	}

	written, err := a.Sink.Put(key, bytesReader(payload))
	if err != nil {
		return ArchiveEntry{}, err
	}

	roomID := parseRoomID(raw)

	entry := ArchiveEntry{
		Name:         name,
		RoomID:       roomID,
		OriginalSize: info.Size(),
		ArchivedSize: written,
		ArchivedAt:   time.Now(),
		Location:     a.buildLocation(key),
		Compression:  compression,
	}

	// 归档成功后清除本地文件
	if err := os.Remove(filepath.Join(a.LocalDir, name)); err != nil {
		return entry, err
	}
	return entry, nil
}

// buildLocation 根据 Sink 类型构造归档位置标识
func (a *Archiver) buildLocation(key string) string {
	switch s := a.Sink.(type) {
	case *LocalArchive:
		return filepath.Join(s.Root, key)
	case *ObjectArchive:
		return fmt.Sprintf("%s://%s/%s", s.Scheme, s.Bucket, s.fullKey(key))
	default:
		return a.Sink.Name() + ":" + key
	}
}

// List 列出所有归档条目
func (a *Archiver) List() []ArchiveEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]ArchiveEntry, 0, len(a.entries))
	for _, e := range a.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ArchivedAt.After(out[j].ArchivedAt)
	})
	return out
}

// Lookup 根据文件名查询归档位置
func (a *Archiver) Lookup(name string) (ArchiveEntry, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	e, ok := a.entries[name]
	return e, ok
}

// Fetch 按需从归档拉取回放数据（自动解压缩）
func (a *Archiver) Fetch(name string) ([]byte, error) {
	a.mu.RLock()
	entry, ok := a.entries[name]
	a.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("not archived: %s", name)
	}

	key := name
	if entry.Compression == "gzip" {
		key = name + ".gz"
	}
	rc, err := a.Sink.Get(key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	if entry.Compression == "gzip" {
		gr, err := gzip.NewReader(rc)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}
	return io.ReadAll(rc)
}

// Remove 同时删除归档和索引条目
func (a *Archiver) Remove(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[name]
	if !ok {
		return fmt.Errorf("not archived: %s", name)
	}
	key := name
	if entry.Compression == "gzip" {
		key = name + ".gz"
	}
	if err := a.Sink.Delete(key); err != nil {
		return err
	}
	delete(a.entries, name)
	return a.saveIndex()
}

// isArchivableReplay 文件是否视为回放文件
func isArchivableReplay(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".replay" || ext == ".rpl" || ext == ".bin"
}

// parseRoomID 从回放二进制尝试解析 RoomID；失败时返回空
func parseRoomID(raw []byte) string {
	data, err := Decode(raw)
	if err != nil {
		return ""
	}
	return data.RoomID
}

// --- 小工具：避免再引入 bytes 包 ---

type gzipBuffer struct {
	data []byte
}

func (b *gzipBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

type bytesReaderImpl struct {
	data []byte
	pos  int
}

func (r *bytesReaderImpl) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func bytesReader(b []byte) io.Reader { return &bytesReaderImpl{data: b} }
