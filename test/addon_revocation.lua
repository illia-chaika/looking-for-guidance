-- Drive the real mailbox, settings and 40 ms frame queue when sharing changes during delivery.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local guid = "Player-123-000A"
function UnitGUID(unit) if unit == "player" then return guid end end
local T = wow.Load(here .. "/../looking-for-guidance")
local _, login = T.CharacterIdentity()
T.Insights.Request = function() return false end
C_AddOns.GetNumAddOns = function() return 1 end
C_AddOns.GetAddOnName = function() return "PrivateInventoryMarker" end
wow.Run(30)

local completed, serial = {}, 0
T.On("PacketSent", function(seq) completed[seq] = (completed[seq] or 0) + 1 end)
local function poll()
    looking_for_guidance_receive("slot")
end
local function request()
    serial = serial + 1
    local id = "inventory-" .. serial
    wow.payload.detailRequest = { id = id, session = T.Transport.session, character = guid, login = login,
        addons = { id = string.format("%032x", serial), offset = 0 } }
    poll()
    local packet = wow.sent[#wow.sent]
    assert(packet.requestId == id and #packet.addons.rows == packet.addons.total and packet.addons.total == 1)
    return packet.seq
end
local function drain(seq)
    while T.Transport.Sending(seq) do wow.Run(0.01) end
end
local function visible(match)
    for _, f in ipairs(wow.frames) do if f:IsVisible() and match(f) then return f end end
    error("missing settings control")
end
local function click(caption)
    visible(function(f) return (f.label and f.label:GetText() == caption) or (f.text and f.text:GetText() == caption) end):Fire("OnClick")
end
local function toggleSharing()
    visible(function(f) return f.glyph and f.glyph.name == "gear" end):Fire("OnClick")
    click("Данные персонажа")
    click("Аддоны")
    click("Сохранить")
end

-- Save off and on between frames. The preceding ordinary packet and a later new request still work.
T.Window.Show()
local ordinary = assert(T.Transport.Send({ type = "read", threadId = "ordinary" }))
local queued = request()
wow.payload.detailRequest = nil
local before = #wow.bits
toggleSharing()
assert(not T.settings.context.addons and not T.Store.Tracked(queued) and T.Store.Delivery(queued) == "failed")
toggleSharing()
assert(T.settings.context.addons and not T.Transport.Retry(queued) and not T.Transport.Sending(queued))
assert(#wow.bits == before, "revocation happened before a frame was written")
drain(ordinary)
assert(completed[ordinary] == 1 and not completed[queued])
looking_for_guidance_window:Hide()
wow.Run(30)

-- This is the final (only) page. Stop after a few frames and verify the very next frame starts a fresh packet.
local partial = request()
local start = #wow.bits
wow.Run(0.13)
assert(#wow.bits > start + 1 and wow.bits[start + 1]:byte(1) >= 128 and T.Transport.Sending(partial))
T.settings.extras = false
before = #wow.bits
poll()
local denial = wow.sent[#wow.sent]
assert(denial.addons.error and not denial.addons.rows and not denial.addons.total)
drain(denial.seq)
assert(wow.bits[before + 1]:byte(1) >= 128, "the helper must see a fresh start instead of another private frame")
assert(completed[denial.seq] == 1 and not completed[partial] and not T.Transport.Sending(partial))
T.settings.extras = true
wow.payload.detailRequest = nil
wow.Run(30)

-- Missing acknowledgements queue both an automatic retry and an explicit mailbox replay after that retry fails.
-- Revoke the active character while each replay is queued, with no new settings or mailbox event.
for _, manual in ipairs({ false, true }) do
    local seq = request()
    drain(seq)
    assert(completed[seq] == 1)
    wow.now = wow.now + 2
    poll()
    assert(T.Store.Retried(seq) and T.Transport.Sending(seq))
    if manual then
        drain(seq)
        wow.now = wow.now + 2
        poll()
        assert(completed[seq] == 2 and T.Transport.Sending(seq), "same mailbox replays the failed retry")
    end
    wow.payload.detailRequest = nil
    local count = completed[seq]
    before = #wow.bits
    guid = "Player-123-000B"
    wow.Run(0.05)
    assert(#wow.bits == before and not T.Transport.Sending(seq) and not T.Store.Tracked(seq))
    guid = "Player-123-000A"
    assert(not T.Transport.Retry(seq) and completed[seq] == count, "returning to the original identity cannot revive a revoked page")
    wow.Run(30)
end

-- An identity change observed between frames also expires the rest of a frozen multipage scan.
C_AddOns.GetNumAddOns = function() return 300 end
C_AddOns.GetAddOnName = function(i) return "PrivateInventoryMarker" .. i end
local multi = { id = "multi-first", session = T.Transport.session, character = guid, login = login,
    addons = { id = string.rep("f", 32), offset = 0 } }
wow.payload.detailRequest = multi
poll()
local first = wow.sent[#wow.sent]
assert(#first.addons.rows < first.addons.total)
wow.Run(0.13)
guid = "Player-123-000B"
wow.Run(0.05)
assert(not T.Transport.Sending(first.seq) and not completed[first.seq])
guid = "Player-123-000A"
multi.id, multi.addons.offset = "multi-next", #first.addons.rows
poll()
local expired = wow.sent[#wow.sent].addons
assert(expired.error and not expired.rows, "returning to the same identity cannot continue a revoked scan")
print("Installed add-on sharing revokes queued pages, partial final pages and both replay paths")
