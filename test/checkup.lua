-- The character check-up reads only what the client reports and stays silent when it cannot tell.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
T.CharacterIdentity = function() return "Player-1-121A", "login" end
T.settings.locale = "enUS"

local points, combat, ranged, fed = 2, false, nil, false
C_Traits.GetTreeCurrencyInfo = function() return { { traitCurrencyID = 9, quantity = points, spent = 3 } } end
C_Traits.GetConfigInfo = function() return { treeIDs = { 1, 2, 3 } } end
function InCombatLockdown() return combat end
function GetInventorySlotInfo(name) return name == "RangedSlot" and 18 or nil end
function GetInventoryItemID(_, slot) return slot == 18 and ranged or (slot == 16 and 15275 or nil) end
function GetInventoryItemDurability(slot) if slot == 16 then return 3, 40 end end
C_SpellBook.IsSpellKnown = function(id) return id == 5019 end
Enum = Enum or {}
Enum.SpellBookSpellBank = { Player = 0 }
Enum.SpellBookItemType = { FutureSpell = 2, Spell = 1 }
C_SpellBook.GetSpellBookSkillLineInfo = function() return { itemIndexOffset = 0, numSpellBookItems = 3, shouldHide = false } end
C_SpellBook.GetSpellBookItemInfo = function(slot)
    return ({ { itemType = 1, name = "Fireball" }, { itemType = 2, name = "Frost Nova" }, { itemType = 2, name = "Blink" } })[slot]
end
C_SpellBook.GetSpellBookItemLevelLearned = function(slot) return ({ 1, 30, 40 })[slot] end
C_Spell.GetSpellName = function(id) return id == 19705 and "Well Fed" or nil end
C_UnitAuras.GetAuraDataByIndex = function(_, index, filter)
    if filter ~= "HELPFUL" then return end
    if index == 1 then return { spellId = 1459, name = "Arcane Intellect" } end
    if index == 2 and fed then return { spellId = 19705, name = "Well Fed" } end
end
local skills = { { skillID = 129, name = "First Aid", rank = 75, maxRank = 75 }, { skillID = 185, name = "Cooking", rank = 40, maxRank = 150 },
    { skillID = 43, name = "Swords", rank = 10, maxRank = 165 } }
C_SkillInfo.GetNumSkillLines = function() return #skills end
C_SkillInfo.GetSkillLineInfo = function(index) return skills[index] end

local function card() return hub.Cards(hub.Model({})).checkup end
local function row(c, title)
    for _, r in ipairs(c.rows) do if r.title == title then return r end end
end

local c = card()
assert(c.title == "Issues: 5", c.title)
assert(row(c, "Unspent talent points").value == "2", "three trees sharing one pool are counted once")
local spells = row(c, "New spells are waiting at the trainer")
assert(spells.value == "1" and spells.text == "Frost Nova", "only spells for a reached level count")
assert(row(c, "No wand").state == "warn" and row(c, "Gear is nearly broken").value == "1")
assert(row(c, "No food bonus").state == "todo", "a missing food bonus is a hint, not an issue")
assert(row(c, "First Aid").state == "warn" and row(c, "First Aid").value == "75/75")
assert(row(c, "Cooking").text == "Until the next campfire for your camp: 50" and not row(c, "Swords"))
assert(c.signal == "33:2:1" and c.priority == 70, "a new level's points and spells raise one badge")

combat = true
c = card()
assert(not row(c, "No food bonus") and not row(c, "Food bonus is active"), "hidden auras read as unknown in combat")
combat, fed, ranged, points = false, true, 5240, 0
skills[1].rank = 60
C_SpellBook.GetSpellBookItemLevelLearned = function() return 60 end
function GetInventoryItemDurability() return 40, 40 end
c = card()
assert(c.title == "All is well" and c.rows[1].kind == "verdict" and c.rows[1].tone == "good", c.title)
assert(row(c, "Food bonus is active").state == "done" and row(c, "Talent points are spent").state == "done")
assert(not c.signal and c.priority == 0)

C_Traits.GetTreeCurrencyInfo = nil
function GetInventorySlotInfo() error("no ranged slot in this client") end
ranged = nil
c = card()
assert(not row(c, "Unspent talent points") and not row(c, "Talent points are spent") and not row(c, "No wand"),
    "a client without these readers gives no verdict about them")

T.settings.context.character = false
assert(card().reason == "Access disabled")
T.settings.context.character = true

-- The card opens from the overview grid and reads as a checklist.
T.Window.Show()
local function visible(text)
    for _, f in ipairs(wow.texts) do if f:IsVisible() and f:GetText() == text then return f end end
end
assert(visible("Character check-up"), "the overview lists the check-up")
print("Check-up: shared talent pool, reached-level spells, wand, wear, food, skill caps, combat and missing readers OK")
