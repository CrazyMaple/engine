package dashboard

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"engine/replay"
)

// replayFileInfo 单个回放文件元数据
type replayFileInfo struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
	RoomID    string `json:"room_id,omitempty"`
	Events    int    `json:"events,omitempty"`
	Duration  int64  `json:"duration_ns,omitempty"`
}

// ---- GET /api/replay/list ----
// 列出 ReplayDir 下所有 .replay/.rpl 文件
func (h *handlers) handleReplayList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	dir := h.config.ReplayDir
	if dir == "" {
		writeError(w, http.StatusServiceUnavailable, "replay dir not configured")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := make([]replayFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isReplayFile(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, replayFileInfo{
			Name:      name,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().Format(time.RFC3339),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime > files[j].ModTime })
	writeJSON(w, files)
}

// ---- GET /api/replay/get?name=xxx&meta=1 ----
// 默认作为 application/octet-stream 下载；meta=1 时返回解码后的元数据
func (h *handlers) handleReplayGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	dir := h.config.ReplayDir
	if dir == "" {
		writeError(w, http.StatusServiceUnavailable, "replay dir not configured")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	path, err := safeReplayPath(dir, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if r.URL.Query().Get("meta") == "1" {
		decoded, err := replay.Decode(data)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, replayFileInfo{
			Name:      name,
			SizeBytes: int64(len(data)),
			RoomID:    decoded.RoomID,
			Events:    len(decoded.Events),
			Duration:  decoded.Duration,
		})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(name)+"\"")
	_, _ = io.Copy(w, strings.NewReader(string(data)))
}

// ---- POST/DELETE /api/replay/delete?name=xxx ----
func (h *handlers) handleReplayDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "DELETE or POST only")
		return
	}
	dir := h.config.ReplayDir
	if dir == "" {
		writeError(w, http.StatusServiceUnavailable, "replay dir not configured")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	path, err := safeReplayPath(dir, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "deleted": name})
}

// ---- GET /api/replay/archive/list ----
// 列出所有已归档的回放条目
func (h *handlers) handleReplayArchiveList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.ReplayArchiver == nil {
		writeError(w, http.StatusServiceUnavailable, "archiver not configured")
		return
	}
	writeJSON(w, h.config.ReplayArchiver.List())
}

// ---- POST /api/replay/archive/run ----
// 手动触发一次归档扫描（按策略或指定文件名）
// Body 可选：{"name":"room_123.replay"} — 仅归档单个文件
func (h *handlers) handleReplayArchiveRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	arc := h.config.ReplayArchiver
	if arc == nil {
		writeError(w, http.StatusServiceUnavailable, "archiver not configured")
		return
	}
	name := r.URL.Query().Get("name")
	if name != "" {
		entry, err := arc.ArchiveFile(name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, entry)
		return
	}
	result, err := arc.Run()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, result)
}

// ---- GET /api/replay/archive/fetch?name=xxx ----
// 按需从归档拉取原始回放二进制（自动解压缩）
func (h *handlers) handleReplayArchiveFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	arc := h.config.ReplayArchiver
	if arc == nil {
		writeError(w, http.StatusServiceUnavailable, "archiver not configured")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	data, err := arc.Fetch(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(name)+"\"")
	_, _ = w.Write(data)
}

// ---- DELETE/POST /api/replay/archive/delete?name=xxx ----
// 删除归档数据和索引条目
func (h *handlers) handleReplayArchiveDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "DELETE or POST only")
		return
	}
	arc := h.config.ReplayArchiver
	if arc == nil {
		writeError(w, http.StatusServiceUnavailable, "archiver not configured")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if err := arc.Remove(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok", "deleted": name})
}

func isReplayFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".replay" || ext == ".rpl" || ext == ".bin"
}

// safeReplayPath 防止路径穿越，确保结果仍位于 dir 下
func safeReplayPath(dir, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", errors.New("invalid name")
	}
	abs, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, dirAbs+string(os.PathSeparator)) && abs != dirAbs {
		return "", errors.New("path escape detected")
	}
	return abs, nil
}
