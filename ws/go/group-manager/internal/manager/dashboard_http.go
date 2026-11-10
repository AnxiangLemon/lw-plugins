package manager

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func (m *Manager) dashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		rows, err := m.store.Rooms(r.URL.Query().Get("account_id"))
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, rows)
	})
	mux.HandleFunc("POST /api/discover", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccountID string `json:"account_id"`
		}
		if err := readJSON(w, r, &body); err != nil {
			httpError(w, err)
			return
		}
		if err := m.Discover(body.AccountID); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"queued": true})
	})
	mux.HandleFunc("GET /api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		days, _ := strconv.Atoi(q.Get("days"))
		out, err := m.store.Dashboard(q.Get("account_id"), q.Get("room_id"), days)
		if err != nil {
			httpError(w, err)
			return
		}
		sort.Slice(out.Inviters, func(i, j int) bool {
			a, b := out.Inviters[i], out.Inviters[j]
			if a.Joined != b.Joined {
				return a.Joined > b.Joined
			}
			if a.Current != b.Current {
				return a.Current > b.Current
			}
			if a.AccountID != b.AccountID {
				return a.AccountID < b.AccountID
			}
			if a.RoomID != b.RoomID {
				return a.RoomID < b.RoomID
			}
			return a.ID < b.ID
		})
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/group-members", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			AccountID string `json:"account_id"`
			RoomID    string `json:"room_id"`
		}
		if err := readJSON(w, r, &b); err != nil {
			httpError(w, err)
			return
		}
		if !validRoom(b.RoomID) || strings.HasSuffix(b.RoomID, "@im.chatroom") {
			httpError(w, fmt.Errorf("完整成员明细仅支持普通微信群"))
			return
		}
		raw, err := m.requestData(r.Context(), b.AccountID, "group.get_chat_room_member_detail", map[string]string{"chatRoomId": b.RoomID})
		if err != nil {
			httpError(w, err)
			return
		}
		out, err := ParseSnapshot(raw, b.AccountID, b.RoomID)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/group-info", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			AccountID string `json:"account_id"`
			RoomID    string `json:"room_id"`
		}
		if err := readJSON(w, r, &b); err != nil {
			httpError(w, err)
			return
		}
		if !validRoom(b.RoomID) {
			httpError(w, fmt.Errorf("请选择有效群聊"))
			return
		}
		raw, err := m.requestData(r.Context(), b.AccountID, "group.get_chat_room_info_detail", map[string]string{"chatRoomId": b.RoomID})
		if err != nil {
			httpError(w, err)
			return
		}
		if err = checkBusiness(raw); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, projectOperation("info", raw))
	})
	mux.HandleFunc("GET /api/operations", func(w http.ResponseWriter, r *http.Request) {
		out, err := m.store.Operations(r.URL.Query().Get("account_id"))
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/operation", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID        string   `json:"id"`
			AccountID string   `json:"account_id"`
			RoomID    string   `json:"room_id"`
			Action    string   `json:"action"`
			Content   string   `json:"content"`
			Members   []string `json:"members"`
			Enabled   *bool    `json:"enabled"`
			URL       string   `json:"url"`
		}
		if err := readJSON(w, r, &b); err != nil {
			httpError(w, err)
			return
		}
		out, err := m.SubmitOperation(Operation{ID: b.ID, AccountID: b.AccountID, RoomID: b.RoomID, Action: b.Action, Content: b.Content, Members: b.Members, Enabled: b.Enabled, URL: b.URL})
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/operation-receipt", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID string `json:"id"`
		}
		if err := readJSON(w, r, &b); err != nil {
			httpError(w, err)
			return
		}
		out, err := m.OperationReceipt(r.Context(), b.ID)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, out)
	})
	// 导出同样在数据库筛选；csv.Writer 处理引号，另防止昵称被表格软件当公式执行。
	mux.HandleFunc("GET /api/events.csv", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rows, err := m.store.SearchEvents(q.Get("account_id"), q.Get("room_id"), q.Get("q"), q.Get("kind"), 200)
		if err != nil {
			httpError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="group-events.csv"`)
		_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
		cw := csv.NewWriter(w)
		defer cw.Flush()
		_ = cw.Write([]string{"发现时间", "账号", "群 ID", "类型", "成员 ID", "成员昵称", "邀请人 ID", "邀请人昵称", "变化前人数", "变化后人数", "离线补记"})
		safe := func(s string) string {
			if strings.HasPrefix(s, "=") || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "@") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
				return "'" + s
			}
			return s
		}
		for _, e := range rows {
			_ = cw.Write([]string{e.ObservedAt, safe(e.AccountID), safe(e.RoomID), e.Kind, safe(e.MemberID), safe(e.Name), safe(e.InviterID), safe(e.InviterName), strconv.Itoa(e.Before), strconv.Itoa(e.After), strconv.FormatBool(e.Gap)})
		}
	})
}
