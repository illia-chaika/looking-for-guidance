-- Dungeon preparation: researched quest list checked against the quest log, for any dungeon the client offers.
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
function UnitFactionGroup() return "Horde" end
function UnitLevel() return 22 end
local log = { { questID = 9, title = "Blackfathom Villainy", level = 24 } }
C_QuestLog = {
    GetNumQuestLogEntries = function() return #log end, GetInfo = function(i) return log[i] end,
    GetQuestObjectives = function() return {} end, IsComplete = function() return false end,
}
function GetLFDChoiceOrder() return { 10 } end
function GetLFGDungeonInfo() return "Blackfathom Deeps", 1, 1, 20, 30, 24, 22, 27, 0, nil, nil, nil, 5 end

local function card() return hub.Cards(hub.Model({}), nil, "Blackfathom Deeps").dungeon end
local function row(c, title)
    for _, r in ipairs(c.rows) do if r.title == title then return r end end
end
local function sent()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" and packet.kind == "dungeon" then list[#list + 1] = packet end end
    return list
end

local c = card()
assert(c.dungeon == "Blackfathom Deeps" and c.rows[2].actions[1].label == "Prepare the run", "a dungeon from the client's own list, with nothing hard-coded")
c.rows[2].actions[1].click()
local asked = sent()
assert(#asked == 1 and asked[1].key == "Blackfathom Deeps:Horde" and asked[1].facts.plevel == 22 and asked[1].facts.name == "Blackfathom Deeps")
assert(card().rows[1].kind == "pending")

local data = { name = "Blackfathom Deeps", level_min = 22, level_max = 27, entrance = "Ashenvale, Zoram Strand, at the end of the cave.",
    quests = {
        { name = "Blackfathom Villainy", min_level = 18, giver = "Argent Guard Thaelrid", zone = "inside", note = "", inside = false, faction = "Both" },
        { name = "The Essence of Aku'Mai", min_level = 17, giver = "Je'neu Sancrea", zone = "Ashenvale, Zoram'gar Outpost", note = "20 Sapphires of Aku'Mai", inside = false, faction = "Horde" },
        { name = "Allegiance to the Old Gods", min_level = 26, giver = "Damp Note", zone = "drop", note = "", inside = false, faction = "Both" },
        { name = "Baron Aquanis", min_level = 21, giver = "Strange Water Globe", zone = "", note = "Dropped by Baron Aquanis.", inside = true, faction = "Horde" },
    },
    tips = { "Bring Water Breathing." }, sources = { { title = "BFD quests", url = "https://example.org/forever/bfd" } }, forever_verified = true }
local payload = CopyTable(ready)
payload.rev = ready.rev + 1
payload.insights = { { kind = "dungeon", key = "Blackfathom Deeps:Horde", state = "ready", at = time(), data = data } }
wow.payload = payload
T.Sync.Check()
c = card()
assert(row(c, "Your level fits").value == "22–27")
assert(row(c, "Blackfathom Villainy").state == "done" and row(c, "Blackfathom Villainy").text == "Already in your log")
local todo = row(c, "The Essence of Aku'Mai")
assert(todo.state == "todo" and todo.text == "Je'neu Sancrea · Ashenvale, Zoram'gar Outpost · 20 Sapphires of Aku'Mai")
assert(row(c, "Allegiance to the Old Gods").state == "skip" and row(c, "Allegiance to the Old Gods").text:find("From level 26", 1, true))
assert(row(c, "Baron Aquanis").state == nil and row(c, "Entrance").text:find("Zoram Strand", 1, true) and row(c, "Bring Water Breathing."))
assert(c.summary == "Quests to pick up first: 1" and c.reason == c.summary)

function UnitLevel() return 20 end
c = card()
assert(c.rows[1].kind == "verdict" and c.rows[1].tone == "warn" and c.rows[1].title == "The dungeon is meant for levels 22–27")
function UnitLevel() return 22 end
log[#log + 1] = { questID = 10, title = "The Essence of Aku'Mai", level = 22 }
assert(card().summary == "All quests picked up")
data.forever_verified = false
payload.rev = payload.rev + 1
T.Sync.Check()
local labelled
for _, r in ipairs(card().rows) do labelled = labelled or r.text == "The sources are not about Forever directly: verify in game." end
assert(labelled)
print("Dungeon prep: any listed dungeon, one watched request, checklist against the log, level fit, inside quests, entrance and tips OK")
