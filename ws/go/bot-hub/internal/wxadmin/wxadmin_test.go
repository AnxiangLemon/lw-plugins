package wxadmin

import (
	"testing"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/blacklist"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
)

func TestHandleWechatRoomAnnouncementAndRename(t *testing.T) {
	dir := t.TempDir()
	cfgMgr, err := config.Load(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	_ = cfgMgr.Mutate(func(c *config.Config) {
		c.WechatRooms = []config.WechatRoomRule{
			{AccountID: "wxid_bot", RoomID: "43358773958@chatroom", Title: "微信测试群", Enabled: true},
		}
	})
	st := store.New(dir)
	bl := blacklist.New(dir)
	mod := New(dir, cfgMgr, st, bl)

	// 1. 收到 appmsg type=87 多行群公告
	mod.handleWechatRoomMessage("wxid_bot", "43358773958@chatroom", &lwplugin.Message{
		From: lwplugin.Peer{ID: "wxid_owner"},
		Text: `<?xml version="1.0"?><msg><appmsg><type>87</type><textannouncement><![CDATA[第一行公告
第二行公告 4:46]]></textannouncement></appmsg></msg>`,
	})
	meta := mod.roomMeta["wxid_bot|43358773958@chatroom"]
	if meta.Announcement != "第一行公告\n第二行公告 4:46" {
		t.Fatalf("unexpected multiline announcement: %q", meta.Announcement)
	}

	// 2. 收到 sysmsg mmchatroombarannouncememt 更新群公告
	mod.handleWechatRoomMessage("wxid_bot", "43358773958@chatroom", &lwplugin.Message{
		From: lwplugin.Peer{ID: "wxid_owner"},
		Text: `<sysmsg type="mmchatroombarannouncememt"><mmchatroombarannouncememt><content><![CDATA[这是群公告！！！4:46]]></content></mmchatroombarannouncememt></sysmsg>`,
	})
	meta = mod.roomMeta["wxid_bot|43358773958@chatroom"]
	if meta.Announcement != "这是群公告！！！4:46" {
		t.Fatalf("unexpected sysmsg announcement: %q", meta.Announcement)
	}

	// 3. 收到群名称变更提示，自动更新内存与持久化配置
	mod.handleWechatRoomMessage("wxid_bot", "43358773958@chatroom", &lwplugin.Message{
		From: lwplugin.Peer{ID: "43358773958@chatroom"},
		Text: `你修改群名为“微信测试群1124”`,
	})
	meta = mod.roomMeta["wxid_bot|43358773958@chatroom"]
	if meta.Title != "微信测试群1124" {
		t.Fatalf("unexpected meta title: %q", meta.Title)
	}
	if got := cfgMgr.Get().WechatRooms[0].Title; got != "微信测试群1124" {
		t.Fatalf("expected config room title synced, got %q", got)
	}

	// 4. 成员角色分配校验（群主 / 管理员 2048 / 普通成员）
	members := []Member{
		{ID: "wxid_owner"},
		{ID: "wxid_admin", MemberFlag: wxAdminFlagBit},
		{ID: "wxid_normal", MemberFlag: 0},
	}
	assignMemberRoles(members, "wxid_owner")
	if members[0].Role != "owner" || members[1].Role != "admin" || members[2].Role != "member" {
		t.Fatalf("unexpected roles: %+v", members)
	}

	// 5. 验证入退群动态与群缓存持久化（模拟重启后恢复）
	mod.recordChange(MemberChange{
		Time: 1700000000, AccountID: "wxid_bot", RoomID: "43358773958@chatroom",
		RoomTitle: "微信测试群1124", Kind: "joined", MemberID: "wxid_new", MemberName: "新人",
		InviterID: "wxid_owner", InviterName: "群主", MemberCount: 160,
	})
	mod.updateRoomRemark("wxid_bot", "43358773958@chatroom", "我的测试备注")

	restarted := New(dir, cfgMgr, st, bl)
	if len(restarted.changes) != 1 || restarted.changes[0].MemberName != "新人" {
		t.Fatalf("expected persisted changes after restart, got %+v", restarted.changes)
	}
	if got := restarted.roomMeta["wxid_bot|43358773958@chatroom"].Remark; got != "我的测试备注" {
		t.Fatalf("expected persisted remark after restart, got %q", got)
	}
}
