package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"adcenter/internal/store"
)

// eventWriter 异步批量事件落库：决策路径只入队（带界队列，满则丢弃计数），
// 后台协程批量写 ad_events。回执事件量级 = 填充量级，批量写足以消化。
type eventWriter struct {
	ch      chan store.AdEvent
	store   *store.Store
	log     interface{ Warn(string, ...any) }
	dropped int64
}

func newEventWriter(s *store.Store, log interface{ Warn(string, ...any) }) *eventWriter {
	return &eventWriter{ch: make(chan store.AdEvent, 8192), store: s, log: log}
}

// NewEventWriter 构造异步事件写入器（main 装配用）。
func NewEventWriter(s *store.Store, log *slog.Logger) *eventWriter {
	return newEventWriter(s, log)
}

func (w *eventWriter) enqueue(e store.AdEvent) {
	select {
	case w.ch <- e:
	default:
		// 队列满：丢弃并计数（决策路径绝不阻塞；DB 端恢复后由对账修正）
		w.dropped++
	}
}

func (w *eventWriter) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var batch []store.AdEvent
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.store.InsertAdEvents(context.Background(), batch); err != nil {
			w.log.Warn("event batch write failed", "err", err, "n", len(batch))
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			// 排空队列尽力落库
			for {
				select {
				case e := <-w.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		case e := <-w.ch:
			batch = append(batch, e)
			if len(batch) >= 500 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// enqueueEvent Server 快捷方法。
func (s *Server) enqueueEvent(e store.AdEvent) { s.events.enqueue(e) }

// ============================================================
// 管理 API handlers
// ============================================================

func (s *Server) handleListAdvertisers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListAdvertisers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminAdvertiser{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetAdvertiser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	a, err := s.Store.GetAdvertiser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleCreateAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var fields map[string]any
	if !decodeJSON(w, r, &fields) {
		return
	}
	fields["updated_by"] = actor
	id, err := s.Store.CreateAdvertiser(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "advertiser", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var fields map[string]any
	if !decodeJSON(w, r, &fields) {
		return
	}
	fields["updated_by"] = actor
	if err := s.Store.UpdateAdvertiser(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteAdvertiser(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListSlots(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListSlots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminSlot{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminApp{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateApp 注册 App：生成 API Key（原文仅此一次返回），哈希落库。
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	key, prefix, hash, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := s.Store.CreateApp(r.Context(), body.Name, prefix, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "app", id, nil)
	// 原文仅创建时返回一次，之后不可再取
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "api_key": key})
}
