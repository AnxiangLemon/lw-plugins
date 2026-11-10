package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Member struct {
	ID          string `json:"userName"`
	Nickname    string `json:"nickName"`
	DisplayName string `json:"displayName"`
	// InviterID 是微信成员详情直接返回的邀请人，不能从昵称或提示文字猜测。
	// 扫码加入也可能带此字段；空值表示当前协议没有提供可确认的邀请来源。
	InviterID string `json:"inviterUserName,omitempty"`
}

func (m Member) Name() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if m.Nickname != "" {
		return m.Nickname
	}
	return m.ID
}

type Snapshot struct {
	Version uint32   `json:"server_version"`
	Members []Member `json:"members"`
}

// ParseSnapshot 严格验证“完整名单”，避免失败回包、部分资料或空名单造成全员退群。
// chatroomVersion/chatroomInfoVersion 与详情 serverVersion 是不同计数器，不能混用。
func ParseSnapshot(data json.RawMessage, account, room string) (Snapshot, error) {
	var wire struct {
		Base *struct {
			Ret *int `json:"ret"`
		} `json:"baseResponse"`
		Room    string  `json:"chatroomUserName"`
		Version *uint32 `json:"serverVersion"`
		Members *struct {
			Count *uint32 `json:"memberCount"`
			Mask  uint32  `json:"infoMask"`
			Room  *struct {
				String string `json:"string"`
			} `json:"chatRoomUserName"`
			List []Member `json:"chatRoomMember"`
		} `json:"newChatroomData"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return Snapshot{}, fmt.Errorf("成员详情 JSON 无效: %w", err)
	}
	if wire.Base == nil || wire.Base.Ret == nil || *wire.Base.Ret != 0 {
		return Snapshot{}, errors.New("成员详情业务状态缺失或失败，保留原名单")
	}
	if wire.Members == nil || wire.Version == nil || wire.Members.Count == nil || wire.Members.Mask != 0 {
		return Snapshot{}, errors.New("成员资料不完整，保留原名单")
	}
	actual := wire.Room
	if actual == "" && wire.Members.Room != nil {
		actual = wire.Members.Room.String
	}
	if actual != room || (wire.Members.Room != nil && wire.Members.Room.String != "" && wire.Members.Room.String != room) {
		return Snapshot{}, errors.New("返回的群 ID 与请求不一致")
	}
	if *wire.Members.Count == 0 || uint32(len(wire.Members.List)) != *wire.Members.Count {
		return Snapshot{}, errors.New("成员数量与完整名单不一致")
	}
	seen := map[string]bool{}
	self := false
	for _, m := range wire.Members.List {
		if m.ID == "" || strings.TrimSpace(m.ID) != m.ID || seen[m.ID] {
			return Snapshot{}, errors.New("成员 ID 为空或重复")
		}
		seen[m.ID] = true
		self = self || m.ID == account
	}
	if !self {
		return Snapshot{}, errors.New("完整名单中没有当前账号，可能已离群；不计算全员消失")
	}
	sort.Slice(wire.Members.List, func(i, j int) bool { return wire.Members.List[i].ID < wire.Members.List[j].ID })
	return Snapshot{Version: *wire.Version, Members: wire.Members.List}, nil
}
