const state = {
  config: { mention_groups: [], monitor_rules: [], telegram_chats: [], wechat_rooms: [] },
  overview: { stats: {}, accounts: [], chats: [], cooldowns: [], logs: [], blacklist: [], wechat_state: { member_counts: {}, room_meta: {}, changes: [], discovered: [] } },
  activeTab: "overview",
  logCategory: "all",
  blPlatform: "all",
  blSearch: "",
  selectedWxAccount: "",
  wxDetail: null, // { account_id, room_id, snap, search, roleFilter, inviterFilter }
};

const TAB_META = {
  overview: ["数据概览", "统一管理多平台账号、群聊风控、成员进退群动态与消息转发"],
  wechat: ["微信群管", "同步微信普通群、精确监控新人入群（含邀请人）与离群动态、管理群公告与成员"],
  tg: ["Telegram 群管", "进群人机验证、入群欢迎、违禁词拦截、警告处罚与群内 /panel 面板同步"],
  monitor: ["消息监控与转发", "支持已接入平台间的同平台与跨平台自由转发，按触发条件、消息类型与冷却策略精准控制"],
  groups: ["人员分组 (@)", "把常用群成员或值班人员打包成统一名单，在多条转发规则中一键复用"],
  blacklist: ["黑名单管理", "全平台共用黑名单库：一处拉黑、全群与全账号通用，静默拦截仅记日志"],
  logs: ["实时审计日志", "查看各平台群管操作、成员变动、监控转发与冷却拦截的完整历史记录"],
};

const PLATFORM_META = {
  wechat: { label: "微信", short: "微", targetPlaceholder: "目标微信群 ID 或 wxid（如 123456@chatroom）" },
  telegram: { label: "Telegram", short: "TG", targetPlaceholder: "目标 Telegram 群或用户 ID（如 -1001234567890）" },
  qq: { label: "QQ", short: "QQ", targetPlaceholder: "目标 QQ 群号或用户 ID" },
  feishu: { label: "飞书", short: "飞", targetPlaceholder: "目标飞书群或用户 ID" },
  dingtalk: { label: "钉钉", short: "钉", targetPlaceholder: "目标钉钉群或用户 ID" },
  discord: { label: "Discord", short: "DC", targetPlaceholder: "目标频道或用户 ID" },
};

function getAvailablePlatforms(extra = []) {
  const seen = new Set();
  const out = [];
  const add = (p) => {
    const key = String(p || "").trim().toLowerCase();
    if (!key || seen.has(key)) return;
    seen.add(key);
    out.push(key);
  };
  // 默认保留已内置支持的核心平台，同时自动合并宿主同步来的新平台
  add("wechat");
  add("telegram");
  (state.overview.accounts || []).forEach((a) => add(a.platform));
  (extra || []).forEach(add);
  return out;
}

function platformTargetPlaceholder(p) {
  const key = String(p || "").toLowerCase();
  return (PLATFORM_META[key] && PLATFORM_META[key].targetPlaceholder) || "输入目标群 ID 或会话 ID";
}

const MSG_TYPE_OPTIONS = [
  { key: "text", label: "文字" },
  { key: "image", label: "图片" },
  { key: "video", label: "视频" },
  { key: "voice", label: "语音" },
  { key: "file", label: "文件" },
  { key: "link", label: "链接" },
  { key: "other", label: "其他" },
];
const MSG_TYPE_LABEL = Object.fromEntries(MSG_TYPE_OPTIONS.map((x) => [x.key, x.label]));

const LOG_CAT_META = {
  all: ["全部", ""],
  wx_admin: ["微信群管", "emerald"],
  tg_admin: ["TG 群管", "sky"],
  monitor: ["监控转发", "primary"],
  cooldown: ["冷却拦截", "amber"],
  system: ["系统", "violet"],
};

const $ = (id) => document.getElementById(id);

