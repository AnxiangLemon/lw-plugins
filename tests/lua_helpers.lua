-- Run from the repository root: lua tests/lua_helpers.lua
local sent, handlers = {}, {}
local plugin = {}
function plugin.reply(event, text)
    event._reply_index = (event._reply_index or 0) + 1
    local key = "reply:" .. event.event_id .. ":" .. event._reply_index
    sent[#sent + 1] = { platform = event.message.platform, key = key, text = text }
    return key
end
function plugin.wechat(account)
    return { send = function(_, peer, segments, options)
        sent[#sent + 1] = { platform = "wechat", key = options.idempotency_key }
        return options.idempotency_key
    end }
end
function plugin.on(name, handler) handlers[name] = handler end
package.loaded["lw.plugin"] = plugin
local L = dofile("lua/lib/L.lua")
package.loaded["L"] = L
for _, platform in ipairs({"wechat", "telegram", "third"}) do
    local function replay()
        local message = { platform = platform, event_id = "event-1", peer_id = "peer", reply_to_id = "peer" }
        assert(L.reply("bot", message, "first") == "reply:event-1:1")
        assert(L.reply("bot", message, "second") == "reply:event-1:2")
        assert(sent[#sent].platform == platform)
    end
    replay()
    replay() -- Replaying an event must reproduce the same two keys.
end
-- Exercise the actual demo handlers; a generic WeChat event must not reply twice.
L.is_self = function() return false end
dofile("lua/demo.lua")
local count = #sent
handlers["wechat.message.received"]({account_id="bot", event_id="ping", message={platform="wechat", peer_id="peer", text="!ping"}})
handlers["message.received"]({account_id="bot", event_id="ping", message={platform="wechat", peer_id="peer", text="!ping"}})
assert(#sent == count + 1)
handlers["message.received"]({account_id="bot", event_id="tg-ping", message={platform="telegram", reply_to_id="peer", text="!ping"}})
assert(#sent == count + 2)
print("Lua reply sequencing, replay and demo routing passed")
