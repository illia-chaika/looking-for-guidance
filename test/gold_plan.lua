-- Gold plan and class brief: the trainer's own prices, buy/skip advice and the road to riding.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local ready = wow.Samples(here .. "/../samples/samples.json").payloads.ready
for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
wow.payload = ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
T.CharacterIdentity = function() return "Player-1-121A", "login" end
T.settings.locale = "enUS"
function UnitClass() return "Warlock", "WARLOCK", 9 end
function UnitRace() return "Undead", "Scourge", 5 end
function UnitFactionGroup() return "Horde" end
local level, coin = 12, 1700
function UnitLevel() return level end
function GetMoney() return coin end

-- A trainer window: two ranks to buy now, three for later levels.
local services = {
    { "Corruption", "available", "Rank 2", 900, 0 },
    { "Curse of Weakness", "available", "Rank 2", 600, 0 },
    { "Drain Soul", "unavailable", "Rank 2", 3000, 24 },
    { "Curse of Weakness", "unavailable", "Rank 3", 2000, 22 },
    { "Howl of Terror", "unavailable", "Rank 1", 9000, 44 },
}
function GetNumTrainerServices() return #services end
function GetTrainerServiceInfo(i) local s = services[i]; return s[1], s[2], nil, nil, s[3] end
function GetTrainerServiceCost(i) return services[i][4] end
function GetTrainerServiceLevelReq(i) return services[i][5] end
function UnitName(unit) return unit == "npc" and "Kaal Soulreaper" or "Elandra" end
function UnitGUID(unit) return unit == "npc" and "Creature-0-1-0-1-4563-0" or "Creature-0-1-0-1-2601-0" end
C_Trainer = { GetTrainerType = function() return 0 end }

local function sent()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" and packet.kind == "brief" then list[#list + 1] = packet end end
    return list
end
wow.Event("PLAYER_ENTERING_WORLD", true)
wow.Run(20)
local asked = sent()
assert(#asked == 1 and asked[1].key == "WARLOCK:Horde:Scourge:10", "one brief per class and ten levels is prepared after login")
assert(asked[1].facts.from == 10 and asked[1].facts.to == 19 and asked[1].facts.level == 12)

T.Window.Show()
wow.Event("TRAINER_SHOW")
wow.Run(1)
local function card(id) return hub.Cards(hub.Model({}))[id or "trainer"] end
local function row(c, title)
    for _, r in ipairs(c.rows) do if r.title == title then return r end end
end
local c = card()
assert(row(c, "Corruption Rank 2").value == "0g 9s 0c" and row(c, "Corruption Rank 2").state == nil, "without a brief the rows are plain prices")
assert(row(c, "Drain Soul Rank 2").text == "Level 24" and not row(c, "Riding · level 40"), "upcoming ranks carry this trainer's prices")

local brief = { summary = "Level as Affliction and keep your pet alive.", stat_priority = { "Spell Power", "Stamina" },
    buy = { { name = "Corruption", rank = 2, level = 14, why = "Your main damage." } },
    skip = { { name = "Curse of Weakness", rank = 0, level = 12, why = "Rarely used solo." } },
    class_quests = { { name = "Halgar's Summons", level = 10, where = "Brill", reward = "Voidwalker" }, { name = "The Binding", level = 20, where = "Undercity", reward = "Succubus" } },
    riding = { level = 40, cost_copper = 1000000, note = "Warlocks get a free mount from a class quest." }, food = "Well Fed gives +5% kill experience.",
    sources = { { title = "Warlock guide", url = "https://example.org/forever/warlock" } }, forever_verified = true }
local payload = CopyTable(ready)
payload.rev = ready.rev + 1
payload.insights = { { kind = "brief", key = "WARLOCK:Horde:Scourge:10", state = "ready", at = time(), data = brief } }
wow.payload = payload
T.Sync.Check()

c = card()
assert(row(c, "Corruption Rank 2").state == "done" and row(c, "Corruption Rank 2").text == "Your main damage.")
local skip = row(c, "Curse of Weakness Rank 2")
assert(skip.state == "skip" and skip.text == "Can be skipped. Rarely used solo.", "advice for every rank matches rank 2")
assert(row(c, "Saved by skipping ranks").value == "0g 6s 0c")
local riding = row(c, "Riding · level 40")
assert(riding.value == "100g 0s 0c" and riding.text == "Warlocks get a free mount from a class quest.")
assert(row(c, "Training up to level 40").value == "0g 30s 0c", "ranks to skip and ranks past the goal are not counted")
assert(row(c, "In your purse").value == "0g 17s 0c" and not row(c, "You save per level"), "no pace is claimed from a single observation")
assert(row(c, "Curse of Weakness Rank 3").state == "skip" and c.rows[#c.rows].kind == "sources")

-- Two more levels show how fast this character saves, and the plan gets a verdict.
level, coin = 13, 2700
wow.Event("PLAYER_LEVEL_UP", 13)
level, coin = 14, 3700
wow.Event("PLAYER_LEVEL_UP", 14)
wow.Run(1)
c = card()
assert(row(c, "You save per level").value == "0g 10s 0c")
assert(c.rows[1].kind == "verdict" and c.rows[1].tone == "warn" and c.rows[1].title:find("By level 40 you will be about", 1, true), c.rows[1].title)
assert(c.summary == "Short of gold for the mount")
coin = 2000000
c = card()
assert(c.rows[1].tone == "good" and c.rows[1].title == "You will have enough gold by level 40")
assert(c.title == "Corruption Rank 2 · 0g 9s 0c" and #c.steps == 0)
services[1][2] = "unavailable"
wow.Event("TRAINER_UPDATE")
wow.Run(1)
assert(card().title == "Nothing needs buying right now", "when only skippable ranks are on offer the card says so")
services[1][2] = "available"
wow.Event("TRAINER_UPDATE")
wow.Run(1)

-- The same brief feeds the check-up.
local check = card("checkup")
assert(row(check, "Stat priority").text == "Spell Power › Stamina")
assert(row(check, "Class quest: Halgar's Summons").state == "todo" and row(check, "Class quest: The Binding").state == "skip", "a class quest for a later level is not due yet")
assert(row(check, "Food").text == "Well Fed gives +5% kill experience.")
brief.forever_verified = false
payload.rev = payload.rev + 1
T.Sync.Check()
local labelled
for _, r in ipairs(card("checkup").rows) do labelled = labelled or r.text == "The sources are not about Forever directly: verify in game." end
assert(labelled, "advice without a Forever source says so")

-- The renamed tile and a visible ledger.
local function visible(text)
    for _, f in ipairs(wow.texts) do if f:IsVisible() and f:GetText() == text then return f end end
end
wow.Event("PLAYER_MONEY")
wow.Run(1)
assert(visible("Corruption Rank 2 · 0g 9s 0c") and visible("Your main damage."), "the lead offers the rank the brief recommends, not the one it says to skip")
assert(visible("Gold and training") and visible("Trainer prices and the road to a mount"))
print("Gold plan: login brief, trainer prices now and ahead, buy/skip marks, riding ledger, saving pace, verdict and check-up advice OK")