function esc(s) {
  return String(s ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function splitCSV(s) {
  return (s || "").split(/[,，\n]/).map((x) => x.trim()).filter(Boolean);
}

function fmtTime(ts) {
  if (!ts) return "--:--:--";
  return new Date(ts * 1000).toLocaleTimeString("zh-CN", { hour12: false });
}

function fmtDuration(sec) {
  sec = Math.max(0, Number(sec) || 0);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}小时 ${m}分`;
  if (m > 0) return `${m}分 ${s}秒`;
  return `${s}秒`;
}

function emptyHTML(title, sub = "", asCard = false) {
  return `
    <div class="empty-box ${asCard ? "empty-card" : ""}" ${asCard ? 'style="grid-column:1/-1"' : ""}>
      <div class="empty-ico" aria-hidden="true">
        <svg class="ico-sm" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
      </div>
      <div class="empty-title">${esc(title)}</div>
      ${sub ? `<div class="empty-sub">${esc(sub)}</div>` : ""}
    </div>
  `;
}

// ── Sonner 风格 Toast（百分比入场 + 可中断过渡 + 悬停暂停计时） ──
function toast(msg, type = "ok") {
  const el = document.createElement("div");
  el.className = `toast ${type}`;
  el.innerHTML = `<span class="toast-dot" aria-hidden="true"></span><span>${esc(msg)}</span>`;
  $("toastContainer").appendChild(el);

  requestAnimationFrame(() => {
    requestAnimationFrame(() => el.classList.add("show"));
  });

  let remain = 3200;
  let startedAt = Date.now();
  let timer = null;

  const dismiss = () => {
    el.classList.remove("show");
    el.classList.add("leaving");
    setTimeout(() => el.remove(), 220);
  };
  const schedule = () => {
    startedAt = Date.now();
    timer = setTimeout(dismiss, remain);
  };
  el.addEventListener("mouseenter", () => {
    if (timer) {
      clearTimeout(timer);
      timer = null;
      remain = Math.max(600, remain - (Date.now() - startedAt));
    }
  });
  el.addEventListener("mouseleave", () => {
    if (!timer && !el.classList.contains("leaving")) schedule();
  });
  schedule();
}

async function api(path, opts = {}) {
  const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...opts });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function setBtnLoading(btn, loading) {
  if (!btn) return;
  btn.classList.toggle("is-loading", loading);
  btn.disabled = loading;
}

function syncBodyModalLock() {
  const anyOpen =
    $("modalBackdrop").classList.contains("open") ||
    $("confirmBackdrop").classList.contains("open");
  document.body.classList.toggle("modal-open", anyOpen);
}

// ── 定制确认弹窗（取代原生 confirm，可中断过渡） ──
let confirmResolver = null;
function closeConfirm(result) {
  const backdrop = $("confirmBackdrop");
  backdrop.classList.remove("open");
  backdrop.setAttribute("aria-hidden", "true");
  syncBodyModalLock();
  if (confirmResolver) {
    const fn = confirmResolver;
    confirmResolver = null;
    fn(result);
  }
}

function askConfirm(title, desc, okText = "确认") {
  return new Promise((resolve) => {
    confirmResolver = resolve;
    $("confirmTitle").textContent = title;
    $("confirmDesc").textContent = desc;
    $("confirmOk").textContent = okText;
    const backdrop = $("confirmBackdrop");
    backdrop.classList.add("open");
    backdrop.setAttribute("aria-hidden", "false");
    syncBodyModalLock();
    $("confirmCancel").onclick = () => closeConfirm(false);
    $("confirmOk").onclick = () => closeConfirm(true);
  });
}

// ── 定制下拉组件（Origin-aware Popover，无横向溢出，防止轮询打断展开态） ──
function closeAllSelects(except = null) {
  document.querySelectorAll(".cselect.open").forEach((x) => {
    if (x !== except) {
      x.classList.remove("open");
      const btn = x.querySelector(".cselect-trigger");
      if (btn) btn.setAttribute("aria-expanded", "false");
    }
  });
}

function mountSelect(el, options, value, onChange) {
  if (!el) return;
  const sig = JSON.stringify({ options, value });
  if (el.classList.contains("open") && el.dataset.sig === sig) {
    return;
  }
  el.dataset.sig = sig;

  const wasOpen = el.classList.contains("open");
  const placeholder = el.dataset.placeholder || "请选择";
  const current = options.find((o) => o.value === value);

  const renderTriggerContent = () => {
    if (!current) {
      return `<span class="cselect-value-text cselect-placeholder">${esc(placeholder)}</span>`;
    }
    return `
      ${current.dot ? `<span class="status-dot ${esc(current.dot)}" aria-hidden="true"></span>` : ""}
      <span class="cselect-value-text" title="${esc(current.label)}">${esc(current.label)}</span>
      ${current.tag ? `<span class="cselect-opt-tag">${esc(current.tag)}</span>` : ""}
    `;
  };

  el.innerHTML = `
    <button type="button" class="cselect-trigger" aria-haspopup="listbox" aria-expanded="${wasOpen ? "true" : "false"}">
      <span class="cselect-value">${renderTriggerContent()}</span>
      <svg class="ico-sm cselect-chevron" viewBox="0 0 24 24"><polyline points="6 9 12 15 18 9"/></svg>
    </button>
    <div class="cselect-menu" role="listbox">
      ${
        options.length
          ? options
              .map(
                (o) => `
          <button type="button" class="cselect-opt ${o.value === value ? "selected" : ""}" role="option" aria-selected="${o.value === value ? "true" : "false"}" data-val="${esc(o.value)}">
            <span class="cselect-opt-body">
              <span class="cselect-opt-main">
                ${o.dot ? `<span class="status-dot ${esc(o.dot)}" aria-hidden="true"></span>` : ""}
                <span class="cselect-opt-label" title="${esc(o.label)}">${esc(o.label)}</span>
                ${o.tag ? `<span class="cselect-opt-tag">${esc(o.tag)}</span>` : ""}
              </span>
              ${o.sub ? `<span class="cselect-opt-sub ${o.dot ? "" : "no-dot"}" title="${esc(o.sub)}">${esc(o.sub)}</span>` : ""}
            </span>
            <svg class="ico-sm cselect-opt-check" viewBox="0 0 24 24"><polyline points="20 6 9 17 4 12"/></svg>
          </button>`
              )
              .join("")
          : `<div class="cselect-empty">暂无可选项</div>`
      }
    </div>
  `;

  if (wasOpen) el.classList.add("open");

  const trigger = el.querySelector(".cselect-trigger");
  trigger.onclick = (e) => {
    e.stopPropagation();
    const isOpen = el.classList.contains("open");
    closeAllSelects(el);
    el.classList.toggle("open", !isOpen);
    trigger.setAttribute("aria-expanded", !isOpen ? "true" : "false");
  };

  el.querySelectorAll(".cselect-opt").forEach((opt) => {
    opt.onclick = (e) => {
      e.stopPropagation();
      el.classList.remove("open");
      trigger.setAttribute("aria-expanded", "false");
      const nextVal = opt.dataset.val;
      onChange(nextVal);
      mountSelect(el, options, nextVal, onChange);
    };
  });
}

document.addEventListener("click", () => closeAllSelects());

// ── 定制开关辅助 ──
function setToggle(btn, on) {
  btn.classList.toggle("on", !!on);
  btn.setAttribute("aria-pressed", on ? "true" : "false");
}
function isToggleOn(btn) {
  return btn.classList.contains("on");
}

function setBadgeCount(id, count) {
  const el = $(id);
  if (!el) return;
  const n = Number(count) || 0;
  el.textContent = n;
  el.classList.toggle("is-zero", n === 0);
}

// ── 加载与保存 ──
async function loadAll(withFeedback = false) {
  try {
    const [cfg, ov] = await Promise.all([api("/api/config"), api("/api/overview")]);
    state.config = cfg;
    state.overview = ov;
    renderAll();
    if (state.activeTab === "logs") {
      await reloadLogs(false);
    }
    if (withFeedback) toast("数据已刷新");
  } catch (e) {
    toast("加载数据失败: " + e.message, "error");
  }
}

async function saveConfig(msg = "配置已保存并立即生效") {
  await api("/api/config", { method: "PUT", body: JSON.stringify(state.config) });
  toast(msg);
  renderAll();
}

function renderAll() {
  renderOverview();
  renderWechatPanel();
  renderTgChats();
  renderMonitorRules();
  renderGroups();
  renderGlobalBlacklist();
  setBadgeCount("badgeWx", (state.config.wechat_rooms || []).length);
  setBadgeCount("badgeTg", (state.config.telegram_chats || []).length);
  setBadgeCount("badgeMonitor", (state.config.monitor_rules || []).length);
  setBadgeCount("badgeGroups", (state.config.mention_groups || []).length);
  setBadgeCount("badgeBlacklist", (state.overview.blacklist || []).length);
}

function accountDisplayName(a) {
  if (!a) return "";
  if (a.nickname) return a.nickname;
  if (a.username) return `@${a.username.replace(/^@/, "")}`;
  return a.id || "";
}

function accountAvatarHTML(a) {
  const key = String(a.platform || "").toLowerCase();
  const meta = PLATFORM_META[key];
  const fallback = meta ? meta.short : (key.slice(0, 2).toUpperCase() || "?");
  const platClass = key === "wechat" ? "wechat" : "telegram";
  const imgHTML = a.avatar
    ? `<img class="avatar-img" src="${esc(a.avatar)}" alt="" referrerpolicy="no-referrer" onerror="this.style.display='none';this.nextElementSibling.style.display='grid'" /><span class="avatar-fallback" style="display:none">${esc(fallback)}</span>`
    : `<span class="avatar-fallback">${esc(fallback)}</span>`;
  return `
    <div class="account-avatar ${platClass}" title="${esc(formatPlatformName(key))}">
      ${imgHTML}
      <span class="avatar-dot ${a.online ? "online" : "offline"}" title="${a.online ? "在线" : "离线"}"></span>
    </div>`;
}

function formatPlatformName(p) {
  const key = String(p || "").toLowerCase();
  if (PLATFORM_META[key]) return PLATFORM_META[key].label;
  return p || "";
}

// ── 概览 ──
function renderOverview() {
  const st = state.overview.stats || {};
  const accs = state.overview.accounts || [];
  const onlineCount = accs.filter((a) => a.online).length;
  $("connDot").className = `status-dot ${st.connected ? "online" : "offline"}`;
  $("connText").textContent = st.connected ? "宿主已连接" : "等待连接宿主";
  $("connSub").textContent = st.connected
    ? accs.length
      ? `${onlineCount} / ${accs.length} 个账号在线`
      : "LW Tools 已就绪"
    : "请在 LW Tools 允许接入";
  if ($("connCard")) {
    $("connCard").title = st.connected
      ? `已连接到 LW Tools${st.host_version ? " " + st.host_version : ""}`
      : "等待连接到 LW Tools 宿主";
  }

  $("statMessages").textContent = st.messages_seen || 0;
  $("statUptime").textContent = `运行时长 ${fmtDuration(st.uptime_sec)}`;
  $("statAlerts").textContent = st.alerts_sent || 0;
  $("statSuppressed").textContent = st.alerts_suppressed || 0;
  $("statAdmin").textContent = st.admin_actions || 0;

  $("accountCountBadge").textContent = `${accs.length} 个账号`;
  $("accountsList").innerHTML = accs.length
    ? accs
        .map((a) => {
          const displayName = accountDisplayName(a);
          const hasProfile = !!(a.nickname || a.username);
          return `
      <div class="account-row">
        ${accountAvatarHTML(a)}
        <div class="account-main">
          <div class="account-name">
            <span class="account-name-text" title="${esc(displayName)}">${esc(displayName)}</span>
            ${a.username && a.nickname ? `<span class="account-username">@${esc(a.username.replace(/^@/, ""))}</span>` : ""}
            ${a.online ? "" : `<span class="badge amber">离线</span>`}
            ${!hasProfile ? `<span class="badge muted">未同步昵称</span>` : ""}
          </div>
          <div class="account-id" title="${esc(a.id)}">${esc(a.id)}</div>
        </div>
        <button type="button" class="btn btn-ghost btn-sm" data-copy-id="${esc(a.id)}">
          <span class="btn-blur-label">复制 ID</span>
        </button>
      </div>`;
        })
        .join("")
    : emptyHTML("尚未检测到宿主账号", "请先在 LW Tools 中登录并启用对应平台账号");

  const cds = state.overview.cooldowns || [];
  $("btnClearCooldowns").disabled = cds.length === 0;
  $("cooldownsList").innerHTML = cds.length
    ? cds
        .map(
          (c) => `
      <div class="cooldown-row">
        <span class="cooldown-key">${esc(c.key)}</span>
        <span class="badge amber">剩余 ${fmtDuration(c.remain_sec)} · 拦截 ${c.suppressed} 次</span>
      </div>`
        )
        .join("")
    : emptyHTML("当前没有处于冷却中的监控目标", "规则触发防刷屏冷却后，活跃窗口会在此实时倒计时");

  const overviewLogs = state.overview.logs || [];
  $("btnClearOverviewLogs").disabled = overviewLogs.length === 0;
  renderLogList($("overviewLogs"), overviewLogs);
}

// 复制文本 + Emil Kowalski Blur 状态过渡反馈
function triggerCopyButtonFeedback(btn) {
  const label = btn.querySelector(".btn-blur-label");
  if (!label) return;
  if (btn._copyTimer) clearTimeout(btn._copyTimer);
  label.classList.add("is-blurring");
  setTimeout(() => {
    label.textContent = "已复制";
    btn.classList.add("btn-copied");
    label.classList.remove("is-blurring");
  }, 90);
  btn._copyTimer = setTimeout(() => {
    label.classList.add("is-blurring");
    setTimeout(() => {
      label.textContent = "复制 ID";
      btn.classList.remove("btn-copied");
      label.classList.remove("is-blurring");
    }, 90);
  }, 1500);
}

document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-copy-id]");
  if (!btn) return;
  const txt = btn.getAttribute("data-copy-id") || "";
  if (!txt) return;
  navigator.clipboard
    ?.writeText(txt)
    .then(() => {
      triggerCopyButtonFeedback(btn);
      toast("已复制账号 ID");
    })
    .catch(() => toast("复制失败，请手动选中复制", "error"));
});

function renderLogList(container, logs) {
  if (!logs.length) {
    container.innerHTML = emptyHTML("暂无动态记录", "后续群管操作、入退群变动与监控转发记录将实时显示在这里");
    return;
  }
  container.innerHTML = logs
    .map((l) => {
      const [label, cls] = LOG_CAT_META[l.category] || [l.category || "日志", ""];
      const badgeCls = l.level === "error" ? "rose" : l.level === "warn" ? "amber" : cls;
      const source = l.chat_title || l.chat_id || l.platform || "";
      return `
      <div class="log-row ${l.level === "error" ? "is-error" : ""}">
        <span class="log-time">${fmtTime(l.time)}</span>
        <span class="log-cat-cell"><span class="badge ${badgeCls}">${esc(label)}</span></span>
        <div class="log-body">
          <div class="log-summary">
            <span>${esc(l.summary)}</span>
            ${l.rule_name ? `<span class="badge muted">${esc(l.rule_name)}</span>` : ""}
          </div>
          ${l.detail ? `<div class="log-detail">${esc(l.detail)}</div>` : ""}
        </div>
        ${source ? `<span class="badge muted log-source" title="${esc(source)}">${esc(source)}</span>` : "<span></span>"}
      </div>`;
    })
    .join("");
}

// ── 微信群管 ──
function renderWechatPanel() {
  const wxAccounts = (state.overview.accounts || []).filter((a) => a.platform === "wechat");
  if ((!state.selectedWxAccount || !wxAccounts.some((a) => a.id === state.selectedWxAccount)) && wxAccounts.length) {
    const online = wxAccounts.find((a) => a.online) || wxAccounts[0];
    state.selectedWxAccount = online.id;
  }
  const activeAccount = wxAccounts.find((a) => a.id === state.selectedWxAccount) || null;
  if ($("btnWxDiscover")) {
    $("btnWxDiscover").disabled = !activeAccount || !activeAccount.online;
    $("btnWxDiscover").title = activeAccount && !activeAccount.online ? "当前微信账号离线，无法同步通讯录" : "";
  }
  if ($("btnAddWxRoom")) {
    $("btnAddWxRoom").disabled = !activeAccount;
  }

  mountSelect(
    $("wxAccountSelect"),
    wxAccounts.map((a) => ({
      value: a.id,
      label: accountDisplayName(a),
      sub: a.id,
      dot: a.online ? "online" : "offline",
      tag: a.online ? "" : "离线",
    })),
    state.selectedWxAccount,
    (v) => {
      state.selectedWxAccount = v;
      if (state.wxDetail && state.wxDetail.account_id !== v) {
        closeWxDetailView();
      }
      renderWechatPanel();
    }
  );

  const allRooms = state.config.wechat_rooms || [];
  const filteredRooms = state.selectedWxAccount
    ? allRooms
        .map((r, idx) => ({ r, idx }))
        .filter(({ r }) => r.account_id === state.selectedWxAccount)
    : [];

  const wxState = state.overview.wechat_state || {};
  const counts = wxState.member_counts || {};
  const metas = wxState.room_meta || {};

  if (!wxAccounts.length) {
    $("wxRoomsGrid").innerHTML = emptyHTML(
      "尚未检测到微信账号",
      "请先在 LW Tools 登录微信账号后刷新",
      true
    );
  } else if (!filteredRooms.length) {
    $("wxRoomsGrid").innerHTML = emptyHTML(
      "当前账号尚未添加微信监控群",
      activeAccount && activeAccount.online
        ? "点击上方「同步通讯录群列表」或「添加微信监控群」开启入群/退群监控与群管理"
        : "当前选中的微信账号处于离线状态，可切换其他在线账号或先添加群配置",
      true
    );
  } else {
    $("wxRoomsGrid").innerHTML = filteredRooms
      .map(({ r, idx }) => {
        const key = `${r.account_id}|${r.room_id}`;
        const cnt = counts[key];
        const meta = metas[key] || {};
        const displayTitle = meta.remark || meta.title || r.title || r.room_id;
        const showRealNameSub = meta.remark && meta.title && meta.remark !== meta.title;
        const roleBadge =
          meta.self_role === "owner"
            ? `<span class="badge emerald">群主</span>`
            : meta.self_role === "admin"
            ? `<span class="badge primary">管理员</span>`
            : meta.self_role === "member"
            ? `<span class="badge muted">仅监控（普通成员）</span>`
            : "";
        return `
        <div class="rule-card">
          <div class="rule-top">
            <div class="rule-head-copy">
              <div class="rule-title" title="${esc(displayTitle)}">${esc(displayTitle)}</div>
              <div class="rule-sub">${esc(r.room_id)}${showRealNameSub ? ` · 群名: ${esc(meta.title)}` : ""}</div>
            </div>
            <button type="button" class="toggle-btn ${r.enabled ? "on" : ""}" aria-pressed="${r.enabled ? "true" : "false"}" onclick="toggleWxRoom(${idx})"><span class="toggle-knob"></span></button>
          </div>
          <div class="tags">
            ${roleBadge}
            <span class="badge emerald">成员 ${cnt != null ? cnt + " 人" : "待核对"}</span>
            <span class="badge ${r.welcome_enabled ? "primary" : "muted"}">欢迎 ${r.welcome_enabled ? "开" : "关"}</span>
            <span class="badge ${r.goodbye_enabled ? "amber" : "muted"}">离群 ${r.goodbye_enabled ? "开" : "关"}</span>
            <span class="badge ${r.blacklist_enabled ? "rose" : "muted"}">黑名单 ${r.blacklist_enabled ? (r.blacklist_on_leave ? "开（含离群）" : "开") : "关"}</span>
            <span class="badge muted">核对 ${r.poll_interval_sec >= 60 ? `每 ${fmtDuration(r.poll_interval_sec)} 兜底` : "仅实时变动触发"}</span>
          </div>
          ${meta.announcement ? `<div class="wx-ann-preview" title="${esc(meta.announcement)}">群公告：${esc(meta.announcement)}</div>` : ""}
          <div class="rule-actions">
            <button type="button" class="btn btn-outline btn-sm" onclick="checkWxRoom(${idx})">核对名单</button>
            <button type="button" class="btn btn-outline btn-sm" onclick="openWxManageModal(${idx})">成员与群管理</button>
            <button type="button" class="btn btn-ghost btn-sm" onclick="openWxRoomModal(${idx})">配置</button>
            <button type="button" class="btn btn-danger btn-sm" onclick="deleteWxRoom(${idx})">移除</button>
          </div>
        </div>`;
      })
      .join("");
  }

  const allChanges = wxState.changes || [];
  const changes = state.selectedWxAccount
    ? allChanges.filter((ch) => ch.account_id === state.selectedWxAccount)
    : allChanges;
  $("btnClearWxChanges").disabled = allChanges.length === 0;
  $("wxChangesList").innerHTML = changes.length
    ? changes
        .map(
          (ch) => `
      <div class="log-row">
        <span class="log-time">${fmtTime(ch.time)}</span>
        <span class="log-cat-cell"><span class="badge ${ch.kind === "joined" ? "emerald" : "rose"}">${ch.kind === "joined" ? "新人入群" : "成员离群"}</span></span>
        <div class="log-body">
          <div class="log-summary">
            <span>${esc(ch.member_name)}</span>
            <span class="badge muted">${esc(ch.member_id)}</span>
          </div>
          <div class="log-detail">${ch.inviter_name ? `邀请人: ${esc(ch.inviter_name)} · ` : ""}当前群人数 ${ch.member_count}</div>
        </div>
        <span class="badge muted log-source" title="${esc(ch.room_title || ch.room_id)}">${esc(ch.room_title || ch.room_id)}</span>
      </div>`
        )
        .join("")
    : emptyHTML("当前账号暂无入群 / 离群变动记录", "首次核对仅建立成员基线，后续变动会自动持久化并实时呈现");
}

window.toggleWxRoom = async (idx) => {
  state.config.wechat_rooms[idx].enabled = !state.config.wechat_rooms[idx].enabled;
  await saveConfig("微信群监控状态已更新");
};

window.checkWxRoom = async (idx) => {
  const r = state.config.wechat_rooms[idx];
  try {
    const snap = await api("/api/wechat/check", {
      method: "POST",
      body: JSON.stringify({ account_id: r.account_id, room_id: r.room_id }),
    });
    toast(`核对完成：共 ${snap.member_count} 名成员`);
    loadAll();
  } catch (e) {
    toast("核对失败: " + e.message, "error");
  }
};

window.deleteWxRoom = async (idx) => {
  if (!(await askConfirm("移除微信监控群", "确定停止监控并移除这个微信群的配置吗？", "移除"))) return;
  state.config.wechat_rooms.splice(idx, 1);
  await saveConfig("已移除微信监控群");
};

window.openWxRoomModal = (idx = -1) => {
  const isNew = idx < 0;
  const targetAccountID = isNew ? state.selectedWxAccount || "" : state.config.wechat_rooms[idx].account_id;
  if (!targetAccountID) {
    toast("请先在顶部选择一个微信账号", "error");
    return;
  }

  const r = isNew
    ? {
        account_id: targetAccountID,
        room_id: "",
        title: "",
        enabled: true,
        welcome_enabled: false,
        welcome_template: "欢迎 {name} 加入 {group}！当前群人数：{count}",
        goodbye_enabled: false,
        goodbye_template: "{name} 离开了 {group}，当前人数：{count}",
        poll_interval_sec: 0,
        blacklist_enabled: false,
        blacklist_on_leave: false,
      }
    : JSON.parse(JSON.stringify(state.config.wechat_rooms[idx]));

  const existingIDs = new Set(
    (state.config.wechat_rooms || [])
      .filter((x) => x.account_id === targetAccountID)
      .map((x) => x.room_id)
  );
  const discovered = ((state.overview.wechat_state && state.overview.wechat_state.discovered) || []).filter(
    (d) => d.account_id === targetAccountID && (!isNew || !existingIDs.has(d.room_id))
  );
  const quickRooms = discovered
    .slice(0, 16)
    .map(
      (d) => `<button type="button" class="pill-toggle" data-fill-room="${esc(d.room_id)}" data-fill-title="${esc(d.title)}">${esc(d.title)}</button>`
    )
    .join("");

  let pickedTitle = r.title || "";

  openModal(
    isNew ? "添加微信监控群" : "配置微信监控群",
    `<div class="form-grid">
      <div class="field full">
        <label for="wxRoomId">微信群 ID *（必须以 @chatroom 结尾）</label>
        <input type="text" id="wxRoomId" value="${esc(r.room_id)}" placeholder="例如 43358773958@chatroom" ${isNew ? "" : "disabled"} />
        <span class="field-hint">群名称与群备注会在核对名单时自动从微信获取，无需手动填写。</span>
      </div>
      ${isNew && quickRooms ? `<div class="field full"><label>从当前账号已同步的通讯录群快速选择</label><div class="pill-group" id="quickWxRooms">${quickRooms}</div></div>` : ""}
      <div class="field full">
        <label for="wxPoll">定时兜底检查间隔（秒）</label>
        <input type="number" id="wxPoll" value="${r.poll_interval_sec || 0}" min="0" />
        <span class="field-hint">填 0 表示仅在微信推送群变动提醒时核对（推荐）；填 ≥60 秒则会按间隔额外定时检查，防止漏报。</span>
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div><div class="toggle-row-title">新人入群发送欢迎消息</div><div class="toggle-row-sub">可用变量：{name} {wxid} {inviter} {group} {count}</div></div>
          <button type="button" class="toggle-btn ${r.welcome_enabled ? "on" : ""}" id="wxWelcomeBtn" aria-pressed="${r.welcome_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full" id="wxWelcomeTplField" style="${r.welcome_enabled ? "" : "display:none"}">
        <label for="wxWelcomeTpl">入群欢迎模板</label>
        <input type="text" id="wxWelcomeTpl" value="${esc(r.welcome_template || "")}" />
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div><div class="toggle-row-title">成员离群发送提示消息</div><div class="toggle-row-sub">可用变量：{name} {wxid} {group} {count}</div></div>
          <button type="button" class="toggle-btn ${r.goodbye_enabled ? "on" : ""}" id="wxGoodbyeBtn" aria-pressed="${r.goodbye_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full" id="wxGoodbyeTplField" style="${r.goodbye_enabled ? "" : "display:none"}">
        <label for="wxGoodbyeTpl">离群提示模板</label>
        <input type="text" id="wxGoodbyeTpl" value="${esc(r.goodbye_template || "")}" />
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div><div class="toggle-row-title">开启黑名单系统（全群通用、静默拦截）</div><div class="toggle-row-sub">通过本面板移出的成员自动入黑名单；黑名单成员再次入群时，管理员账号自动静默移出（不发群消息，仅记日志）</div></div>
          <button type="button" class="toggle-btn ${r.blacklist_enabled ? "on" : ""}" id="wxBlacklistBtn" aria-pressed="${r.blacklist_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full" id="wxBlacklistOnLeaveField" style="${r.blacklist_enabled ? "" : "display:none"}">
        <div class="toggle-row">
          <div><div class="toggle-row-title">成员离群也自动加入黑名单（可选）</div><div class="toggle-row-sub">开启后，普通成员离群（退群或被移出）都会自动加入全平台微信黑名单</div></div>
          <button type="button" class="toggle-btn ${r.blacklist_on_leave ? "on" : ""}" id="wxBlacklistOnLeaveBtn" aria-pressed="${r.blacklist_on_leave ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
    </div>`,
    async () => {
      const room_id = $("wxRoomId").value.trim();
      if (!/^[A-Za-z0-9_.-]+@chatroom$/.test(room_id)) {
        throw new Error("微信群 ID 格式不正确，必须以 @chatroom 结尾（例如 43358773958@chatroom）");
      }
      if (isNew && existingIDs.has(room_id)) {
        throw new Error("当前账号已添加过该微信群，请勿重复添加");
      }
      const pollVal = Math.max(0, Number($("wxPoll").value) || 0);
      if (pollVal > 0 && pollVal < 60) {
        throw new Error("定时兜底检查间隔填写非 0 时不能低于 60 秒");
      }
      const next = {
        ...r,
        account_id: targetAccountID,
        room_id,
        title: pickedTitle || r.title || "",
        welcome_enabled: isToggleOn($("wxWelcomeBtn")),
        welcome_template: $("wxWelcomeTpl").value,
        goodbye_enabled: isToggleOn($("wxGoodbyeBtn")),
        goodbye_template: $("wxGoodbyeTpl").value,
        poll_interval_sec: pollVal,
        blacklist_enabled: isToggleOn($("wxBlacklistBtn")),
        blacklist_on_leave: isToggleOn($("wxBlacklistOnLeaveBtn")),
      };
      if (isNew) state.config.wechat_rooms.push(next);
      else state.config.wechat_rooms[idx] = next;
      await saveConfig("微信监控群配置已保存");
      if (next.enabled) {
        api("/api/wechat/check", { method: "POST", body: JSON.stringify({ account_id: targetAccountID, room_id }) }).then(loadAll).catch(() => {});
      }
    }
  );

  $("wxWelcomeBtn").onclick = () => {
    const nextOn = !isToggleOn($("wxWelcomeBtn"));
    setToggle($("wxWelcomeBtn"), nextOn);
    $("wxWelcomeTplField").style.display = nextOn ? "" : "none";
  };
  $("wxGoodbyeBtn").onclick = () => {
    const nextOn = !isToggleOn($("wxGoodbyeBtn"));
    setToggle($("wxGoodbyeBtn"), nextOn);
    $("wxGoodbyeTplField").style.display = nextOn ? "" : "none";
  };
  $("wxBlacklistBtn").onclick = () => {
    const nextOn = !isToggleOn($("wxBlacklistBtn"));
    setToggle($("wxBlacklistBtn"), nextOn);
    $("wxBlacklistOnLeaveField").style.display = nextOn ? "" : "none";
  };
  $("wxBlacklistOnLeaveBtn").onclick = () => setToggle($("wxBlacklistOnLeaveBtn"), !isToggleOn($("wxBlacklistOnLeaveBtn")));

  const quickWrap = $("quickWxRooms");
  if (quickWrap) {
    quickWrap.querySelectorAll("[data-fill-room]").forEach((btn) => {
      btn.onclick = () => {
        $("wxRoomId").value = btn.dataset.fillRoom || "";
        pickedTitle = btn.dataset.fillTitle || "";
        quickWrap.querySelectorAll("[data-fill-room]").forEach((x) => x.classList.remove("on"));
        btn.classList.add("on");
      };
    });
  }
};

function memberAvatarHTML(avatar, name, small = false) {
  const ch = (name || "?").trim().slice(0, 1).toUpperCase();
  const cls = small ? "mem-avatar mem-avatar-sm" : "mem-avatar";
  if (avatar) {
    return `<div class="${cls}"><img src="${esc(avatar)}" alt="" loading="lazy" referrerpolicy="no-referrer" onerror="this.remove();this.parentNode.textContent='${esc(ch)}'" /></div>`;
  }
  return `<div class="${cls}">${esc(ch)}</div>`;
}

window.closeWxDetailView = () => {
  state.wxDetail = null;
  $("wxDetailView").style.display = "none";
  $("wxDetailView").innerHTML = "";
  $("wxListView").style.display = "";
};

window.openWxManageModal = async (idx, forceRefresh = false) => {
  const r = state.config.wechat_rooms[idx];
  if (!r) return;
  state.wxDetail = {
    idx,
    account_id: r.account_id,
    room_id: r.room_id,
    snap: null,
    search: "",
    roleFilter: "all",
    inviterFilter: "",
  };
  $("wxListView").style.display = "none";
  $("wxDetailView").style.display = "";
  $("wxDetailView").innerHTML = `<div class="card">${emptyHTML("正在读取群成员缓存与资料...")}</div>`;

  await loadWxDetailSnapshot(forceRefresh);
};

async function loadWxDetailSnapshot(forceRefresh = false) {
  const d = state.wxDetail;
  if (!d) return;
  const refreshBtn = $("btnWxDetailRefresh");
  if (refreshBtn) setBtnLoading(refreshBtn, true);
  try {
    const url = `/api/wechat/members?account_id=${encodeURIComponent(d.account_id)}&room_id=${encodeURIComponent(d.room_id)}${forceRefresh ? "&refresh=1" : ""}`;
    const snap = await api(url);
    d.snap = snap;
    renderWxDetailView();
    if (forceRefresh) {
      toast(`已核对最新名单（共 ${(snap.members || []).length} 人），头像将在后台自动缓存`);
      loadAll(false);
    }
  } catch (e) {
    if (!d.snap) {
      $("wxDetailView").innerHTML = `
        <div class="wx-detail-bar">
          <button type="button" class="btn btn-outline" onclick="closeWxDetailView()">← 返回微信群列表</button>
        </div>
        <div class="card">${emptyHTML("加载群成员失败", e.message)}</div>`;
    } else {
      toast("核对更新失败: " + e.message, "error");
    }
  } finally {
    if (refreshBtn) setBtnLoading(refreshBtn, false);
  }
}

function renderWxDetailView() {
  const d = state.wxDetail;
  if (!d || !d.snap) return;
  const snap = d.snap;
  const meta = snap.meta || {};
  const members = snap.members || [];
  const r = (state.config.wechat_rooms || []).find((x) => x.account_id === d.account_id && x.room_id === d.room_id) || {};
  const liveTitle = meta.title || r.title || d.room_id;

  // 构建成员映射表，用于将邀请人 wxid 映射为真实昵称与头像
  const memMap = new Map();
  members.forEach((m) => memMap.set(m.userName, m));
  const memDisplay = (m) => (m && (m.displayName || m.nickName || m.userName)) || "";

  // 统计邀请排行榜
  const inviterCounts = new Map();
  members.forEach((m) => {
    if (!m.inviterUserName) return;
    inviterCounts.set(m.inviterUserName, (inviterCounts.get(m.inviterUserName) || 0) + 1);
  });
  const inviterRank = Array.from(inviterCounts.entries())
    .map(([id, count]) => {
      const inv = memMap.get(id);
      return {
        id,
        count,
        name: inv ? memDisplay(inv) : id,
        avatar: inv ? inv.avatar || inv.smallHeadImgUrl || "" : "",
      };
    })
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));

  let roleBannerHTML = "";
  const canManage = meta.can_manage === true;
  if (meta.self_role === "owner" || meta.self_role === "admin") {
    const roleName = meta.self_role === "owner" ? "群主" : "群管理员";
    roleBannerHTML = `<div class="wx-role-banner is-admin"><span>当前账号为 <b>${roleName}</b>，可编辑群公告、设置群备注与移出成员</span><span>上次缓存：${fmtTime(snap.updated_at)}</span></div>`;
  } else if (meta.self_role === "member") {
    roleBannerHTML = `<div class="wx-role-banner is-readonly"><span>当前账号为 <b>普通成员（仅监控模式）</b>：群公告与移出成员仅限群主或管理员操作（已设为只读），群聊备注可随时修改</span><span>上次缓存：${fmtTime(snap.updated_at)}</span></div>`;
  } else {
    roleBannerHTML = `<div class="wx-role-banner is-unknown"><span>权限状态待核对（点击右上角「核对名单并更新缓存」同步最新权限与头像）</span><span>上次缓存：${fmtTime(snap.updated_at)}</span></div>`;
  }

  $("wxDetailView").innerHTML = `
    <div class="wx-detail-bar">
      <div class="wx-detail-bar-left">
        <button type="button" class="btn btn-outline" onclick="closeWxDetailView()">← 返回群列表</button>
        <div>
          <div class="wx-detail-title">${esc(liveTitle)}${meta.remark ? ` <span class="badge muted" style="vertical-align:middle;font-weight:500">备注: ${esc(meta.remark)}</span>` : ""}</div>
          <div class="wx-detail-sub">${esc(d.room_id)} · 共 ${members.length} 人 · 账号 ${esc(d.account_id)}</div>
        </div>
      </div>
      <button type="button" class="btn btn-primary" id="btnWxDetailRefresh">
        <svg class="ico-sm ico-spin" viewBox="0 0 24 24"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
        <span>核对名单并更新缓存</span>
      </button>
    </div>

    ${roleBannerHTML}

    <div class="grid-2">
      <div class="card">
        <div class="card-head" style="margin-bottom:10px">
          <div>
            <h3>群公告（支持多行）</h3>
            <p>${canManage ? "群主/管理员可直接编辑并发布到群聊" : "当前账号无管理员权限，仅只读展示"}</p>
          </div>
          ${canManage ? `<button type="button" class="btn btn-outline btn-sm" id="btnWxSetAnn">发布公告</button>` : ""}
        </div>
        <div class="field">
          <textarea id="wxAnnInput" rows="4" ${canManage ? "" : "disabled"} placeholder="${canManage ? "输入群公告内容（支持多行换行）" : "暂无群公告"}">${esc(meta.announcement || "")}</textarea>
        </div>
      </div>

      <div class="card">
        <div class="card-head" style="margin-bottom:10px">
          <div>
            <h3>群聊备注（仅自己可见）</h3>
            <p>修改该微信账号视角下的群聊备注，不会改动群名称、不打扰群成员</p>
          </div>
          <button type="button" class="btn btn-outline btn-sm" id="btnWxSetRemark">保存备注</button>
        </div>
        <div class="field">
          <input type="text" id="wxRemarkInput" value="${esc(meta.remark || "")}" placeholder="输入仅自己可见的群聊备注（留空可清空备注）" />
          <span class="field-hint">为避免误操作，已禁用机器人修改公开群名称功能，仅保留群备注修改。</span>
        </div>
      </div>
    </div>

    <div class="wx-workspace-grid">
      <div class="card card-flush-body">
        <div class="card-head">
          <div>
            <h3>邀请统计排行</h3>
            <p>点击任意邀请人筛选其邀请的成员</p>
          </div>
          <span class="soft-badge">${inviterRank.length} 位</span>
        </div>
        <div class="inviter-list" id="wxInviterList">
          <button type="button" class="inviter-item ${d.inviterFilter === "" ? "active" : ""}" data-inviter="">
            <span class="inviter-item-left">
              <span class="inviter-name">全部成员</span>
            </span>
            <span class="badge muted">${members.length} 人</span>
          </button>
          ${
            inviterRank.length
              ? inviterRank
                  .map(
                    (inv) => `
            <button type="button" class="inviter-item ${d.inviterFilter === inv.id ? "active" : ""}" data-inviter="${esc(inv.id)}">
              <span class="inviter-item-left">
                ${memberAvatarHTML(inv.avatar, inv.name, true)}
                <span style="min-width:0;flex:1">
                  <div class="inviter-name" title="${esc(inv.name)}">${esc(inv.name)}</div>
                  <div class="inviter-sub" title="${esc(inv.id)}">${esc(inv.id)}</div>
                </span>
              </span>
              <span class="badge emerald">邀 ${inv.count} 人</span>
            </button>`
                  )
                  .join("")
              : `<div class="empty-box"><div class="empty-sub">暂无邀请人统计数据</div></div>`
          }
        </div>
      </div>

      <div class="card card-flush-body">
        <div class="card-head">
          <div>
            <h3 id="wxMemberPanelTitle">群成员名单</h3>
            <p>头像自动缓存至本地磁盘（data/bot-hub/cache/img），仅在变动或手动核对时更新</p>
          </div>
          <span class="soft-badge" id="wxMemberFilteredBadge">${members.length} 人</span>
        </div>
        <div class="member-search-bar">
          <input type="text" class="member-search-input" id="wxMemberSearch" value="${esc(d.search)}" placeholder="搜索成员昵称、群名片、wxid 或邀请人..." />
          <div class="segmented" id="wxRoleFilterSeg">
            <button type="button" class="seg-btn ${d.roleFilter === "all" ? "active" : ""}" data-val="all">全部</button>
            <button type="button" class="seg-btn ${d.roleFilter === "managers" ? "active" : ""}" data-val="managers">群主/管理</button>
            <button type="button" class="seg-btn ${d.roleFilter === "invited" ? "active" : ""}" data-val="invited">有邀请人</button>
          </div>
        </div>
        <div class="member-workspace-list" id="wxMemberWorkspaceList"></div>
      </div>
    </div>
  `;

  $("btnWxDetailRefresh").onclick = () => loadWxDetailSnapshot(true);

  if (canManage && $("btnWxSetAnn")) {
    $("btnWxSetAnn").onclick = async () => {
      const val = $("wxAnnInput").value.trim();
      if (!val) return toast("请输入公告内容", "error");
      const btn = $("btnWxSetAnn");
      setBtnLoading(btn, true);
      try {
        await api("/api/wechat/action", {
          method: "POST",
          body: JSON.stringify({ account_id: d.account_id, room_id: d.room_id, action: "set_announcement", value: val }),
        });
        toast("群公告已发布并同步");
        d.snap.meta.announcement = val;
        loadAll(false);
      } catch (e) {
        toast(e.message, "error");
      } finally {
        setBtnLoading(btn, false);
      }
    };
  }

  $("btnWxSetRemark").onclick = async () => {
    const val = $("wxRemarkInput").value.trim();
    const btn = $("btnWxSetRemark");
    setBtnLoading(btn, true);
    try {
      await api("/api/wechat/action", {
        method: "POST",
        body: JSON.stringify({ account_id: d.account_id, room_id: d.room_id, action: "set_remarks", value: val }),
      });
      toast(val ? "群聊备注已保存（仅自己可见）" : "群聊备注已清空");
      d.snap.meta.remark = val;
      renderWxDetailView();
      loadAll(false);
    } catch (e) {
      toast(e.message, "error");
    } finally {
      setBtnLoading(btn, false);
    }
  };

  $("wxInviterList").querySelectorAll("[data-inviter]").forEach((btn) => {
    btn.onclick = () => {
      d.inviterFilter = btn.dataset.inviter || "";
      $("wxInviterList").querySelectorAll("[data-inviter]").forEach((x) => x.classList.toggle("active", (x.dataset.inviter || "") === d.inviterFilter));
      renderWxDetailMembers(memMap, memDisplay, canManage);
    };
  });

  $("wxRoleFilterSeg").querySelectorAll(".seg-btn").forEach((btn) => {
    btn.onclick = () => {
      d.roleFilter = btn.dataset.val;
      $("wxRoleFilterSeg").querySelectorAll(".seg-btn").forEach((x) => x.classList.toggle("active", x.dataset.val === d.roleFilter));
      renderWxDetailMembers(memMap, memDisplay, canManage);
    };
  });

  $("wxMemberSearch").oninput = (e) => {
    d.search = e.target.value;
    renderWxDetailMembers(memMap, memDisplay, canManage);
  };

  renderWxDetailMembers(memMap, memDisplay, canManage);
}

function renderWxDetailMembers(memMap, memDisplay, canManage) {
  const d = state.wxDetail;
  if (!d || !d.snap) return;
  const members = d.snap.members || [];
  const q = (d.search || "").trim().toLowerCase();

  const filtered = members.filter((m) => {
    if (d.inviterFilter && m.inviterUserName !== d.inviterFilter) return false;
    if (d.roleFilter === "managers" && m.role !== "owner" && m.role !== "admin") return false;
    if (d.roleFilter === "invited" && !m.inviterUserName) return false;
    if (q) {
      const inv = m.inviterUserName ? memMap.get(m.inviterUserName) : null;
      const hay = [m.displayName, m.nickName, m.userName, m.inviterUserName, inv ? memDisplay(inv) : ""]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      if (!hay.includes(q)) return false;
    }
    return true;
  });

  const invName = d.inviterFilter ? memDisplay(memMap.get(d.inviterFilter)) || d.inviterFilter : "";
  $("wxMemberPanelTitle").textContent = d.inviterFilter ? `由「${invName}」邀请的成员` : "群成员名单";
  $("wxMemberFilteredBadge").textContent = `${filtered.length} / ${members.length} 人`;

  const box = $("wxMemberWorkspaceList");
  if (!filtered.length) {
    box.innerHTML = emptyHTML("没有匹配的群成员", "可尝试清空搜索关键词或切换左侧邀请人筛选");
    return;
  }

  box.innerHTML = filtered
    .map((m) => {
      const name = memDisplay(m);
      const avatar = m.avatar || m.smallHeadImgUrl || "";
      const roleTag =
        m.role === "owner"
          ? `<span class="badge emerald">群主</span>`
          : m.role === "admin"
          ? `<span class="badge primary">管理员</span>`
          : "";
      const isSelf = m.userName === d.account_id;
      const showKick = canManage && !isSelf && m.role !== "owner";
      let invText = "";
      if (m.inviterUserName) {
        const inv = memMap.get(m.inviterUserName);
        const invLabel = inv ? `${memDisplay(inv)} (${m.inviterUserName})` : m.inviterUserName;
        invText = `<div style="font-size:11.5px;color:var(--emerald);margin-top:2px">邀请人: ${esc(invLabel)}</div>`;
      }
      return `
      <div class="member-item-row">
        <div class="member-item-left">
          ${memberAvatarHTML(avatar, name, false)}
          <div style="min-width:0;flex:1">
            <div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;font-weight:600;color:var(--text)">
              <span>${esc(name)}</span>
              ${roleTag}
              ${isSelf ? `<span class="badge sky">本账号</span>` : ""}
              <span style="font-weight:400;font-size:12px;color:var(--text-3);font-family:var(--mono)">${esc(m.userName)}</span>
            </div>
            ${invText}
          </div>
        </div>
        ${
          showKick
            ? `<div style="display:flex;gap:6px;flex-shrink:0">
                <button type="button" class="btn btn-outline btn-sm" onclick="kickWxMember('${esc(d.account_id)}','${esc(d.room_id)}','${esc(m.userName)}',false)">移出</button>
                <button type="button" class="btn btn-danger btn-sm" onclick="kickWxMember('${esc(d.account_id)}','${esc(d.room_id)}','${esc(m.userName)}',true)">移出并拉黑</button>
              </div>`
            : ""
        }
      </div>`;
    })
    .join("");
}

window.kickWxMember = async (accountID, roomID, userID, withBlacklist = false) => {
  const title = withBlacklist ? "移出并加入全局黑名单" : "移出群成员";
  const desc = withBlacklist
    ? `确定将 ${userID} 移出该微信群，并加入全平台微信黑名单吗？（一处拉黑，全群全账号通用）`
    : `确定将 ${userID} 移出该微信群吗？`;
  if (!(await askConfirm(title, desc, withBlacklist ? "移出并拉黑" : "移出"))) return;
  try {
    await api("/api/wechat/action", {
      method: "POST",
      body: JSON.stringify({ account_id: accountID, room_id: roomID, action: withBlacklist ? "kick_blacklist" : "kick", value: userID }),
    });
    toast(withBlacklist ? "已移出并加入全局黑名单" : "已提交移出操作");
    await loadAll(false);
  } catch (e) { toast(e.message, "error"); }
};

// ── Telegram 群管 ──
function tgBotPermBadges(cs) {
  if (!cs || !cs.bot_status) {
    return `<span class="badge muted" title="点击「检测权限」查询机器人是否为该群管理员">权限待检测</span>`;
  }
  if (cs.bot_status === "creator" || cs.bot_status === "administrator") {
    const missing = [];
    if (!cs.can_restrict) missing.push("限制成员");
    if (!cs.can_delete) missing.push("删除消息");
    if (missing.length) {
      return `<span class="badge amber" title="机器人是管理员，但缺少权限：${esc(missing.join("、"))}">管理员（缺：${esc(missing.join("/"))}）</span>`;
    }
    return `<span class="badge emerald">机器人：管理员</span>`;
  }
  return `<span class="badge rose" title="机器人当前不是群管理员，进群验证禁言、删违规消息和踢人将无法执行">非管理员（功能受限）</span>`;
}

function renderTgChats() {
  const list = state.config.telegram_chats || [];
  const tgState = (state.overview && state.overview.tg_state) || {};
  const chatMap = Object.fromEntries((tgState.chats || []).map((x) => [x.chat_id, x]));
  const mutedList = tgState.muted || [];
  const warnList = tgState.warnings || [];

  const mutedByChat = {};
  mutedList.forEach((m) => {
    mutedByChat[m.chat_id] = (mutedByChat[m.chat_id] || 0) + 1;
  });
  const warnByChat = {};
  warnList.forEach((w) => {
    warnByChat[w.chat_id] = (warnByChat[w.chat_id] || 0) + 1;
  });

  if (!list.length) {
    $("tgChatsGrid").innerHTML = emptyHTML("暂无 TG 群管策略", "点击右上角「添加 TG 群策略」开始配置进群验证、违禁词与警告规则", true);
  } else {
    $("tgChatsGrid").innerHTML = list
      .map((c, idx) => {
        const isGlobal = c.chat_id === "*";
        const cs = !isGlobal ? chatMap[c.chat_id] : null;
        const displayTitle = c.title || (cs && cs.title) || (isGlobal ? "全局默认策略" : c.chat_id);
        const mCnt = !isGlobal ? mutedByChat[c.chat_id] || 0 : 0;
        const wCnt = !isGlobal ? warnByChat[c.chat_id] || 0 : 0;
        return `
        <div class="rule-card">
          <div class="rule-top">
            <div class="rule-head-copy">
              <div class="rule-title" title="${esc(displayTitle)}">${esc(displayTitle)}</div>
              <div class="rule-sub">${isGlobal ? "全局默认（对未单独配置的群生效）" : esc(c.chat_id)}</div>
            </div>
            <button type="button" class="toggle-btn ${c.enabled ? "on" : ""}" aria-pressed="${c.enabled ? "true" : "false"}" onclick="toggleTgChat(${idx})"><span class="toggle-knob"></span></button>
          </div>
          <div class="tags">
            ${!isGlobal ? tgBotPermBadges(cs) : `<span class="badge sky">全局兜底</span>`}
            ${mCnt > 0 ? `<span class="badge rose">禁言中 ${mCnt} 人</span>` : ""}
            ${wCnt > 0 ? `<span class="badge amber">有警告 ${wCnt} 人</span>` : ""}
            <span class="badge ${c.verify_enabled ? "emerald" : "muted"}">进群验证 ${c.verify_enabled ? `${c.verify_timeout_sec || 120}s / ${c.verify_max_attempts || 3}次` : "关"}</span>
            <span class="badge ${c.welcome_enabled ? "primary" : "muted"}">入群欢迎 ${c.welcome_enabled ? "开" : "关"}</span>
            <span class="badge ${c.goodbye_enabled ? "sky" : "muted"}">退群提示 ${c.goodbye_enabled ? "开" : "关"}</span>
            <span class="badge ${c.block_links ? "amber" : "muted"}">拦外链 ${c.block_links ? "开" : "关"}</span>
            <span class="badge ${c.auto_approve_join ? "violet" : "muted"}">自动审群 ${c.auto_approve_join ? "开" : "关"}</span>
            <span class="badge ${c.blacklist_enabled ? "rose" : "muted"}">黑名单 ${c.blacklist_enabled ? (c.blacklist_on_leave ? "开（含退群）" : "开") : "关"}</span>
            <span class="badge muted">违禁词 ${(c.banned_keywords || []).length} 个</span>
            <span class="badge muted">警告 ${c.warn_limit || 3} 次→${c.warn_action === "ban" ? "踢出" : `禁言 ${c.warn_mute_minutes || 60} 分`}</span>
          </div>
          <div class="rule-actions">
            ${!isGlobal ? `<button type="button" class="btn btn-outline btn-sm" onclick="checkTgPerms('${esc(c.chat_id)}', '${esc((cs && cs.account_id) || "")}', this)">检测权限</button>` : ""}
            <button type="button" class="btn btn-outline btn-sm" onclick="openTgModal(${idx})">配置策略</button>
            ${!isGlobal ? `<button type="button" class="btn btn-danger btn-sm" onclick="deleteTgChat(${idx})">删除</button>` : ""}
          </div>
        </div>`;
      })
      .join("");
  }

  // 渲染当前受限（禁言）与警告成员列表
  const totalSanctions = mutedList.length + warnList.length;
  $("tgSanctionCountBadge").textContent = `${totalSanctions} 条记录`;
  if (!totalSanctions) {
    $("tgSanctionsList").innerHTML = emptyHTML(
      "当前没有被禁言或有警告记录的成员",
      "若成员因进群验证失败、触发外链/违禁词警告而被禁言，会自动显示在这里供你一键解禁或清零警告"
    );
  } else {
    const rows = [];
    mutedList.forEach((m) => {
      const untilText = m.until > 0 ? `自动解禁：${fmtTime(m.until)}` : "不限时（需管理员手动解禁）";
      rows.push(`
      <div class="log-row">
        <span class="log-time">${fmtTime(m.muted_at)}</span>
        <span class="log-cat-cell"><span class="badge rose">禁言中</span></span>
        <div class="log-body">
          <div class="log-summary">
            <span>${esc(m.name || String(m.user_id))}</span>
            <span class="badge muted" style="font-family:var(--mono)">${esc(String(m.user_id))}</span>
            <span class="badge muted">${esc(m.chat_title || m.chat_id)}</span>
          </div>
          <div class="log-detail">${esc(m.reason || "已限制发言")} · ${esc(untilText)}</div>
        </div>
        <button type="button" class="btn btn-outline btn-sm" onclick="tgQuickAction('${esc(m.account_id || "")}','${esc(m.chat_id)}',${Number(m.user_id)},'unmute',this)">一键解禁</button>
      </div>`);
    });
    warnList.forEach((w) => {
      rows.push(`
      <div class="log-row">
        <span class="log-time">${fmtTime(w.updated_at)}</span>
        <span class="log-cat-cell"><span class="badge amber">警告 ${w.count} 次</span></span>
        <div class="log-body">
          <div class="log-summary">
            <span>${esc(w.name || String(w.user_id))}</span>
            <span class="badge muted" style="font-family:var(--mono)">${esc(String(w.user_id))}</span>
            <span class="badge muted">${esc(w.chat_title || w.chat_id)}</span>
          </div>
          <div class="log-detail">最近原因：${esc(w.last_reason || "违规")}（解禁/重新入群不自动清零）</div>
        </div>
        <button type="button" class="btn btn-ghost btn-sm" onclick="tgQuickAction('${esc(w.account_id || "")}','${esc(w.chat_id)}',${Number(w.user_id)},'unwarn',this)">清零警告</button>
      </div>`);
    });
    $("tgSanctionsList").innerHTML = rows.join("");
  }
}

window.checkTgPerms = async (chatID, accountID, btn) => {
  if (btn) setBtnLoading(btn, true);
  try {
    const res = await api("/api/telegram/check-perms", {
      method: "POST",
      body: JSON.stringify({ account_id: accountID || "", chat_id: chatID }),
    });
    const isAdmin = res.bot_status === "administrator" || res.bot_status === "creator";
    if (isAdmin) {
      const missing = [];
      if (!res.can_restrict) missing.push("限制成员");
      if (!res.can_delete) missing.push("删除消息");
      toast(missing.length ? `机器人是管理员，但缺少：${missing.join("、")}` : `权限正常：机器人是「${res.title || chatID}」的管理员`);
    } else {
      toast(`机器人当前不是管理员（状态：${res.bot_status || "未知"}），请在 TG 群设置中将其设为管理员`, "error");
    }
    await loadAll(false);
  } catch (e) {
    toast("权限检测失败: " + e.message, "error");
  } finally {
    if (btn) setBtnLoading(btn, false);
  }
};

window.tgQuickAction = async (accountID, chatID, userID, action, btn) => {
  if (btn) setBtnLoading(btn, true);
  try {
    await api("/api/telegram/action", {
      method: "POST",
      body: JSON.stringify({ account_id: accountID || "", chat_id: chatID, user_id: Number(userID), action }),
    });
    const msgMap = {
      unmute: `已解除用户 ${userID} 的禁言（警告次数保留）`,
      unwarn: `已清零用户 ${userID} 的警告记录`,
    };
    toast(msgMap[action] || "操作成功");
    await loadAll(false);
  } catch (e) {
    toast("操作失败: " + e.message, "error");
  } finally {
    if (btn) setBtnLoading(btn, false);
  }
};

window.openTgManualUnmuteModal = () => {
  const tgState = (state.overview && state.overview.tg_state) || {};
  const chats = tgState.chats || [];
  const quickBtns = chats
    .slice(0, 12)
    .map((k) => `<button type="button" class="pill-toggle" data-mu-chat="${esc(k.chat_id)}" data-mu-acc="${esc(k.account_id || "")}">${esc(k.title || k.chat_id)}</button>`)
    .join("");
  let pickedAcc = "";

  openModal(
    "手动解除 Telegram 成员禁言",
    `<div class="form-grid">
      <div class="field full">
        <label for="muChatId">Telegram 群 ID *</label>
        <input type="text" id="muChatId" placeholder="-1001234567890" />
      </div>
      ${quickBtns ? `<div class="field full"><label>快速选择群聊</label><div class="pill-group" id="muQuickChats">${quickBtns}</div></div>` : ""}
      <div class="field full">
        <label for="muUserId">目标成员的 Telegram 数字 User ID *</label>
        <input type="number" id="muUserId" placeholder="例如 123456789" />
        <span class="field-hint">若不知 User ID，也可以直接在 TG 群内<b>回复该成员发过的消息</b>并发送 <code>/unmute</code>，或点击禁言提示消息下方的「管理员一键解禁」按钮。</span>
      </div>
    </div>`,
    async () => {
      const chatId = $("muChatId").value.trim();
      const userId = Number($("muUserId").value.trim());
      if (!chatId || !/^-\d+$/.test(chatId)) {
        throw new Error("请填写有效的 Telegram 群 ID（以 - 开头的数字，如 -1001234567890）");
      }
      if (!userId || userId <= 0) {
        throw new Error("请填写有效的 Telegram 数字 User ID");
      }
      await api("/api/telegram/action", {
        method: "POST",
        body: JSON.stringify({ account_id: pickedAcc, chat_id: chatId, user_id: userId, action: "unmute" }),
      });
      toast(`已解除用户 ${userId} 在群 ${chatId} 的禁言`);
      await loadAll(false);
    },
    "立即解禁"
  );

  const wrap = $("muQuickChats");
  if (wrap) {
    wrap.querySelectorAll("[data-mu-chat]").forEach((btn) => {
      btn.onclick = () => {
        $("muChatId").value = btn.dataset.muChat || "";
        pickedAcc = btn.dataset.muAcc || "";
        wrap.querySelectorAll("[data-mu-chat]").forEach((x) => x.classList.remove("on"));
        btn.classList.add("on");
      };
    });
  }
};

// ── 全平台共用黑名单管理 ──
function platformBadgeCls(p) {
  const key = String(p || "").toLowerCase();
  if (key === "wechat") return "emerald";
  if (key === "telegram") return "sky";
  return "violet";
}

function platformUserIDPlaceholder(p) {
  const key = String(p || "").toLowerCase();
  if (key === "wechat") return "例如 wxid_xxxxxxxx";
  if (key === "telegram") return "例如 123456789（纯数字 User ID）";
  if (key === "qq") return "例如 10001（QQ 号）";
  return "输入平台用户唯一 ID";
}

function renderGlobalBlacklist() {
  const entries = (state.overview && state.overview.blacklist) || [];
  setBadgeCount("badgeBlacklist", entries.length);

  const filterWrap = $("blPlatformFilters");
  if (filterWrap) {
    const plats = getAvailablePlatforms(entries.map((e) => e.platform));
    const options = [{ key: "all", label: "全部平台" }, ...plats.map((p) => ({ key: p, label: formatPlatformName(p) }))];
    if (state.blPlatform !== "all" && !plats.includes(state.blPlatform)) {
      state.blPlatform = "all";
    }
    filterWrap.innerHTML = options
      .map(
        (opt) =>
          `<button type="button" class="seg-btn ${state.blPlatform === opt.key ? "active" : ""}" data-bl-plat="${esc(opt.key)}">${esc(opt.label)}</button>`
      )
      .join("");
    filterWrap.querySelectorAll("[data-bl-plat]").forEach((btn) => {
      btn.onclick = () => {
        state.blPlatform = btn.dataset.blPlat || "all";
        renderGlobalBlacklist();
      };
    });
  }

  const q = (state.blSearch || "").trim().toLowerCase();
  const filtered = entries.filter((e) => {
    if (state.blPlatform !== "all" && String(e.platform || "").toLowerCase() !== state.blPlatform) return false;
    if (q) {
      const hay = [e.user_id, e.user_name, e.reason, e.source_chat_title, e.source_chat_id, formatPlatformName(e.platform)]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      if (!hay.includes(q)) return false;
    }
    return true;
  });

  if ($("globalBlacklistBadge")) {
    $("globalBlacklistBadge").textContent =
      filtered.length === entries.length ? `${entries.length} 人` : `${filtered.length} / ${entries.length} 人`;
  }
  const box = $("globalBlacklistList");
  if (!box) return;

  if (!filtered.length) {
    box.innerHTML = emptyHTML(
      entries.length ? "没有匹配的黑名单记录" : "当前全平台黑名单为空",
      entries.length
        ? "可尝试清空搜索关键词或切换平台筛选"
        : "在群策略中开启「黑名单系统」后，被移出群聊或触发退群拉黑的成员会自动加入此处；也可点击右上角「手动拉黑」直接添加"
    );
    return;
  }

  box.innerHTML = filtered
    .map((e) => {
      const srcLabel = e.source_chat_title || e.source_chat_id || "";
      const banCount = (e.banned_chats || []).length;
      const detailParts = [`原因：${e.reason || "违规拉黑"}`];
      if (srcLabel) detailParts.push(`来源群：${srcLabel}`);
      if (banCount > 1) detailParts.push(`已拦截 ${banCount} 个群`);
      return `
      <div class="log-row">
        <span class="log-time">${fmtTime(e.added_at)}</span>
        <span class="log-cat-cell"><span class="badge ${platformBadgeCls(e.platform)}">${esc(formatPlatformName(e.platform))}</span></span>
        <div class="log-body">
          <div class="log-summary">
            <span>${esc(e.user_name || e.user_id)}</span>
            <span class="badge muted" style="font-family:var(--mono)">${esc(e.user_id)}</span>
            <span class="badge rose">全群通用</span>
          </div>
          <div class="log-detail">${esc(detailParts.join(" · "))}</div>
        </div>
        <button type="button" class="btn btn-outline btn-sm" onclick="removeGlobalBlacklist('${esc(e.platform)}','${esc(e.user_id)}',this)">移出黑名单</button>
      </div>`;
    })
    .join("");
}

window.removeGlobalBlacklist = async (platform, userID, btn) => {
  if (btn) setBtnLoading(btn, true);
  try {
    await api("/api/blacklist/remove", {
      method: "POST",
      body: JSON.stringify({ platform, user_id: userID }),
    });
    toast(
      platform === "telegram"
        ? `已将 ${userID} 移出黑名单并自动解除相关群封禁`
        : `已将 ${userID} 移出全平台黑名单`
    );
    await loadAll(false);
  } catch (e) {
    toast("移出黑名单失败: " + e.message, "error");
  } finally {
    if (btn) setBtnLoading(btn, false);
  }
};

window.openGlobalBlacklistModal = () => {
  const availPlatforms = getAvailablePlatforms();
  let pickedPlatform = state.blPlatform !== "all" && availPlatforms.includes(state.blPlatform) ? state.blPlatform : availPlatforms[0] || "wechat";
  let kickExisting = true;

  openModal(
    "手动添加全平台黑名单",
    `<div class="form-grid">
      <div class="field full">
        <label>目标平台 *</label>
        <div class="segmented" id="blPlatSeg">
          ${availPlatforms
            .map(
              (p) =>
                `<button type="button" class="seg-btn ${p === pickedPlatform ? "active" : ""}" data-val="${esc(p)}">${esc(formatPlatformName(p))}</button>`
            )
            .join("")}
        </div>
      </div>
      <div class="field">
        <label for="blUserId">用户 ID *</label>
        <input type="text" id="blUserId" placeholder="${esc(platformUserIDPlaceholder(pickedPlatform))}" />
      </div>
      <div class="field">
        <label for="blUserName">备注昵称（可选）</label>
        <input type="text" id="blUserName" placeholder="方便日后识别，留空显示 ID" />
      </div>
      <div class="field full">
        <label for="blReason">拉黑原因（可选）</label>
        <input type="text" id="blReason" placeholder="例如：发广告 / 恶意骚扰" />
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">立即从当前已管理的群中静默移出</div>
            <div class="toggle-row-sub">开启后会自动扫描该平台下所有已管理群聊并静默移出该成员（不在群内发消息，仅记日志）</div>
          </div>
          <button type="button" class="toggle-btn on" id="blKickNowBtn" aria-pressed="true"><span class="toggle-knob"></span></button>
        </div>
      </div>
    </div>`,
    async () => {
      const userId = $("blUserId").value.trim();
      if (!userId) throw new Error("请填写用户 ID");
      if (pickedPlatform === "telegram" && !/^\d+$/.test(userId)) {
        throw new Error("Telegram 用户 ID 必须是纯数字（如 123456789）");
      }
      const res = await api("/api/blacklist/add", {
        method: "POST",
        body: JSON.stringify({
          platform: pickedPlatform,
          user_id: userId,
          user_name: $("blUserName").value.trim(),
          reason: $("blReason").value.trim(),
          kick_existing: kickExisting,
        }),
      });
      const suffix = res.kicked > 0 ? `，并已从 ${res.kicked} 个群静默移出` : "";
      toast(`已加入 ${formatPlatformName(pickedPlatform)} 全局黑名单${suffix}`);
      await loadAll(false);
    },
    "加入黑名单"
  );

  $("blPlatSeg").querySelectorAll(".seg-btn").forEach((b) => {
    b.onclick = () => {
      $("blPlatSeg").querySelectorAll(".seg-btn").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      pickedPlatform = b.dataset.val;
      if ($("blUserId")) $("blUserId").placeholder = platformUserIDPlaceholder(pickedPlatform);
    };
  });
  $("blKickNowBtn").onclick = () => {
    kickExisting = !kickExisting;
    setToggle($("blKickNowBtn"), kickExisting);
  };
};

window.toggleTgChat = async (idx) => {
  state.config.telegram_chats[idx].enabled = !state.config.telegram_chats[idx].enabled;
  await saveConfig("TG 群策略已更新");
};
window.deleteTgChat = async (idx) => {
  if (!(await askConfirm("删除 TG 群策略", "确定删除该群的独立策略吗？", "删除"))) return;
  state.config.telegram_chats.splice(idx, 1);
  await saveConfig("已删除 TG 群策略");
};

window.openTgModal = (idx = -1) => {
  const isNew = idx < 0;
  const c = isNew
    ? {
        chat_id: "",
        title: "",
        enabled: true,
        verify_enabled: true,
        verify_timeout_sec: 120,
        verify_max_attempts: 3,
        verify_kick_on_fail: false,
        welcome_enabled: true,
        welcome_template: "欢迎 {name} 加入 {group}！",
        welcome_delete_sec: 60,
        goodbye_enabled: false,
        goodbye_template: "{name} 离开了群聊。",
        goodbye_delete_sec: 60,
        auto_approve_join: false,
        block_links: false,
        banned_keywords: [],
        delete_banned_msg: true,
        warn_limit: 3,
        warn_action: "mute",
        warn_mute_minutes: 60,
        blacklist_enabled: false,
        blacklist_on_leave: false,
      }
    : JSON.parse(JSON.stringify(state.config.telegram_chats[idx]));

  const existingIDs = new Set((state.config.telegram_chats || []).map((x) => x.chat_id));
  const tgState = (state.overview && state.overview.tg_state) || {};
  const discoveredChats = (tgState.chats || []).length
    ? tgState.chats
    : (state.overview.chats || []).filter((x) => x.platform === "telegram");
  const quickBtns = discoveredChats
    .filter((k) => !isNew || !existingIDs.has(k.chat_id))
    .slice(0, 16)
    .map((k) => {
      const roleSuffix =
        k.bot_status === "administrator" || k.bot_status === "creator"
          ? " · 管理员"
          : k.bot_status
          ? " · 非管理员"
          : "";
      return `<button type="button" class="pill-toggle" data-tg-id="${esc(k.chat_id)}" data-tg-title="${esc(k.title || k.chat_id)}">${esc(k.title || k.chat_id)}${roleSuffix}</button>`;
    })
    .join("");

  let warnAction = c.warn_action === "ban" ? "ban" : "mute";
  openModal(
    isNew ? "添加 Telegram 群管策略" : "配置 Telegram 群管策略",
    `<div class="form-grid">
      <div class="field">
        <label for="tChatId">Telegram 群 ID *</label>
        <input type="text" id="tChatId" value="${esc(c.chat_id)}" placeholder="-1001234567890" ${!isNew ? "disabled" : ""} />
        <span class="field-hint">填写 <code>*</code> 表示全局默认策略；具体群填以 <code>-</code> 开头的数字群 ID。</span>
      </div>
      <div class="field">
        <label for="tTitle">面板显示名称</label>
        <input type="text" id="tTitle" value="${esc(c.title || "")}" placeholder="例如：官方交流群" />
        <span class="field-hint">仅用于本面板识别展示，不会修改 Telegram 群名。</span>
      </div>
      ${
        isNew
          ? `<div class="field full">
              <label>从已发现的 TG 群快速填入</label>
              ${quickBtns ? `<div class="pill-group" id="quickTgChats">${quickBtns}</div>` : `<div class="field-hint">暂未发现新的 TG 群。</div>`}
              <span class="field-hint">Telegram 官方不提供拉取机器人群列表的接口；将机器人拉入群、设为管理员或群内收到任意消息后会自动出现在这里（已持久化缓存）。</span>
            </div>`
          : ""
      }

      <div class="field full form-section-title">1. 进群人机验证（需机器人有「限制成员」权限）</div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">开启进群算术按钮验证</div>
            <div class="toggle-row-sub">新人入群立即禁言并发送算术题，答错会自动刷新新题目，答对立即解禁</div>
          </div>
          <button type="button" class="toggle-btn ${c.verify_enabled ? "on" : ""}" id="tVerifyBtn" aria-pressed="${c.verify_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field" id="tVerifySecField" style="${c.verify_enabled ? "" : "display:none"}">
        <label for="tVerifySec">验证超时（秒）</label>
        <input type="number" id="tVerifySec" value="${c.verify_timeout_sec || 120}" min="15" />
      </div>
      <div class="field" id="tVerifyMaxField" style="${c.verify_enabled ? "" : "display:none"}">
        <label for="tVerifyMax">最大答错次数（答错自动换题）</label>
        <input type="number" id="tVerifyMax" value="${c.verify_max_attempts || 3}" min="1" max="10" />
      </div>
      <div class="field full" id="tVerifyKickField" style="${c.verify_enabled ? "" : "display:none"}">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">验证超时或答错达上限时移出群聊</div>
            <div class="toggle-row-sub">关闭则保持禁言，并在群内及本面板提供「一键解禁」入口供管理员人工放行</div>
          </div>
          <button type="button" class="toggle-btn ${c.verify_kick_on_fail ? "on" : ""}" id="tVerifyKickBtn" aria-pressed="${c.verify_kick_on_fail ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>

      <div class="field full form-section-title">2. 入群欢迎与退群提示（均发在当前 TG 群内）</div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">新人入群发送欢迎消息</div>
            <div class="toggle-row-sub">若开启了进群验证，会在新人通过验证后发送；可用变量：{name} {group}</div>
          </div>
          <button type="button" class="toggle-btn ${c.welcome_enabled ? "on" : ""}" id="tWelcomeBtn" aria-pressed="${c.welcome_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field" id="tWelcomeTplField" style="${c.welcome_enabled ? "" : "display:none"}">
        <label for="tWelcomeTpl">欢迎文案</label>
        <input type="text" id="tWelcomeTpl" value="${esc(c.welcome_template || "")}" />
      </div>
      <div class="field" id="tWelcomeDelField" style="${c.welcome_enabled ? "" : "display:none"}">
        <label for="tWelcomeDel">欢迎消息自动撤回（秒，0=不撤回）</label>
        <input type="number" id="tWelcomeDel" value="${c.welcome_delete_sec ?? 60}" min="0" />
      </div>

      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">成员退群发送群内提示</div>
            <div class="toggle-row-sub">有成员主动退群或被移出时，在<b>当前 TG 群内</b>公开发送提示；可用变量：{name} {group}</div>
          </div>
          <button type="button" class="toggle-btn ${c.goodbye_enabled ? "on" : ""}" id="tGoodbyeBtn" aria-pressed="${c.goodbye_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field" id="tGoodbyeTplField" style="${c.goodbye_enabled ? "" : "display:none"}">
        <label for="tGoodbyeTpl">退群提示文案</label>
        <input type="text" id="tGoodbyeTpl" value="${esc(c.goodbye_template || "{name} 离开了群聊。")}" />
      </div>
      <div class="field" id="tGoodbyeDelField" style="${c.goodbye_enabled ? "" : "display:none"}">
        <label for="tGoodbyeDel">退群提示自动撤回（秒，0=不撤回）</label>
        <input type="number" id="tGoodbyeDel" value="${c.goodbye_delete_sec ?? 60}" min="0" />
      </div>

      <div class="field full form-section-title">3. 内容风控与自动审群（仅针对普通成员，群管理员自动豁免）</div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">拦截外部链接</div>
            <div class="toggle-row-sub">自动拦截普通成员发送的 <code>http(s)://</code>、<code>www.</code>、<code>t.me/</code> 及隐藏超链接；命中后记 1 次警告</div>
          </div>
          <button type="button" class="toggle-btn ${c.block_links ? "on" : ""}" id="tLinksBtn" aria-pressed="${c.block_links ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">自动删除违规消息</div>
            <div class="toggle-row-sub">命中外链或违禁词时自动删除原消息（需机器人有「删除消息」权限）</div>
          </div>
          <button type="button" class="toggle-btn ${c.delete_banned_msg ? "on" : ""}" id="tDelBannedBtn" aria-pressed="${c.delete_banned_msg ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full">
        <label for="tBanned">违禁词列表（逗号或换行分隔，不区分大小写）</label>
        <textarea id="tBanned" rows="2" placeholder="例如：兼职刷单, 代充, 广告关键词">${esc((c.banned_keywords || []).join(", "))}</textarea>
      </div>
      <div class="field">
        <label for="tWarnLimit">警告上限次数</label>
        <input type="number" id="tWarnLimit" value="${c.warn_limit || 3}" min="1" />
      </div>
      <div class="field">
        <label>达上限处罚方式</label>
        <div class="segmented" id="tWarnSeg">
          <button type="button" class="seg-btn ${warnAction === "mute" ? "active" : ""}" data-val="mute">禁言</button>
          <button type="button" class="seg-btn ${warnAction === "ban" ? "active" : ""}" data-val="ban">移出群聊</button>
        </div>
      </div>
      <div class="field full" id="tWarnMuteField" style="${warnAction === "mute" ? "" : "display:none"}">
        <label for="tWarnMute">警告满后禁言时长（分钟）</label>
        <input type="number" id="tWarnMute" value="${c.warn_mute_minutes || 60}" min="1" />
        <span class="field-hint">警告次数在达上限、解禁或重新入群后均不会自动清零，仅支持管理员在面板点击「清零警告」。</span>
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">开启黑名单系统（全群通用、静默拦截）</div>
            <div class="toggle-row-sub">开启后，被移出群聊（含管理员踢人、警告达上限被踢出）的成员自动加入全局黑名单；单纯警告或禁言不拉黑。黑名单成员申请加群会被静默拒绝，再次入群会自动静默踢出（不发群消息，仅记日志），统一在左侧「黑名单管理」中管理</div>
          </div>
          <button type="button" class="toggle-btn ${c.blacklist_enabled ? "on" : ""}" id="tBlacklistBtn" aria-pressed="${c.blacklist_enabled ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full" id="tBlacklistOnLeaveField" style="${c.blacklist_enabled ? "" : "display:none"}">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">成员主动退出群聊也自动加入黑名单（可选）</div>
            <div class="toggle-row-sub">开启后，普通成员自行退群也会自动拉黑并禁止再次加群；关闭则仅在被移出群聊时拉黑</div>
          </div>
          <button type="button" class="toggle-btn ${c.blacklist_on_leave ? "on" : ""}" id="tBlacklistOnLeaveBtn" aria-pressed="${c.blacklist_on_leave ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
      <div class="field full">
        <div class="toggle-row">
          <div>
            <div class="toggle-row-title">自动通过入群申请</div>
            <div class="toggle-row-sub">当群开启了「需要管理员审批才能加入」时，自动同意新人的入群申请（黑名单成员仍会被自动拒绝）</div>
          </div>
          <button type="button" class="toggle-btn ${c.auto_approve_join ? "on" : ""}" id="tApproveBtn" aria-pressed="${c.auto_approve_join ? "true" : "false"}"><span class="toggle-knob"></span></button>
        </div>
      </div>
    </div>`,
    async () => {
      const chatId = $("tChatId").value.trim();
      if (!chatId) throw new Error("请填写 Telegram 群 ID");
      if (chatId !== "*" && !/^-\d+$/.test(chatId)) {
        throw new Error("Telegram 群 ID 格式无效，请填写 * 或以 - 开头的数字群 ID（如 -1001234567890）");
      }
      if (isNew && existingIDs.has(chatId)) {
        throw new Error("该 Telegram 群已存在策略配置，请勿重复添加");
      }
      const next = {
        ...c,
        chat_id: chatId,
        title: $("tTitle").value.trim(),
        verify_enabled: isToggleOn($("tVerifyBtn")),
        verify_timeout_sec: Math.max(15, Number($("tVerifySec").value) || 120),
        verify_max_attempts: Math.max(1, Math.min(10, Number($("tVerifyMax").value) || 3)),
        verify_kick_on_fail: isToggleOn($("tVerifyKickBtn")),
        welcome_enabled: isToggleOn($("tWelcomeBtn")),
        welcome_template: $("tWelcomeTpl").value,
        welcome_delete_sec: Math.max(0, Number($("tWelcomeDel").value) || 0),
        goodbye_enabled: isToggleOn($("tGoodbyeBtn")),
        goodbye_template: $("tGoodbyeTpl").value,
        goodbye_delete_sec: Math.max(0, Number($("tGoodbyeDel").value) || 0),
        block_links: isToggleOn($("tLinksBtn")),
        delete_banned_msg: isToggleOn($("tDelBannedBtn")),
        auto_approve_join: isToggleOn($("tApproveBtn")),
        banned_keywords: splitCSV($("tBanned").value),
        warn_limit: Math.max(1, Number($("tWarnLimit").value) || 3),
        warn_action: warnAction,
        warn_mute_minutes: Math.max(1, Number($("tWarnMute").value) || 60),
        blacklist_enabled: isToggleOn($("tBlacklistBtn")),
        blacklist_on_leave: isToggleOn($("tBlacklistOnLeaveBtn")),
      };
      if (isNew) state.config.telegram_chats.push(next);
      else state.config.telegram_chats[idx] = next;
      await saveConfig("Telegram 群策略已保存");
    }
  );

  // 开关折叠联动
  $("tVerifyBtn").onclick = () => {
    const on = !isToggleOn($("tVerifyBtn"));
    setToggle($("tVerifyBtn"), on);
    $("tVerifySecField").style.display = on ? "" : "none";
    $("tVerifyMaxField").style.display = on ? "" : "none";
    $("tVerifyKickField").style.display = on ? "" : "none";
  };
  $("tVerifyKickBtn").onclick = () => setToggle($("tVerifyKickBtn"), !isToggleOn($("tVerifyKickBtn")));
  $("tWelcomeBtn").onclick = () => {
    const on = !isToggleOn($("tWelcomeBtn"));
    setToggle($("tWelcomeBtn"), on);
    $("tWelcomeTplField").style.display = on ? "" : "none";
    $("tWelcomeDelField").style.display = on ? "" : "none";
  };
  $("tGoodbyeBtn").onclick = () => {
    const on = !isToggleOn($("tGoodbyeBtn"));
    setToggle($("tGoodbyeBtn"), on);
    $("tGoodbyeTplField").style.display = on ? "" : "none";
    $("tGoodbyeDelField").style.display = on ? "" : "none";
  };
  $("tLinksBtn").onclick = () => setToggle($("tLinksBtn"), !isToggleOn($("tLinksBtn")));
  $("tDelBannedBtn").onclick = () => setToggle($("tDelBannedBtn"), !isToggleOn($("tDelBannedBtn")));
  $("tBlacklistBtn").onclick = () => {
    const on = !isToggleOn($("tBlacklistBtn"));
    setToggle($("tBlacklistBtn"), on);
    $("tBlacklistOnLeaveField").style.display = on ? "" : "none";
  };
  $("tBlacklistOnLeaveBtn").onclick = () => setToggle($("tBlacklistOnLeaveBtn"), !isToggleOn($("tBlacklistOnLeaveBtn")));
  $("tApproveBtn").onclick = () => setToggle($("tApproveBtn"), !isToggleOn($("tApproveBtn")));

  const quickWrap = $("quickTgChats");
  if (quickWrap) {
    quickWrap.querySelectorAll("[data-tg-id]").forEach((btn) => {
      btn.onclick = () => {
        $("tChatId").value = btn.dataset.tgId || "";
        $("tTitle").value = btn.dataset.tgTitle || "";
        quickWrap.querySelectorAll("[data-tg-id]").forEach((x) => x.classList.remove("on"));
        btn.classList.add("on");
      };
    });
  }
  $("tWarnSeg").querySelectorAll(".seg-btn").forEach((b) => {
    b.onclick = () => {
      $("tWarnSeg").querySelectorAll(".seg-btn").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      warnAction = b.dataset.val;
      $("tWarnMuteField").style.display = warnAction === "mute" ? "" : "none";
    };
  });
};

// ── 监控与转发规则 ──
const RULE_DEFAULT_TEMPLATE = [
  "🔔 群消息转发通知",
  "规则：{rule}",
  "来源：[{platform}] {group}",
  "触发：{target} · 类型：{msg_type}",
  "发送者：{sender}",
  "内容：{content}",
  "时间：{time}",
].join("\n");

const RULE_MINIMAL_TEMPLATE = "[{group}] {sender}: {content}";

const RULE_TEMPLATE_VARS = [
  { token: "{rule}", label: "规则名" },
  { token: "{platform}", label: "来源平台" },
  { token: "{group}", label: "来源群" },
  { token: "{chat_id}", label: "群 ID" },
  { token: "{sender}", label: "发送者" },
  { token: "{target}", label: "触发原因" },
  { token: "{msg_type}", label: "消息类型" },
  { token: "{content}", label: "消息内容" },
  { token: "{time}", label: "时间" },
];

const COOLDOWN_SCOPE_OPTIONS = [
  {
    value: "target",
    title: "群 + 触发项",
    desc: "同一群里 @张三 与 @李四 各自独立计时，互不干扰（推荐）",
  },
  {
    value: "chat",
    title: "按整群",
    desc: "同一个群冷却期内无论触发谁或什么词，总共只转发 1 次",
  },
  {
    value: "rule",
    title: "规则全局",
    desc: "所有监听群共用 1 个计时器，冷却期内整条规则只转发 1 次",
  },
];

function formatCooldownScopeLabel(scope) {
  const hit = COOLDOWN_SCOPE_OPTIONS.find((x) => x.value === scope);
  return hit ? hit.title : "群 + 触发项";
}

function insertAtCursor(el, text) {
  if (!el) return;
  const start = typeof el.selectionStart === "number" ? el.selectionStart : el.value.length;
  const end = typeof el.selectionEnd === "number" ? el.selectionEnd : el.value.length;
  const before = el.value.slice(0, start);
  const after = el.value.slice(end);
  el.value = before + text + after;
  const nextPos = start + text.length;
  el.focus();
  if (typeof el.setSelectionRange === "function") {
    el.setSelectionRange(nextPos, nextPos);
  }
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

function renderMonitorRules() {
  const rules = state.config.monitor_rules || [];
  if (!rules.length) {
    $("rulesGrid").innerHTML = emptyHTML(
      "还没有转发规则",
      "支持任意已接入平台间的同平台直转与跨平台转发，点击右上角「新建转发规则」开始配置",
      true
    );
    return;
  }
  const groupNameMap = Object.fromEntries((state.config.mention_groups || []).map((g) => [g.id, g.name]));
  $("rulesGrid").innerHTML = rules
    .map((r, idx) => {
      const platText = (r.platforms || []).length
        ? r.platforms.map(formatPlatformName).join(" / ")
        : "全部平台";
      const typeTags = (r.message_types || []).length
        ? r.message_types.map((t) => `<span class="badge muted">${esc(MSG_TYPE_LABEL[t] || t)}</span>`).join("")
        : `<span class="badge muted">全部消息类型</span>`;
      const forwardMedia = r.forward_media !== false;
      const maxMB = r.max_media_mb > 0 ? r.max_media_mb : 10;
      const mediaBadge = forwardMedia
        ? `<span class="badge sky">原媒体转发（跨平台 ≤${maxMB}MB）</span>`
        : `<span class="badge muted">仅文字摘要</span>`;
      const formatBadge = r.raw_forward
        ? `<span class="badge primary">纯原样转发</span>`
        : "";
      const filterBadges = [
        r.ignore_commands ? `<span class="badge muted">过滤 /命令</span>` : "",
        r.ignore_bots ? `<span class="badge muted">忽略 Bot</span>` : "",
        (r.exclude_keywords || []).length ? `<span class="badge muted">排除 ${r.exclude_keywords.length} 个词</span>` : "",
      ].join("");
      return `
    <div class="rule-card">
      <div class="rule-top">
        <div class="rule-head-copy">
          <div class="rule-title" title="${esc(r.name)}">${esc(r.name)}</div>
          <div class="rule-sub">监听: ${esc(platText)}${(r.chat_ids || []).length ? `（${r.chat_ids.length} 个群）` : "（全部群）"} · 冷却: ${r.cooldown_seconds > 0 ? `${fmtDuration(r.cooldown_seconds)}（${formatCooldownScopeLabel(r.cooldown_scope)}）` : "不冷却"}</div>
        </div>
        <button type="button" class="toggle-btn ${r.enabled ? "on" : ""}" aria-pressed="${r.enabled ? "true" : "false"}" onclick="toggleRule(${idx})"><span class="toggle-knob"></span></button>
      </div>
      <div class="tags">
        ${
          r.match_all_messages
            ? `<span class="badge sky">全部消息同步</span>`
            : `
          ${(r.watch_group_ids || []).map((g) => `<span class="badge violet">分组: ${esc(groupNameMap[g] || g)}</span>`).join("")}
          ${(r.watch_mentions || []).map((m) => `<span class="badge primary">@${esc(m.replace(/^@/, ""))}</span>`).join("")}
          ${r.watch_all ? `<span class="badge rose">@全体成员</span>` : ""}
          ${(r.keywords || []).map((k) => `<span class="badge amber">关键词: ${esc(k)}</span>`).join("")}`
        }
        ${typeTags}
        ${formatBadge}
        ${mediaBadge}
        ${filterBadges}
      </div>
      <div class="tags">
        ${(r.targets || [])
          .map((t) => `<span class="badge emerald">→ ${esc(formatPlatformName(t.type))}: ${esc(t.to)}</span>`)
          .join("")}
      </div>
      <div class="rule-actions">
        <button type="button" class="btn btn-outline btn-sm" onclick="openRuleEditor(${idx})">编辑配置</button>
        <button type="button" class="btn btn-danger btn-sm" onclick="deleteRule(${idx})">删除</button>
      </div>
    </div>`;
    })
    .join("");
}

window.toggleRule = async (idx) => {
  state.config.monitor_rules[idx].enabled = !state.config.monitor_rules[idx].enabled;
  await saveConfig("转发规则状态已更新");
};
window.deleteRule = async (idx) => {
  if (!(await askConfirm("删除转发规则", "确定删除这条监控转发规则吗？", "删除"))) return;
  state.config.monitor_rules.splice(idx, 1);
  await saveConfig("转发规则已删除");
};

window.closeRuleEditor = () => {
  state.ruleEditing = false;
  closeAllSelects();
  if ($("ruleEditorView")) {
    $("ruleEditorView").style.display = "none";
    $("ruleEditorView").innerHTML = "";
  }
  if ($("ruleListView")) {
    $("ruleListView").style.display = "";
  }
};

window.openRuleEditor = (idx = -1, preselectGroupId = "") => {
  if (state.activeTab !== "monitor") {
    switchTab("monitor");
  }
  state.ruleEditing = true;
  const isNew = idx < 0;
  const accounts = state.overview.accounts || [];
  const availPlatforms = getAvailablePlatforms([
    ...(isNew ? [] : state.config.monitor_rules[idx].platforms || []),
    ...(isNew ? [] : (state.config.monitor_rules[idx].targets || []).map((t) => t.type)),
  ]);
  const preferredAccount = accounts.find((a) => a.online) || accounts[0];
  const defaultPlat = (preferredAccount && preferredAccount.platform) || availPlatforms[0] || "wechat";
  const defaultAccId = (preferredAccount && preferredAccount.id) || "";

  const r = isNew
    ? {
        id: "rule_" + Date.now().toString(36),
        name: "",
        enabled: true,
        platforms: [],
        chat_ids: [],
        ignore_senders: [],
        ignore_commands: true,
        ignore_bots: false,
        exclude_keywords: [],
        match_all_messages: false,
        message_types: [],
        watch_mentions: [],
        watch_group_ids: preselectGroupId ? [preselectGroupId] : [],
        watch_all: false,
        keywords: [],
        cooldown_seconds: 300,
        cooldown_scope: "target",
        forward_media: true,
        max_media_mb: 10,
        raw_forward: false,
        template: "",
        targets: [{ type: defaultPlat, account_id: defaultAccId, to: "" }],
      }
    : JSON.parse(JSON.stringify(state.config.monitor_rules[idx]));

  if (typeof r.forward_media !== "boolean") r.forward_media = true;
  if (!r.max_media_mb || r.max_media_mb <= 0) r.max_media_mb = 10;
  r.targets = (r.targets || []).filter((t) => t && t.type && t.type !== "email");
  if (!r.targets.length) {
    r.targets = [{ type: defaultPlat, account_id: defaultAccId, to: "" }];
  }

  let matchMode = r.match_all_messages ? "all" : "cond";
  let scopeVal = r.cooldown_scope || "target";
  let enabledVal = r.enabled !== false;
  let forwardMediaVal = r.forward_media !== false;
  let rawForwardVal = !!r.raw_forward;

  const selectedPlatforms = new Set((r.platforms || []).map((x) => x.toLowerCase()));
  const selectedMsgTypes = new Set((r.message_types || []).map((x) => x.toLowerCase()));

  const groupPills = (state.config.mention_groups || [])
    .map(
      (g) =>
        `<button type="button" class="pill-toggle ${(r.watch_group_ids || []).includes(g.id) ? "on" : ""}" data-gid="${esc(g.id)}" onclick="this.classList.toggle('on');window._refreshRulePreview()">${esc(g.name)} (${(g.members || []).length}人)</button>`
    )
    .join("");

  const knownChats = (state.overview.chats || []).slice(0, 12);
  const quickChats = knownChats
    .map(
      (ch) =>
        `<button type="button" class="pill-toggle" data-append-chat="${esc(ch.chat_id)}">${esc(formatPlatformName(ch.platform))}: ${esc(ch.title || ch.chat_id)}</button>`
    )
    .join("");

  const varButtons = RULE_TEMPLATE_VARS.map(
    (v) =>
      `<button type="button" class="tpl-var-chip" data-insert-var="${v.token}" title="点击在光标位置插入 ${v.token}"><span>${v.label}</span><code>${v.token}</code></button>`
  ).join("");

  const scopeCardsHTML = COOLDOWN_SCOPE_OPTIONS.map(
    (opt) => `
      <button type="button" class="scope-card ${scopeVal === opt.value ? "active" : ""}" data-scope="${opt.value}">
        <div class="scope-card-title">${opt.title}</div>
        <div class="scope-card-desc">${opt.desc}</div>
      </button>`
  ).join("");

  $("ruleListView").style.display = "none";
  $("ruleEditorView").style.display = "";
  $("ruleEditorView").innerHTML = `
    <div class="rule-editor-shell">
      <div class="rule-editor-topbar">
        <div class="rule-editor-topbar-left">
          <button type="button" class="btn btn-outline btn-sm" onclick="closeRuleEditor()">← 返回列表</button>
          <div>
            <div class="rule-editor-heading">${isNew ? "新建消息转发规则" : `编辑规则：${esc(r.name)}`}</div>
            <div class="rule-editor-sub">配置监听来源、触发条件、原文件/媒体策略与转发文案模板</div>
          </div>
        </div>
        <div class="rule-editor-topbar-right">
          <div class="rule-enabled-inline">
            <span>规则启用</span>
            <button type="button" class="toggle-btn ${enabledVal ? "on" : ""}" id="fRuleEnabled" aria-pressed="${enabledVal ? "true" : "false"}"><span class="toggle-knob"></span></button>
          </div>
          <button type="button" class="btn btn-outline" onclick="closeRuleEditor()">取消</button>
          <button type="button" class="btn btn-primary" id="btnSaveRuleEditor">保存规则</button>
        </div>
      </div>

      <div class="rule-editor-grid">
        <div class="rule-editor-main">
          <!-- 1. 基础信息与监听来源 -->
          <div class="rule-section-card">
            <div class="rule-section-head">
              <div>
                <div class="rule-section-title"><span class="rule-step-num">1</span>基础信息与监听来源</div>
                <div class="rule-section-desc">指定要监听哪些平台和群聊；机器人自己发出的消息会自动跳过，不会死循环</div>
              </div>
            </div>
            <div class="form-grid">
              <div class="field">
                <label>规则名称 *</label>
                <input type="text" id="fName" value="${esc(r.name)}" placeholder="例如：A群同步到B群 / 运维 @ 告警" />
              </div>
              <div class="field">
                <label>监听来源平台（不选 = 监听全部已接入平台）</label>
                <div class="pill-group" id="fPlatPills">
                  ${availPlatforms
                    .map(
                      (p) =>
                        `<button type="button" class="pill-toggle ${selectedPlatforms.has(p) ? "on" : ""}" data-plat="${esc(p)}" onclick="this.classList.toggle('on');window._refreshRulePreview()">${esc(formatPlatformName(p))}</button>`
                    )
                    .join("")}
                </div>
              </div>
              <div class="field full">
                <label>指定来源群 ID（逗号分隔，留空监听所选平台的全部群聊）</label>
                <input type="text" id="fChats" value="${esc((r.chat_ids || []).join(", "))}" placeholder="输入来源群 ID，多个用逗号分隔（也可直接点击下方已发现群聊填入）" />
                ${quickChats ? `<div class="pill-group" id="quickRuleChats" style="margin-top:6px">${quickChats}</div>` : ""}
              </div>
              <div class="field full">
                <label>忽略发送者（可选，逗号分隔）</label>
                <input type="text" id="fIgnoreSenders" value="${esc((r.ignore_senders || []).join(", "))}" placeholder="填写要忽略的账号 ID 或用户名，多个用逗号分隔" />
              </div>
            </div>
          </div>

          <!-- 2. 触发方式 -->
          <div class="rule-section-card">
            <div class="rule-section-head">
              <div>
                <div class="rule-section-title"><span class="rule-step-num">2</span>触发方式（什么时候转发）</div>
                <div class="rule-section-desc">可按 @人 / 人员分组 / 关键词精准触发，或开启全量同步搬运整群消息</div>
              </div>
            </div>
            <div class="form-grid">
              <div class="field full">
                <div class="segmented" id="fModeSeg">
                  <button type="button" class="seg-btn ${matchMode === "cond" ? "active" : ""}" data-val="cond">按条件触发（@ / 人员分组 / 关键词）</button>
                  <button type="button" class="seg-btn ${matchMode === "all" ? "active" : ""}" data-val="all">转发全部消息（同群/跨群全量同步）</button>
                </div>
              </div>
              <div class="field full" id="fCondSection">
                <div class="form-grid">
                  <div class="field full">
                    <label>引用人员分组（群里 @ 组内任意成员即触发）</label>
                    <div class="pill-group">${groupPills || '<span class="field-hint">暂无分组，可先去左侧「人员分组 (@)」创建，或直接在下方填写个人</span>'}</div>
                  </div>
                  <div class="field">
                    <label>额外监控被 @ 的个人（逗号分隔，已选分组的成员无需重复填）</label>
                    <input type="text" id="fMentions" value="${esc((r.watch_mentions || []).join(", "))}" placeholder="填写账号 ID、用户名或昵称，多个用逗号分隔" />
                  </div>
                  <div class="field">
                    <label>关键词监控（逗号分隔，不区分大小写）</label>
                    <input type="text" id="fKeywords" value="${esc((r.keywords || []).join(", "))}" placeholder="例如 紧急, 报错, 宕机, #ops" />
                  </div>
                  <div class="field full">
                    <div class="toggle-row">
                      <div>
                        <div class="toggle-row-title">同时监控 @所有人 / @all</div>
                        <div class="toggle-row-sub">群里出现 @所有人、@全体成员 或 @all 时也触发转发</div>
                      </div>
                      <button type="button" class="toggle-btn ${r.watch_all ? "on" : ""}" id="fWatchAll" aria-pressed="${r.watch_all ? "true" : "false"}" onclick="setToggle(this, !isToggleOn(this));window._refreshRulePreview()"><span class="toggle-knob"></span></button>
                    </div>
                  </div>
                </div>
              </div>

              <!-- 排除过滤（对条件触发与全量同步均生效） -->
              <div class="field full" style="margin-top:4px;padding-top:12px;border-top:1px dashed var(--border)">
                <label>排除过滤（命中以下条件的消息将被跳过，不转发）</label>
                <div class="pill-group" style="margin-bottom:8px">
                  <button type="button" class="pill-toggle ${r.ignore_commands ? "on" : ""}" id="fIgnoreCommands" onclick="this.classList.toggle('on');window._refreshRulePreview()">忽略 / 机器人命令（如 /warn、/panel）</button>
                  <button type="button" class="pill-toggle ${r.ignore_bots ? "on" : ""}" id="fIgnoreBots" onclick="this.classList.toggle('on');window._refreshRulePreview()">忽略其他 Bot 机器人消息</button>
                </div>
                <input type="text" id="fExcludeKeywords" value="${esc((r.exclude_keywords || []).join(", "))}" placeholder="过滤包含以下文本的消息（逗号分隔），例如：@LemonQuantBot, Bot Hub, ^!, 广告" />
                <span class="field-hint">不区分大小写；包含任意词即跳过转发。若以 <code>^</code> 开头（如 <code>^#</code>）表示仅过滤以该前缀开头的消息。</span>
              </div>
            </div>
          </div>

          <!-- 3. 消息类型与原文件/媒体转发策略 -->
          <div class="rule-section-card">
            <div class="rule-section-head">
              <div>
                <div class="rule-section-title"><span class="rule-step-num">3</span>消息类型与原文件转发策略</div>
                <div class="rule-section-desc">筛选允许转发的消息类型，并控制原图、原视频、原文件、语音的转发方式与大小上限</div>
              </div>
            </div>
            <div class="field full">
              <label>消息类型筛选（都不选 = 全部类型均转发）</label>
              <div class="pill-group" id="fMsgTypePills">
                ${MSG_TYPE_OPTIONS.map(
                  (opt) =>
                    `<button type="button" class="pill-toggle ${selectedMsgTypes.has(opt.key) ? "on" : ""}" data-mtype="${opt.key}" onclick="this.classList.toggle('on');window._refreshRulePreview()">${opt.label}</button>`
                ).join("")}
              </div>
            </div>

            <div class="media-policy-box">
              <div class="media-policy-row">
                <div>
                  <div style="font-size:13.5px;font-weight:600;color:var(--text)">转发原图片 / 原视频 / 原文件 / 语音</div>
                  <div style="font-size:12px;color:var(--text-3);margin-top:2px">开启后将尝试原样投递媒体文件；关闭后仅转发文字说明与文件名摘要</div>
                </div>
                <button type="button" class="toggle-btn ${forwardMediaVal ? "on" : ""}" id="fForwardMedia" aria-pressed="${forwardMediaVal ? "true" : "false"}"><span class="toggle-knob"></span></button>
              </div>

              <div id="fMediaLimitRow" style="${forwardMediaVal ? "" : "display:none"}">
                <div class="media-policy-row" style="margin-bottom:10px">
                  <div style="font-size:12.5px;font-weight:600;color:var(--text-2)">跨平台文件大小上限（MB，最大 32 MB）</div>
                  <div class="media-size-bar">
                    <input type="number" id="fMaxMediaMB" class="media-size-input" min="1" max="32" value="${r.max_media_mb}" />
                    <span style="font-size:12px;color:var(--text-3)">MB</span>
                    <button type="button" class="pill-toggle" onclick="$('fMaxMediaMB').value=5;window._refreshRulePreview()">5 MB</button>
                    <button type="button" class="pill-toggle" onclick="$('fMaxMediaMB').value=10;window._refreshRulePreview()">10 MB</button>
                    <button type="button" class="pill-toggle" onclick="$('fMaxMediaMB').value=20;window._refreshRulePreview()">20 MB</button>
                    <button type="button" class="pill-toggle" onclick="$('fMaxMediaMB').value=32;window._refreshRulePreview()">32 MB</button>
                  </div>
                </div>
                <div class="media-mode-list">
                  <div class="media-mode-item">
                    <strong>同平台直转（免重复上传）</strong>
                    源与目标为同一平台时，优先复用平台原生资源标识秒级直转，<b>不受下方大小上限限制</b>。
                  </div>
                  <div class="media-mode-item">
                    <strong>跨平台 / 跨账号中转</strong>
                    自动下载并重新上传原图、视频、文件与语音；若文件超过设定上限，自动跳过下载并附带超限提示。
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 4. 转发目标与防刷屏冷却 -->
          <div class="rule-section-card">
            <div class="rule-section-head">
              <div>
                <div class="rule-section-title"><span class="rule-step-num">4</span>转发目标与防刷屏冷却</div>
                <div class="rule-section-desc">支持同时转发到多个群聊或个人会话，并按冷却窗口防止短时间重复刷屏</div>
              </div>
              <button type="button" class="btn btn-outline btn-sm" id="btnAddTarget">+ 添加目标</button>
            </div>
            <div class="form-grid">
              <div class="field full">
                <div id="fTargetsBox"></div>
              </div>
              <div class="field full">
                <label>防刷屏冷却时长（秒，0 = 不冷却）</label>
                <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
                  <input type="number" id="fCooldown" style="width:120px" min="0" value="${r.cooldown_seconds ?? 300}" />
                  <div class="pill-group">
                    <button type="button" class="pill-toggle" onclick="$('fCooldown').value=0;window._refreshRulePreview()">不冷却</button>
                    <button type="button" class="pill-toggle" onclick="$('fCooldown').value=60;window._refreshRulePreview()">1 分钟</button>
                    <button type="button" class="pill-toggle" onclick="$('fCooldown').value=300;window._refreshRulePreview()">5 分钟</button>
                    <button type="button" class="pill-toggle" onclick="$('fCooldown').value=600;window._refreshRulePreview()">10 分钟</button>
                    <button type="button" class="pill-toggle" onclick="$('fCooldown').value=1800;window._refreshRulePreview()">30 分钟</button>
                  </div>
                </div>
              </div>
              <div class="field full">
                <label>冷却粒度（冷却计时器按什么维度分开计算）</label>
                <div class="scope-cards" id="fScopeCards">${scopeCardsHTML}</div>
              </div>
            </div>
          </div>

          <!-- 5. 转发内容格式（纯原样转发 / 自定义模板） -->
          <div class="rule-section-card">
            <div class="rule-section-head">
              <div>
                <div class="rule-section-title"><span class="rule-step-num">5</span>转发内容格式</div>
                <div class="rule-section-desc">决定转发出去时是“只发原始内容/原图”，还是外层套用通知模板</div>
              </div>
            </div>
            <div class="format-mode-cards" id="fFormatModeCards">
              <button type="button" class="scope-card ${rawForwardVal ? "active" : ""}" data-raw="1">
                <div class="scope-card-title">纯原样转发（推荐同步/搬运）</div>
                <div class="scope-card-desc">只发原始文字或原图/原视频/原文件/贴纸，<b>不额外发送通知模板文本</b>；有配文时仅附原配文</div>
              </button>
              <button type="button" class="scope-card ${!rawForwardVal ? "active" : ""}" data-raw="0">
                <div class="scope-card-title">带模板通知（适合 @ 告警）</div>
                <div class="scope-card-desc">附带来源群、发送者、触发原因、时间等信息；下方可点击变量自由组合排版</div>
              </button>
            </div>

            <div id="fRawModeNote" class="raw-mode-note" style="${rawForwardVal ? "" : "display:none"}">
              <div><strong>纯原样转发说明：</strong></div>
              <div>• <strong>文字消息：</strong>直接转发原文（如 <code>😜</code>），不加任何前缀与时间。</div>
              <div>• <strong>图片 / 视频 / 文件 / 语音 / 贴纸：</strong>直接发出原媒体，不再先发一条 <code>🔔 群消息转发通知</code>；仅当文件超限或目标平台不支持该媒体格式时，自动降级为对应文字或 emoji 兜底。</div>
            </div>

            <div id="fTplModeBox" style="${rawForwardVal ? "display:none" : ""}">
              <div class="tpl-toolbar">
                <span style="font-size:12px;color:var(--text-3)">点击下方变量，直接插入到文本框光标处：</span>
                <div class="pill-group">
                  <button type="button" class="btn btn-ghost btn-sm" id="btnTplDefault">填入默认模板</button>
                  <button type="button" class="btn btn-ghost btn-sm" id="btnTplMinimal">极简格式</button>
                  <button type="button" class="btn btn-ghost btn-sm" id="btnTplClear">清空（用默认）</button>
                </div>
              </div>
              <div class="tpl-var-bar" id="fTplVarBar">${varButtons}</div>
              <textarea id="fTemplate" class="tpl-textarea" rows="6" placeholder="留空使用默认模板。点击上方变量按钮可一键插入，例如：[{group}] {sender}: {content}">${esc(r.template || "")}</textarea>
            </div>
          </div>
        </div>

        <!-- 右侧粘性预览与摘要栏 -->
        <aside class="rule-editor-side">
          <div class="preview-card">
            <div class="preview-card-title">转发消息实时预览</div>
            <div class="preview-card-sub">下方为使用示例数据渲染后的实际发出效果</div>
            <div class="tpl-preview-bubble" id="fTplPreview"></div>
          </div>

          <div class="preview-card">
            <div class="preview-card-title">当前规则摘要</div>
            <div class="preview-card-sub">保存前快速核对配置是否符合预期</div>
            <div class="preview-summary-list" id="fRuleSummary"></div>
          </div>
        </aside>
      </div>
    </div>`;

  const renderTargets = () => {
    const box = $("fTargetsBox");
    if (!box) return;
    box.innerHTML = r.targets
      .map(
        (t, i) => `
      <div class="target-row">
        <div class="cselect" id="tSel_${i}"></div>
        <div class="cselect" id="tAcc_${i}" data-placeholder="选择发送账号"></div>
        <input type="text" placeholder="${esc(platformTargetPlaceholder(t.type))}" value="${esc(t.to || "")}" oninput="window._tUpd(${i},'to',this.value)" />
        <button type="button" class="btn btn-danger btn-sm" onclick="window._tDel(${i})">删</button>
      </div>`
      )
      .join("");

    r.targets.forEach((t, i) => {
      const targetType = t.type || defaultPlat;
      const platAccounts = (state.overview.accounts || []).filter((a) => a.platform === targetType);
      if (t.account_id && !platAccounts.some((a) => a.id === t.account_id)) {
        platAccounts.unshift({ id: t.account_id, nickname: t.account_id, online: false, platform: targetType });
      }
      if (!t.account_id && platAccounts.length) {
        const preferred = platAccounts.find((a) => a.online) || platAccounts[0];
        t.account_id = preferred.id;
      }

      mountSelect(
        $(`tSel_${i}`),
        availPlatforms.map((p) => ({ value: p, label: formatPlatformName(p) })),
        targetType,
        (val) => {
          r.targets[i].type = val;
          const nextList = (state.overview.accounts || []).filter((a) => a.platform === val);
          const nextAcc = nextList.find((a) => a.online) || nextList[0];
          r.targets[i].account_id = nextAcc ? nextAcc.id : "";
          renderTargets();
          refreshPreview();
        }
      );

      mountSelect(
        $(`tAcc_${i}`),
        platAccounts.map((a) => ({
          value: a.id,
          label: accountDisplayName(a),
          sub: a.id,
          dot: a.online ? "online" : "offline",
          tag: a.online ? "" : "离线",
        })),
        t.account_id || "",
        (val) => {
          r.targets[i].account_id = val;
          refreshPreview();
        }
      );
    });
  };

  const refreshPreview = () => {
    const nameVal = ($("fName")?.value || "").trim() || "示例转发规则";
    if ($("fTplPreview")) {
      if (rawForwardVal) {
        $("fTplPreview").textContent = [
          "【纯原样转发效果】",
          "• 收到文字「你好 😜」→ 只发出：你好 😜",
          "• 收到纯图片/贴纸 → 只发出原图（不发前置文本）",
          "• 收到图文消息 → 发出原图 + 原配文",
        ].join("\n");
      } else {
        const tplRaw = $("fTemplate")?.value || "";
        const tpl = tplRaw.trim() ? tplRaw : RULE_DEFAULT_TEMPLATE;
        const sampleVars = {
          "{rule}": nameVal,
          "{platform}": "微信",
          "{group}": "产品研发交流群",
          "{chat_id}": "12345678@chatroom",
          "{sender}": "张三",
          "{target}": matchMode === "all" ? "全部消息" : "@运维值班组",
          "{msg_type}": "图片+文字",
          "{content}": "[图片] 线上接口响应变慢，麻烦看一下",
          "{time}": "2026-11-24 14:30:05",
        };
        let rendered = tpl;
        Object.entries(sampleVars).forEach(([k, v]) => {
          rendered = rendered.split(k).join(v);
        });
        $("fTplPreview").textContent = rendered;
      }
    }

    const plats = Array.from(document.querySelectorAll("#fPlatPills [data-plat].on")).map((el) =>
      formatPlatformName(el.dataset.plat)
    );
    const chats = splitCSV($("fChats")?.value || "");
    const mtypes = Array.from(document.querySelectorAll("#fMsgTypePills [data-mtype].on")).map(
      (el) => MSG_TYPE_LABEL[el.dataset.mtype] || el.dataset.mtype
    );
    const cd = Math.max(0, Number($("fCooldown")?.value) || 0);
    const maxMB = Math.min(32, Math.max(1, Number($("fMaxMediaMB")?.value) || 10));
    const validTargetCount = r.targets.filter((t) => t.to && t.to.trim() && t.account_id && t.account_id.trim()).length;
    const excludeParts = [];
    if ($("fIgnoreCommands")?.classList.contains("on")) excludeParts.push("忽略 /命令");
    if ($("fIgnoreBots")?.classList.contains("on")) excludeParts.push("忽略 Bot");
    const exclKws = splitCSV($("fExcludeKeywords")?.value || "");
    if (exclKws.length) excludeParts.push(`排除 ${exclKws.length} 个词`);

    if ($("fRuleSummary")) {
      $("fRuleSummary").innerHTML = `
        <div class="preview-summary-row"><span class="preview-summary-k">监听范围</span><span class="preview-summary-v">${esc(plats.length ? plats.join(" / ") : "全部平台")} · ${chats.length ? `${chats.length} 个指定群` : "全部群聊"}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">触发模式</span><span class="preview-summary-v">${matchMode === "all" ? "转发全部消息" : "按 @ / 分组 / 关键词"}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">排除过滤</span><span class="preview-summary-v">${esc(excludeParts.length ? excludeParts.join(" · ") : "未设置")}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">消息类型</span><span class="preview-summary-v">${esc(mtypes.length ? mtypes.join("、") : "全部类型")}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">内容格式</span><span class="preview-summary-v">${rawForwardVal ? "纯原样转发（无模板）" : "带模板通知"}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">原文件转发</span><span class="preview-summary-v">${forwardMediaVal ? `开启（跨平台 ≤${maxMB} MB）` : "关闭（仅文字摘要）"}</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">转发目标</span><span class="preview-summary-v">${validTargetCount} 个已就绪目标</span></div>
        <div class="preview-summary-row"><span class="preview-summary-k">防刷屏冷却</span><span class="preview-summary-v">${cd > 0 ? `${fmtDuration(cd)} · ${formatCooldownScopeLabel(scopeVal)}` : "不冷却"}</span></div>`;
    }
  };
  window._refreshRulePreview = refreshPreview;

  window._tUpd = (i, k, v) => {
    r.targets[i][k] = v;
    refreshPreview();
  };
  window._tDel = (i) => {
    r.targets.splice(i, 1);
    renderTargets();
    refreshPreview();
  };

  const syncCondVisibility = () => {
    const condBox = $("fCondSection");
    if (condBox) condBox.style.display = matchMode === "cond" ? "" : "none";
  };

  renderTargets();
  syncCondVisibility();
  refreshPreview();

  $("fRuleEnabled").onclick = () => {
    enabledVal = !enabledVal;
    $("fRuleEnabled").classList.toggle("on", enabledVal);
    $("fRuleEnabled").setAttribute("aria-pressed", enabledVal ? "true" : "false");
  };

  $("fForwardMedia").onclick = () => {
    forwardMediaVal = !forwardMediaVal;
    $("fForwardMedia").classList.toggle("on", forwardMediaVal);
    $("fForwardMedia").setAttribute("aria-pressed", forwardMediaVal ? "true" : "false");
    $("fMediaLimitRow").style.display = forwardMediaVal ? "" : "none";
    refreshPreview();
  };

  $("fModeSeg").querySelectorAll(".seg-btn").forEach((b) => {
    b.onclick = () => {
      $("fModeSeg").querySelectorAll(".seg-btn").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      matchMode = b.dataset.val;
      syncCondVisibility();
      refreshPreview();
    };
  });

  $("fScopeCards").querySelectorAll(".scope-card").forEach((card) => {
    card.onclick = () => {
      $("fScopeCards").querySelectorAll(".scope-card").forEach((x) => x.classList.remove("active"));
      card.classList.add("active");
      scopeVal = card.dataset.scope;
      refreshPreview();
    };
  });

  $("fFormatModeCards").querySelectorAll(".scope-card").forEach((card) => {
    card.onclick = () => {
      $("fFormatModeCards").querySelectorAll(".scope-card").forEach((x) => x.classList.remove("active"));
      card.classList.add("active");
      rawForwardVal = card.dataset.raw === "1";
      $("fRawModeNote").style.display = rawForwardVal ? "" : "none";
      $("fTplModeBox").style.display = rawForwardVal ? "none" : "";
      refreshPreview();
    };
  });

  $("fTplVarBar").querySelectorAll("[data-insert-var]").forEach((btn) => {
    btn.onclick = () => {
      insertAtCursor($("fTemplate"), btn.dataset.insertVar);
    };
  });

  $("btnTplDefault").onclick = () => {
    $("fTemplate").value = RULE_DEFAULT_TEMPLATE;
    refreshPreview();
  };
  $("btnTplMinimal").onclick = () => {
    $("fTemplate").value = RULE_MINIMAL_TEMPLATE;
    refreshPreview();
  };
  $("btnTplClear").onclick = () => {
    $("fTemplate").value = "";
    refreshPreview();
  };

  ["fName", "fChats", "fExcludeKeywords", "fCooldown", "fMaxMediaMB", "fTemplate"].forEach((id) => {
    const el = $(id);
    if (el) el.addEventListener("input", refreshPreview);
  });

  const quickChatsWrap = $("quickRuleChats");
  if (quickChatsWrap) {
    quickChatsWrap.querySelectorAll("[data-append-chat]").forEach((btn) => {
      btn.onclick = () => {
        const cur = splitCSV($("fChats").value);
        const id = btn.dataset.appendChat;
        if (id && !cur.includes(id)) {
          cur.push(id);
          $("fChats").value = cur.join(", ");
          refreshPreview();
        }
      };
    });
  }

  $("btnAddTarget").onclick = () => {
    r.targets.push({ type: defaultPlat, account_id: defaultAccId, to: "" });
    renderTargets();
    refreshPreview();
  };

  $("btnSaveRuleEditor").onclick = async () => {
    const btn = $("btnSaveRuleEditor");
    try {
      const name = $("fName").value.trim();
      if (!name) throw new Error("请填写规则名称");
      const validTargets = r.targets.filter((t) => t.to && t.to.trim() && t.account_id && t.account_id.trim());
      if (!validTargets.length) throw new Error("请至少完整填写一个转发目标（选择发送账号并填写接收目标 ID）");

      const watchMentions = splitCSV($("fMentions").value);
      const watchGroupIds = Array.from(document.querySelectorAll("#ruleEditorView [data-gid].on")).map((el) => el.dataset.gid);
      const keywords = splitCSV($("fKeywords").value);
      const watchAll = isToggleOn($("fWatchAll"));
      if (matchMode === "cond" && !watchMentions.length && !watchGroupIds.length && !keywords.length && !watchAll) {
        throw new Error("按条件触发模式下，请至少填写被 @ 个人、人员分组、关键词或开启 @所有人；若需全量同步请切换到「转发全部消息」");
      }

      const maxMB = Math.min(32, Math.max(1, Number($("fMaxMediaMB").value) || 10));
      const updated = {
        ...r,
        name,
        enabled: enabledVal,
        platforms: Array.from(document.querySelectorAll("#fPlatPills [data-plat].on")).map((el) => el.dataset.plat),
        chat_ids: splitCSV($("fChats").value),
        ignore_senders: splitCSV($("fIgnoreSenders").value),
        ignore_commands: $("fIgnoreCommands").classList.contains("on"),
        ignore_bots: $("fIgnoreBots").classList.contains("on"),
        exclude_keywords: splitCSV($("fExcludeKeywords").value),
        match_all_messages: matchMode === "all",
        message_types: Array.from(document.querySelectorAll("#fMsgTypePills [data-mtype].on")).map((el) => el.dataset.mtype),
        watch_mentions: watchMentions,
        watch_group_ids: watchGroupIds,
        keywords,
        watch_all: watchAll,
        cooldown_seconds: Math.max(0, Number($("fCooldown").value) || 0),
        cooldown_scope: scopeVal,
        forward_media: forwardMediaVal,
        max_media_mb: maxMB,
        raw_forward: rawForwardVal,
        template: $("fTemplate").value,
        targets: validTargets,
      };
      btn.disabled = true;
      if (isNew) state.config.monitor_rules.push(updated);
      else state.config.monitor_rules[idx] = updated;
      await saveConfig("转发规则已保存");
      closeRuleEditor();
    } catch (err) {
      toast(err.message || "保存失败", "error");
    } finally {
      btn.disabled = false;
    }
  };

  window.scrollTo({ top: 0, behavior: "smooth" });
};
window.openRuleModal = window.openRuleEditor;

// ── 人员分组 (@) ──
function renderGroups() {
  const groups = state.config.mention_groups || [];
  const rules = state.config.monitor_rules || [];
  if (!groups.length) {
    $("groupsGrid").innerHTML = emptyHTML("暂无人员分组", "点击右上角「新建人员分组」把常用联系人打包成组，再在转发规则中引用", true);
    return;
  }
  $("groupsGrid").innerHTML = groups
    .map((g, idx) => {
      const refCount = rules.filter((r) => (r.watch_group_ids || []).includes(g.id)).length;
      return `
    <div class="rule-card">
      <div class="rule-top">
        <div class="rule-head-copy">
          <div class="rule-title" title="${esc(g.name)}">${esc(g.name)}</div>
          <div class="rule-sub">标识: ${esc(g.id)}${g.description ? " · " + esc(g.description) : ""}</div>
        </div>
        <span class="badge ${refCount > 0 ? "emerald" : "muted"}">${refCount > 0 ? `已被 ${refCount} 条规则引用` : "尚未被规则引用"}</span>
      </div>
      <div class="tags">
        ${(g.members || []).map((m) => `<span class="badge primary">成员: ${esc(m)}</span>`).join("")}
        ${!(g.members || []).length ? `<span class="badge muted">暂未添加成员</span>` : ""}
      </div>
      <div class="rule-actions">
        <button type="button" class="btn btn-outline btn-sm" onclick="useGroupInNewRule('${esc(g.id)}')">用此组新建转发规则</button>
        <button type="button" class="btn btn-ghost btn-sm" onclick="openGroupModal(${idx})">编辑</button>
        <button type="button" class="btn btn-danger btn-sm" onclick="deleteGroup(${idx})">删除</button>
      </div>
    </div>`;
    })
    .join("");
}

window.useGroupInNewRule = (gid) => {
  switchTab("monitor");
  openRuleModal(-1, gid);
};

window.deleteGroup = async (idx) => {
  if (!(await askConfirm("删除人员分组", "确定删除这个人员分组吗？（不会删除已创建的转发规则，但会取消引用）", "删除"))) return;
  state.config.mention_groups.splice(idx, 1);
  await saveConfig("人员分组已删除");
};

window.openGroupModal = (idx = -1) => {
  const isNew = idx < 0;
  const g = isNew
    ? { id: "", name: "", description: "", members: [] }
    : JSON.parse(JSON.stringify(state.config.mention_groups[idx]));
  openModal(
    isNew ? "新建人员分组" : "编辑人员分组",
    `<div class="form-grid">
      <div class="field">
        <label>分组英文标识 *</label>
        <input type="text" id="gId" value="${esc(g.id)}" placeholder="例如 ops / sales" ${isNew ? "" : "disabled"} />
      </div>
      <div class="field">
        <label>分组名称 *</label>
        <input type="text" id="gName" value="${esc(g.name)}" placeholder="例如 运维值班组" />
      </div>
      <div class="field full">
        <label>用途说明（可选）</label>
        <input type="text" id="gDesc" value="${esc(g.description || "")}" placeholder="说明这个分组用于哪些值班或通知场景" />
      </div>
      <div class="field full">
        <label>组成员账号（逗号分隔，可混填各平台账号 ID、用户名或昵称）</label>
        <textarea id="gMembers" rows="3" placeholder="填写账号 ID、用户名或昵称，多个用逗号分隔">${esc((g.members || []).join(", "))}</textarea>
        <span class="field-hint">群里有人 @ 上述任意一位成员时，引用本分组的转发规则就会触发；关键词与 @所有人 请统一在转发规则中配置。</span>
      </div>
    </div>`,
    async () => {
      const id = $("gId").value.trim();
      const name = $("gName").value.trim();
      if (!id || !name) throw new Error("请填写分组标识与名称");
      if (isNew && (state.config.mention_groups || []).some((x) => x.id === id)) {
        throw new Error("分组英文标识已存在，请换一个标识");
      }
      const next = {
        id,
        name,
        description: $("gDesc").value.trim(),
        members: splitCSV($("gMembers").value),
      };
      if (isNew) state.config.mention_groups.push(next);
      else state.config.mention_groups[idx] = next;
      await saveConfig("人员分组已保存");
    }
  );
};

// ── 多平台通道连通性测试弹窗 ──
function openTestNotifyModal() {
  const availPlatforms = getAvailablePlatforms();
  let testType = availPlatforms[0] || "wechat";
  let testAccount = "";

  const mountTestAcc = () => {
    const list = (state.overview.accounts || []).filter((a) => a.platform === testType);
    if (!list.some((a) => a.id === testAccount)) {
      const online = list.find((a) => a.online) || list[0];
      testAccount = online?.id || "";
    }
    mountSelect(
      $("mTestAccount"),
      list.map((a) => ({
        value: a.id,
        label: accountDisplayName(a),
        sub: a.id,
        dot: a.online ? "online" : "offline",
        tag: a.online ? "" : "离线",
      })),
      testAccount,
      (v) => {
        testAccount = v;
      }
    );
    const toInput = $("mTestTo");
    if (toInput) {
      toInput.placeholder = platformTargetPlaceholder(testType);
    }
  };

  openModal(
    "通道连通性测试",
    `<div class="form-grid">
      <div class="field full">
        <label>发送平台</label>
        <div class="segmented" id="mTestTypeSeg">
          ${availPlatforms
            .map(
              (p, i) =>
                `<button type="button" class="seg-btn ${i === 0 ? "active" : ""}" data-val="${esc(p)}">${esc(formatPlatformName(p))}</button>`
            )
            .join("")}
        </div>
      </div>
      <div class="field full">
        <label>发送账号</label>
        <div class="cselect" id="mTestAccount" data-placeholder="选择在线账号"></div>
      </div>
      <div class="field full">
        <label for="mTestTo">接收目标（群 ID 或个人 ID）</label>
        <input type="text" id="mTestTo" placeholder="${esc(platformTargetPlaceholder(testType))}" />
      </div>
      <div class="field full">
        <label for="mTestBody">测试消息正文（留空使用默认文案）</label>
        <textarea id="mTestBody" rows="3" placeholder="留空将发送带时间戳的默认测试消息"></textarea>
      </div>
    </div>`,
    async () => {
      const to = $("mTestTo").value.trim();
      if (!testAccount) throw new Error("请选择发送账号");
      if (!to) throw new Error("请填写接收目标 ID");
      await api("/api/test-notify", {
        method: "POST",
        body: JSON.stringify({
          target: { type: testType, account_id: testAccount, to },
          body: $("mTestBody").value.trim(),
        }),
      });
      toast("测试消息发送成功");
    },
    "发送测试"
  );

  mountTestAcc();
  $("mTestTypeSeg").querySelectorAll(".seg-btn").forEach((b) => {
    b.onclick = () => {
      $("mTestTypeSeg").querySelectorAll(".seg-btn").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      testType = b.dataset.val;
      testAccount = "";
      mountTestAcc();
    };
  });
}

// ── Modal 基础 ──
function openModal(title, bodyHTML, onSave, saveText = "保存") {
  $("modalTitle").textContent = title;
  $("modalBody").innerHTML = bodyHTML;
  $("modalFooter").innerHTML = `
    <button type="button" class="btn btn-outline" id="mCancel">取消</button>
    <button type="button" class="btn btn-primary" id="mSave">${esc(saveText)}</button>
  `;
  const backdrop = $("modalBackdrop");
  backdrop.classList.add("open");
  backdrop.setAttribute("aria-hidden", "false");
  syncBodyModalLock();
  $("mCancel").onclick = closeModal;
  $("mSave").onclick = async () => {
    try {
      await onSave();
      closeModal();
    } catch (e) {
      toast(e.message, "error");
    }
  };
}

function closeModal() {
  const backdrop = $("modalBackdrop");
  backdrop.classList.remove("open");
  backdrop.setAttribute("aria-hidden", "true");
  syncBodyModalLock();
}

// ── Tab 切换 ──
function switchTab(tab) {
  if (!TAB_META[tab]) return;
  state.activeTab = tab;
  closeAllSelects();
  if (tab !== "wechat" && state.wxDetail) {
    closeWxDetailView();
  }
  if (tab !== "monitor" && state.ruleEditing) {
    closeRuleEditor();
  }
  document.querySelectorAll(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
  document.querySelectorAll(".tab-panel").forEach((p) => p.classList.toggle("active", p.id === `tab-${tab}`));
  $("pageTitle").textContent = TAB_META[tab][0];
  $("pageDesc").textContent = TAB_META[tab][1];
  if (tab === "logs") reloadLogs(false);
}

document.querySelectorAll(".nav-item").forEach((btn) => {
  btn.onclick = () => switchTab(btn.dataset.tab);
});

// ── 审计日志加载与清空 ──
function updateClearLogsButtonLabel() {
  const cat = state.logCategory;
  const label = (LOG_CAT_META[cat] && LOG_CAT_META[cat][0]) || cat;
  $("btnClearLogsText").textContent = cat === "all" ? "清空全部日志" : `清空${label}日志`;
}

async function reloadLogs(withFeedback = false) {
  const btn = withFeedback ? $("btnReloadLogs") : null;
  if (btn) setBtnLoading(btn, true);
  try {
    const res = await api(`/api/logs?category=${encodeURIComponent(state.logCategory)}&limit=150`);
    const logs = res.logs || [];
    $("btnClearLogs").disabled = logs.length === 0;
    renderLogList($("fullLogs"), logs);
    if (withFeedback) toast("日志已刷新");
  } catch (e) {
    toast("加载日志失败: " + e.message, "error");
  } finally {
    if (btn) setBtnLoading(btn, false);
  }
}

async function clearLogsWithConfirm(category) {
  const isAll = !category || category === "all";
  const catLabel = (LOG_CAT_META[category] && LOG_CAT_META[category][0]) || category;
  const title = isAll ? "清空全部审计日志" : `清空「${catLabel}」日志`;
  const desc = isAll
    ? "将同时清空内存与持久化文件（events.jsonl）中的全部历史日志，清空后无法恢复。"
    : `将清除所有分类为「${catLabel}」的历史日志，其余分类日志保持不变。`;
  if (!(await askConfirm(title, desc, "确认清空"))) return;

  try {
    await api("/api/logs/clear", {
      method: "POST",
      body: JSON.stringify({ category: isAll ? "all" : category }),
    });
    toast(isAll ? "已清空全部日志" : `已清空${catLabel}日志`);
    await loadAll(false);
    if (state.activeTab === "logs") {
      await reloadLogs(false);
    }
  } catch (e) {
    toast("清空日志失败: " + e.message, "error");
  }
}

// ── 事件绑定 ──
$("logFilters").querySelectorAll(".seg-btn").forEach((b) => {
  b.onclick = () => {
    $("logFilters").querySelectorAll(".seg-btn").forEach((x) => x.classList.remove("active"));
    b.classList.add("active");
    state.logCategory = b.dataset.cat;
    updateClearLogsButtonLabel();
    reloadLogs(false);
  };
});

$("btnReloadLogs").onclick = () => reloadLogs(true);
$("btnClearLogs").onclick = () => clearLogsWithConfirm(state.logCategory);
$("btnClearOverviewLogs").onclick = () => clearLogsWithConfirm("all");
$("btnGotoLogs").onclick = () => switchTab("logs");

$("btnClearWxChanges").onclick = async () => {
  if (!(await askConfirm("清空入退群变动记录", "确定清空当前展示的微信入群 / 离群动态列表吗？（不影响群成员监控基线）", "清空"))) return;
  try {
    await api("/api/wechat/changes/clear", { method: "POST" });
    toast("已清空微信入退群动态");
    await loadAll(false);
  } catch (e) {
    toast("清空失败: " + e.message, "error");
  }
};

if ($("btnAddWxRoom")) $("btnAddWxRoom").onclick = () => openWxRoomModal(-1);
if ($("btnAddTgChat")) $("btnAddTgChat").onclick = () => openTgModal(-1);
if ($("btnTgManualUnmute")) $("btnTgManualUnmute").onclick = () => openTgManualUnmuteModal();
if ($("btnAddGlobalBlacklist")) $("btnAddGlobalBlacklist").onclick = () => openGlobalBlacklistModal();
if ($("blSearchInput")) {
  $("blSearchInput").oninput = (e) => {
    state.blSearch = e.target.value;
    renderGlobalBlacklist();
  };
}
if ($("btnAddRule")) $("btnAddRule").onclick = () => openRuleModal(-1);
if ($("btnOpenTestModal")) $("btnOpenTestModal").onclick = () => openTestNotifyModal();
if ($("btnAddGroup")) $("btnAddGroup").onclick = () => openGroupModal(-1);
if ($("modalClose")) $("modalClose").onclick = closeModal;

document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (document.querySelector(".cselect.open")) {
    closeAllSelects();
    return;
  }
  if ($("confirmBackdrop").classList.contains("open")) {
    closeConfirm(false);
    return;
  }
  if ($("modalBackdrop").classList.contains("open")) {
    closeModal();
  }
});

$("btnWxDiscover").onclick = async () => {
  if (!state.selectedWxAccount) return toast("请先选择一个在线微信账号", "error");
  const btn = $("btnWxDiscover");
  setBtnLoading(btn, true);
  toast("正在同步微信群列表，请稍候...");
  try {
    const res = await api("/api/wechat/discover", { method: "POST", body: JSON.stringify({ account_id: state.selectedWxAccount }) });
    toast(`同步完成，发现 ${(res.rooms || []).length} 个普通微信群`);
    await loadAll(false);
  } catch (e) {
    toast("同步失败: " + e.message, "error");
  } finally {
    setBtnLoading(btn, false);
  }
};

$("btnClearCooldowns").onclick = async () => {
  try {
    await api("/api/cooldowns/clear", { method: "POST" });
    toast("已清空全部冷却窗口");
    await loadAll(false);
  } catch (e) {
    toast("清空冷却失败: " + e.message, "error");
  }
};

updateClearLogsButtonLabel();
loadAll(false);
setInterval(() => {
  api("/api/overview")
    .then((ov) => {
      state.overview = ov;
      renderOverview();
      renderWechatPanel();
      renderTgChats();
      renderGlobalBlacklist();
    })
    .catch(() => {});
}, 5000);
