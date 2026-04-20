package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"engine/leaderboard"
)

// GET /api/leaderboard/season/current?board=xxx
func (h *handlers) handleSeasonCurrent(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	board := r.URL.Query().Get("board")
	if board == "" {
		writeError(w, http.StatusBadRequest, "board required")
		return
	}
	cfg, state, ok := sm.Current(board)
	if !ok {
		writeError(w, http.StatusNotFound, "no season registered for board")
		return
	}
	writeJSON(w, map[string]interface{}{
		"board":      cfg.Board,
		"season_id":  cfg.SeasonID,
		"state":      state.String(),
		"start_at":   cfg.StartAt.Unix(),
		"end_at":     cfg.EndAt.Unix(),
		"carry":      cfg.CarryRatio,
		"rewards_n":  len(cfg.Rewards),
	})
}

// seasonRegisterRequest 注册赛季的 HTTP 入参
type seasonRegisterRequest struct {
	Board             string                         `json:"board"`
	SeasonID          string                         `json:"season_id"`
	StartAtUnix       int64                          `json:"start_at"`
	EndAtUnix         int64                          `json:"end_at"`
	EndingSoonLeadSec int64                          `json:"ending_soon_lead_seconds"`
	CarryRatio        float64                        `json:"carry_ratio"`
	Rewards           []leaderboard.SeasonRewardRule `json:"rewards"`
}

// POST /api/leaderboard/season/register
func (h *handlers) handleSeasonRegister(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req seasonRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := leaderboard.SeasonConfig{
		Board:          req.Board,
		SeasonID:       req.SeasonID,
		StartAt:        time.Unix(req.StartAtUnix, 0),
		EndAt:          time.Unix(req.EndAtUnix, 0),
		EndingSoonLead: time.Duration(req.EndingSoonLeadSec) * time.Second,
		CarryRatio:     req.CarryRatio,
		Rewards:        req.Rewards,
	}
	if err := sm.RegisterSeason(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok"})
}

// POST /api/leaderboard/season/settle  body: {"board":"xxx"}
func (h *handlers) handleSeasonSettle(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Board string `json:"board"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Board == "" {
		writeError(w, http.StatusBadRequest, "board required")
		return
	}
	snap, err := sm.SettleNow(req.Board, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, snap)
}

// GET /api/leaderboard/season/history?board=xxx
func (h *handlers) handleSeasonHistory(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	board := r.URL.Query().Get("board")
	if board == "" {
		writeError(w, http.StatusBadRequest, "board required")
		return
	}
	writeJSON(w, sm.History(board))
}

// GET /api/leaderboard/season/snapshot?board=xxx&season_id=s1
func (h *handlers) handleSeasonSnapshot(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	board := r.URL.Query().Get("board")
	seasonID := r.URL.Query().Get("season_id")
	if board == "" || seasonID == "" {
		writeError(w, http.StatusBadRequest, "board and season_id required")
		return
	}
	snap, ok := sm.GetSnapshot(board, seasonID)
	if !ok {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	writeJSON(w, snap)
}

// GET /api/leaderboard/season/cross?board=xxx&scope=all-time&n=50
func (h *handlers) handleSeasonCrossQuery(w http.ResponseWriter, r *http.Request) {
	sm := h.config.SeasonManager
	if sm == nil {
		writeError(w, http.StatusServiceUnavailable, "season manager not configured")
		return
	}
	board := r.URL.Query().Get("board")
	if board == "" {
		writeError(w, http.StatusBadRequest, "board required")
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all-time"
	}
	n := 100
	if s := r.URL.Query().Get("n"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}
	result := sm.QueryCrossSeason(leaderboard.CrossSeasonQuery{
		Board: board,
		Scope: scope,
		N:     n,
	})
	writeJSON(w, result)
}
