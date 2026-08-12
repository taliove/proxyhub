package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// scheduleConfig 夜间调度配置:标签重算(retag_all)+ 全员补齐(batch_exam mode=backfill)。
type scheduleConfig struct {
	RetagTime    string `json:"retag_time"`    // "HH:MM" 24 小时零填充
	RetagEnabled bool   `json:"retag_enabled"` // 是否启用晚间重算
	ExamTime     string `json:"exam_time"`     // 全员补齐时刻,默认与重算错开 30 分钟
	ExamEnabled  bool   `json:"exam_enabled"`  // 是否启用定时全员补齐(默认关,同 ADR 0042 成本纪律)
}

const (
	settingKeyRetagTime    = "schedule_retag_time"
	settingKeyRetagEnabled = "schedule_retag_enabled"
	settingKeyExamTime     = "schedule_exam_time"
	settingKeyExamEnabled  = "schedule_exam_enabled"
	defaultRetagTime       = "03:30"
	defaultExamTime        = "04:00"
)

// handleGetSchedule GET /api/settings/schedule 读取夜间调度配置。
func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		http.Error(w, "storage not initialized", http.StatusServiceUnavailable)
		return
	}

	cfg := scheduleConfig{
		RetagTime: defaultRetagTime,
		ExamTime:  defaultExamTime,
	}

	getStr := func(key string) (string, bool) {
		v, err := s.st.GetSetting(key)
		if err == nil {
			return v, true
		}
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Error("get schedule setting failed", "key", key, "error", err)
			return "", false
		}
		return "", true
	}

	if v, ok := getStr(settingKeyRetagTime); !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if v != "" {
		cfg.RetagTime = v
	}
	if v, ok := getStr(settingKeyRetagEnabled); !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else {
		cfg.RetagEnabled = v == "true"
	}
	if v, ok := getStr(settingKeyExamTime); !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if v != "" {
		cfg.ExamTime = v
	}
	if v, ok := getStr(settingKeyExamEnabled); !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else {
		cfg.ExamEnabled = v == "true"
	}

	writeJSON(w, cfg)
}

// handleSaveSchedule PUT /api/settings/schedule 写入夜间调度配置。
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

	// 兼容老客户端(不带 exam 字段):空 ExamTime 落默认,不作为非法输入。
	if cfg.ExamTime == "" {
		cfg.ExamTime = defaultExamTime
	}

	// 校验 "HH:MM" 零填充格式(与调度器 parseScheduleTime 契约一致)。
	if !validScheduleTime(cfg.RetagTime) || !validScheduleTime(cfg.ExamTime) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid time format, expected HH:MM (zero-padded)"})
		return
	}

	boolStr := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	pairs := [][2]string{
		{settingKeyRetagTime, cfg.RetagTime},
		{settingKeyRetagEnabled, boolStr(cfg.RetagEnabled)},
		{settingKeyExamTime, cfg.ExamTime},
		{settingKeyExamEnabled, boolStr(cfg.ExamEnabled)},
	}
	for _, p := range pairs {
		if err := s.st.SetSetting(p[0], p[1]); err != nil {
			s.logger.Error("save schedule setting failed", "key", p[0], "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	s.logger.Info("schedule saved",
		"retag_time", cfg.RetagTime, "retag_enabled", cfg.RetagEnabled,
		"exam_time", cfg.ExamTime, "exam_enabled", cfg.ExamEnabled)
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
