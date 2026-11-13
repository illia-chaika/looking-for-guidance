-- "Where to level": client facts first, researched zones on request, one badge per five-level range.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local ready = wow.Samples(here .. "/../samples/samples.json").payloads.ready
for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
wow.payload = ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
T.CharacterIdentity = function() return "Player-1-121A", "login" end
T.settings.locale = "enUS"
T.HubData.AskBrief = function() return false end
T.Insights.Request = (function(original) return function(kind, ...) if kind == "digest" then return false end return original(kind, ...) end end)(T.Insights.Request)
local level = 33
function UnitLevel() return level end
function UnitFactionGroup() return "Alliance" end
function GetRealZoneText() return "Arathi Highlands" end
local function card() return hub.Cards(hub.Model({})).leveling end
local function row(c, title)
    for _, r in ipairs(c.rows or {}) do if r.title == title then return r end end
end
local function sent()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" and packet.kind == "leveling" then list[#list + 1] = packet end end
    return list
end

local c = card()
assert(c.kicker == "Levels 30–34" and row(c, "No quests of your level in the log").state == "warn", "a level 36 quest is out of range for level 33")
assert(row(c, "You have rested experience") and c.signal == "range:30" and c.priority == 45)
assert(#sent() == 0, "nothing is researched until the player opens the card")
c.rows[#c.rows].actions[1].click()
local asked = sent()
assert(#asked == 1 and asked[1].key == "Alliance:30:MAGE" and asked[1].facts.from == 30 and asked[1].facts.to == 34 and asked[1].facts.zone == "Arathi Highlands")

local payload = CopyTable(ready)
payload.rev = ready.rev + 1
payload.insights = { { kind = "leveling", key = "Alliance:30:MAGE", state = "ready", at = time(), data = {
    summary = "Quest in Arathi Highlands first; move to Desolace when it is crowded.",
    zones = { { name = "Arathi Highlands", levels = "30-40", why = "Dense quests.", crowd = "busy" }, { name = "Desolace", levels = "30-39", why = "Few players.", crowd = "quiet" },
        { name = "Riverglades", levels = "28-34", why = "A new Forever zone.", crowd = "unknown" } },
    dungeons = { { name = "Scarlet Monastery", levels = "30-40", why = "Four wings with quests.", crowd = "unknown" } },
    tips = { "Eat cooked food for +5% kill experience." }, sources = { { title = "Zones by level", url = "https://example.org/forever/zones" } }, forever_verified = true } } }
wow.payload = payload
T.Sync.Check()
c = card()
assert(c.title == "Arathi Highlands" and c.reason:find("Desolace", 1, true) and c.summary == "Arathi Highlands")
local arathi = row(c, "Arathi Highlands · you are here")
assert(arathi and arathi.state == "done" and arathi.value == "30-40" and arathi.text == "Dense quests. · reported busy")
assert(row(c, "Desolace").text == "Few players. · reported quiet" and row(c, "Riverglades").text == "A new Forever zone.", "an unknown crowd is not labelled")
assert(row(c, "Scarlet Monastery").value == "30-40" and row(c, "Eat cooked food for +5% kill experience."))
local honest
for _, r in ipairs(c.rows) do honest = honest or r.text == "The game does not tell add-ons how crowded a zone is: this comes from guides and player reports." end
assert(honest, "the card says where crowd information comes from")

-- No badge at login or inside a range; one when a new range begins.
hub.Observe(hub.Cards(hub.Model({})))
assert(hub.UnreadCount() == 0)
level = 34
hub.Observe(hub.Cards(hub.Model({})))
assert(hub.UnreadCount() == 0 and card().signal == "range:30")
level = 35
hub.Observe(hub.Cards(hub.Model({})))
assert(hub.UnreadCount() == 1 and card().signal == "range:35" and card().rows[#card().rows].kind == "actions", "a new range asks again")
level = 60
assert(card().rows == nil and card().reason == "At the level cap no levelling route is needed.")
print("Leveling: client facts, on-demand research, current zone, crowd labels with their origin, a badge only when a new range begins OK")
