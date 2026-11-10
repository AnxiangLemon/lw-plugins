package manager

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Room 是群列表的白名单投影。联系人回包内的票据、校验信息不会落到面板数据库。
type Room struct {
	AccountID string `json:"account_id"`
	RoomID    string `json:"room_id"`
	Name      string `json:"name"`
	Remark    string `json:"remark"`
	Owner     string `json:"owner"`
	UpdatedAt string `json:"updated_at"`
}

func migrateDashboard(db *sql.DB, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version < 2 {
		if _, err = tx.Exec(`ALTER TABLE events ADD COLUMN inviter_id TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN inviter_name TEXT NOT NULL DEFAULT '';`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS rooms(account_id TEXT NOT NULL,room_id TEXT NOT NULL,name TEXT NOT NULL,remark TEXT NOT NULL,owner TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(account_id,room_id));
CREATE TABLE IF NOT EXISTS operations(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,room_id TEXT NOT NULL,action TEXT NOT NULL,params TEXT NOT NULL,status TEXT NOT NULL,call_id TEXT NOT NULL DEFAULT '',detail TEXT NOT NULL DEFAULT '',result TEXT NOT NULL DEFAULT '{}',created_at TEXT NOT NULL);
UPDATE operations SET status='unknown',detail='程序重启，需查询原操作结果' WHERE status='running';
PRAGMA user_version=2;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveRooms(rooms []Room) error {
	return s.saveRooms("", rooms)
}

// ReplaceRooms 只在完整分页和资料查询成功后替换该账号的目录。
// 消失的群会从自动列表移除；客户自己添加的记录设置和历史日志仍保留。
func (s *Store) ReplaceRooms(account string, rooms []Room) error {
	return s.saveRooms(account, rooms)
}
func (s *Store) saveRooms(replaceAccount string, rooms []Room) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replaceAccount != "" {
		if _, err = tx.Exec("DELETE FROM rooms WHERE account_id=?", replaceAccount); err != nil {
			return err
		}
	}
	for _, r := range rooms {
		_, err = tx.Exec(`INSERT INTO rooms VALUES(?,?,?,?,?,?) ON CONFLICT(account_id,room_id) DO UPDATE SET name=excluded.name,remark=excluded.remark,owner=excluded.owner,updated_at=excluded.updated_at`, r.AccountID, r.RoomID, r.Name, r.Remark, r.Owner, r.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Rooms(account string) ([]Room, error) {
	rows, err := s.db.Query(`SELECT account_id,room_id,name,remark,owner,updated_at FROM rooms WHERE (?='' OR account_id=?) ORDER BY name,room_id`, account, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Room{}
	for rows.Next() {
		var r Room
		if err = rows.Scan(&r.AccountID, &r.RoomID, &r.Name, &r.Remark, &r.Owner, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type TrendPoint struct {
	Day    string `json:"day"`
	Joined int    `json:"joined"`
	Left   int    `json:"left"`
}
type InviterStat struct {
	AccountID string `json:"account_id"`
	RoomID    string `json:"room_id"`
	ID        string `json:"inviter_id"`
	Name      string `json:"name"`
	Current   int    `json:"current_members"`
	Joined    int    `json:"joined"`
}
type Dashboard struct {
	Joined          int           `json:"joined"`
	Left            int           `json:"left"`
	Members         int           `json:"members"`
	KnownInviters   int           `json:"known_invite_members"`
	UnknownInviters int           `json:"unknown_invite_members"`
	Trend           []TrendPoint  `json:"trend"`
	Inviters        []InviterStat `json:"inviters"`
	Days            int           `json:"days"`
}

// Dashboard 从整个筛选时间段聚合，不能拿页面最近 200 条日志充当完整统计。
// 当前成员来源与“记录期间新增”分开显示：首次读取的历史成员不会冒充新增邀请。
func (s *Store) Dashboard(account, room string, days int) (Dashboard, error) {
	if days != 1 && days != 7 && days != 14 && days != 30 {
		days = 7
	}
	out := Dashboard{Days: days, Trend: []TrendPoint{}, Inviters: []InviterStat{}}
	start := time.Now().In(time.Local)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1-days)
	indices := map[string]int{}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		indices[day] = i
		out.Trend = append(out.Trend, TrendPoint{Day: day})
	}
	stats := map[string]*InviterStat{}
	get := func(a, r, id, name string) *InviterStat {
		k := a + "\x00" + r + "\x00" + id
		if stats[k] == nil {
			stats[k] = &InviterStat{AccountID: a, RoomID: r, ID: id, Name: name}
		}
		if name != "" {
			stats[k].Name = name
		}
		return stats[k]
	}
	rows, err := s.db.Query(`SELECT account_id,room_id,members FROM snapshots WHERE (?='' OR account_id=?) AND (?='' OR room_id=?)`, account, account, room, room)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var a, r, raw string
		if err = rows.Scan(&a, &r, &raw); err != nil {
			rows.Close()
			return out, err
		}
		var members []Member
		if err = json.Unmarshal([]byte(raw), &members); err != nil {
			rows.Close()
			return out, err
		}
		names := map[string]string{}
		for _, m := range members {
			names[m.ID] = m.Name()
		}
		for _, m := range members {
			out.Members++
			if m.InviterID == "" {
				out.UnknownInviters++
				continue
			}
			out.KnownInviters++
			get(a, r, m.InviterID, names[m.InviterID]).Current++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.db.Query(`SELECT account_id,room_id,kind,observed_at,inviter_id,inviter_name FROM events WHERE (?='' OR account_id=?) AND (?='' OR room_id=?) AND observed_at>=? AND kind IN ('joined','left')`, account, account, room, room, start.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a, r, kind, observed, id, name string
		if err = rows.Scan(&a, &r, &kind, &observed, &id, &name); err != nil {
			return out, err
		}
		at, e := time.Parse(time.RFC3339Nano, observed)
		if e != nil {
			continue
		}
		idx, ok := indices[at.In(time.Local).Format("2006-01-02")]
		if !ok {
			continue
		}
		if kind == "joined" {
			out.Joined++
			out.Trend[idx].Joined++
			if id != "" {
				get(a, r, id, name).Joined++
			}
		} else {
			out.Left++
			out.Trend[idx].Left++
		}
	}
	for _, stat := range stats {
		if stat.Name == "" {
			stat.Name = stat.ID
		}
		out.Inviters = append(out.Inviters, *stat)
	}
	return out, rows.Err()
}
