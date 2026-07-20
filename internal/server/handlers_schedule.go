package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// scheduleConfig 晚间标签重算调度配置(读写 schedule_retag_* 两个 settings 键)。
type scheduleConfig struct {
	RetagTime    string `json:"retag_time"`    // "HH:MM" 24 小时零填充
	RetagEnabled bool   `json:"retag_enabled"` // 是否启用晚间重算
}

const (
	settingKeyRetagTime    = "schedule_retag_time"
	settingKeyRetagEnabled = "schedule_retag_enabled"
	defaultRetagTime       = "03:30"
)

// handleGetSchedule GET /api/settings/schedule 读取晚间标签重算调度配置。
func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	cfg := scheduleConfig{RetagTime: defaultRetagTime, RetagEnabled: false}

	if v, err := s.st.GetSetting(settingKeyRetagTime); err == nil {
		if v != "" {
			cfg.RetagTime = v
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("get schedule time failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if v, err := s.st.GetSetting(settingKeyRetagEnabled); err == nil {
		cfg.RetagEnabled = v == "true"
	} else if !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("get schedule enabled failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, cfg)
}

// handleSaveSchedule PUT /api/settings/schedule 写入晚间标签重算调度配置。
func (s *Server) handleSaveSchedule(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	var cfg scheduleConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// 校验 "HH:MM" 零填充格式(与调度器 parseScheduleTime 契约一致)。
	if !validScheduleTime(cfg.RetagTime) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid time format, expected HH:MM (zero-padded)"})
		return
	}

	if err := s.st.SetSetting(settingKeyRetagTime, cfg.RetagTime); err != nil {
		s.logger.Error("save schedule time failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	enabled := "false"
	if cfg.RetagEnabled {
		enabled = "true"
	}
	if err := s.st.SetSetting(settingKeyRetagEnabled, enabled); err != nil {
		s.logger.Error("save schedule enabled failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("schedule saved", "retag_time", cfg.RetagTime, "retag_enabled", cfg.RetagEnabled)
	writeJSON(w, map[string]string{"status": "ok"})
}

// validScheduleTime 校验 "HH:MM" 24 小时零填充格式(00:00..23:59)。
func validScheduleTime(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	h := (int(v[0])-'0')*10 + (int(v[1]) - '0')
	m := (int(v[3])-'0')*10 + (int(v[4]) - '0')
	for _, c := range []byte{v[0], v[1], v[3], v[4]} {
		if c < '0' || c > '9' {
			return false
		}
	}
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}
