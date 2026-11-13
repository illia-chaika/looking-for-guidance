-- The quest check: what the client can tell at once, then a researched verdict with sources.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local ready = wow.Samples(here .. "/../samples/samples.json").payloads.ready
for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
wow.payload = ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
T.CharacterIdentity = function() return "Player-1-121A", "login" end
T.settings.locale = "enUS"

local log = {
    { questID = 1234, title = "A Taste of the Sea", level = 12 },
    { questID = 77, title = "Into the Depths", level = 40 },
}
local free, done = 4, false
C_QuestLog = {
    GetNumQuestLogEntries = function() return #log end,
    GetInfo = function(i) return log[i] end,
    GetSelectedQuest = function() return 1234 end,
    GetQuestObjectives = function(id)
        return id == 1234 and { { text = "Saltwater Murloc Eyes: 0/12", numFulfilled = 0, numRequired = 12 } } or {}
    end,
    IsComplete = function(id) return id == 1234 and done end,
    GetQuestTagInfo = function(id) return id == 77 and { tagID = 81 } or nil end,
}
C_Container = {
    GetContainerNumSlots = function() return 0 end,
    GetContainerNumFreeSlots = function(b) return b == 0 and free or 0, 0 end,
}
function GetRealZoneText() return "Westfall" end
local function card(quest) return hub.Cards(hub.Model({}), quest).stuck end
local function row(c, title)
    for _, r in ipairs(c.rows) do if r.title == title then return r end end
end
local function sent()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" and packet.kind == "quest" then list[#list + 1] = packet end end
    return list
end

local c = card()
assert(c.quest.id == 1234 and c.rows[#c.rows].kind == "actions" and c.rows[#c.rows].actions[1].label == "Check against hotfixes")
assert(#sent() == 0, "opening the card asks nothing: research starts with the button")
free = 0
assert(row(card(), "Bags are full").state == "warn", "full bags are the first thing to rule out")
free = 4
local deep = card(77)
assert(row(deep, "The quest is above your level").text == "The quest is level 40; you are level 33." and row(deep, "Dungeon quest"))
done = true
assert(card().rows[1].tone == "good" and card().rows[1].title == "The quest is complete")
done = false

c = card()
c.rows[#c.rows].actions[1].click()
local asked = sent()
assert(#asked == 1 and asked[1].key == "q1234:MAGE:Alliance" and T.Store.Tracked(asked[1].seq), "the button sends one watched request")
local f = asked[1].facts
assert(f.id == 1234 and f.title == "A Taste of the Sea" and f.objectives[1].need == 12 and f.objectives[1].done == 0)
assert(f.zone == "Westfall" and f.class == "MAGE" and f.plevel == 33 and f.items == nil)
assert(card().rows[1].kind == "pending")

local function answer(data, extra)
    local payload = CopyTable(ready)
    payload.rev = (wow.payload.rev or ready.rev) + 1
    payload.insights = { { kind = "quest", key = "q1234:MAGE:Alliance", state = "ready", at = time(), data = data } }
    for _, item in ipairs(extra or {}) do payload.insights[#payload.insights + 1] = item end
    wow.payload = payload
    T.Sync.Check()
    return card()
end
c = answer({ verdict = "fixed", confidence = "confirmed", summary = "Blizzard fixed the drop on 10 November. Try again.",
    cause = "Murloc Tideskimmers did not drop the eyes.", forever_verified = true,
    steps = { { title = "Kill Murloc Tideskimmers", text = "The eyes drop from them again." } },
    sources = { { title = "WoW: Forever Hotfixes", url = "https://news.blizzard.com/en-us/article/24315281", quote = "Fixed an issue…", date = "2026-11-10" } } })
assert(c.rows[1].tone == "good" and c.rows[1].title == "Blizzard has fixed this" and c.rows[1].text:find("10 November", 1, true))
assert(c.rows[2].text == "Murloc Tideskimmers did not drop the eyes." and row(c, "Kill Murloc Tideskimmers"))
local sources = c.rows[#c.rows - 1]
assert(sources.kind == "sources" and sources.list[1].title == "2026-11-10 · WoW: Forever Hotfixes" and c.summary == "Blizzard has fixed this")
for _, proof in ipairs(c.evidence) do assert(proof.text ~= "Game fixes were not checked.", "a checked quest no longer says it was not checked") end

c = answer({ verdict = "bug", confidence = "likely", summary = "Players report the escort NPC vanishing.", cause = "", forever_verified = false, steps = {},
    sources = { { title = "Old guide", url = "https://example.org/classic", quote = "…", date = "" } } })
assert(c.rows[1].tone == "bad" and c.rows[1].title == "Known bug · likely")
assert(c.rows[2].text == "The sources are not about Forever directly: verify in game.", "an unverified source is labelled")

c = answer({ verdict = "unknown", confidence = "unverified", summary = "Nothing reliable found.", cause = "", forever_verified = false, steps = {}, sources = {} },
    { { kind = "digest", key = "me", state = "ready", at = time(), data = { headline = "x", items = { { quest_id = 1234, text = "The eyes drop again.", relevance = "direct", date = "2026-11-10" } } } } })
assert(c.rows[1].title == "Blizzard mentions this quest in the hotfixes" and c.rows[1].text == "The eyes drop again.", "the login digest already flags the quest")
assert(c.rows[2].title == "No confirmation of a bug was found · unconfirmed" and c.rows[2].tone == nil)

print("Quest check: client checks, button-only research, verdicts, doubt and Forever labels, digest flag OK")
