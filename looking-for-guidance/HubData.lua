local _, T = ...
-- Character facts shared by the journal cards and the helper's background advice.
local Hub = T.HubOverview
local call, clean, allowed = Hub.Call, Hub.Clean, Hub.Allowed
local Data = {}
T.HubData = Data

-- Skill line IDs are the same in every client language. English names go to the advisor.
Data.professions = {
    [171] = "Alchemy", [164] = "Blacksmithing", [333] = "Enchanting", [202] = "Engineering", [182] = "Herbalism",
    [165] = "Leatherworking", [186] = "Mining", [393] = "Skinning", [197] = "Tailoring",
    [185] = "Cooking", [129] = "First Aid", [356] = "Fishing",
}

function Data.Level()
    local level = allowed("character") and call(UnitLevel, "player")
    return type(level) == "number" and level > 0 and level or nil
end

function Data.Class()
    if not allowed("character") then return end
    local name, token = call(UnitClass, "player")
    return type(token) == "string" and token or nil, name
end

function Data.Professions()
    local list = {}
    if not allowed("skills") or not C_SkillInfo then return list end
    for index = 1, call(C_SkillInfo.GetNumSkillLines) or 0 do
        local skill = call(C_SkillInfo.GetSkillLineInfo, index)
        local id = skill and clean(skill.skillID)
        local rank, max = skill and clean(skill.rank), skill and clean(skill.maxRank)
        if id and Data.professions[id] and type(rank) == "number" and type(max) == "number" then
            list[#list + 1] = { id = id, english = Data.professions[id], name = clean(skill.name) or Data.professions[id], rank = rank, max = max }
        end
    end
    return list
end

-- Unspent talent points, or nil when the client does not say.
function Data.TalentPoints()
    if not allowed("talents") or not C_Traits then return end
    local group = call(C_SpecializationInfo and C_SpecializationInfo.GetActiveSpecGroup)
    local configID = group and call(C_SpecializationInfo.GetCombatConfigIDForSpecGroup, group)
    configID = configID or call(C_ClassTalents and C_ClassTalents.GetActiveConfigID)
    local config = configID and call(C_Traits.GetConfigInfo, configID)
    if not config or type(config.treeIDs) ~= "table" then return end
    local balances, total = {}, nil
    for _, treeID in ipairs(config.treeIDs) do
        for _, currency in ipairs(call(C_Traits.GetTreeCurrencyInfo, configID, treeID, true) or {}) do
            local id, quantity = clean(currency.traitCurrencyID), clean(currency.quantity)
            -- Trees that share one pool report the same currency.
            if id and type(quantity) == "number" and not balances[id] then
                balances[id] = true
                total = (total or 0) + quantity
            end
        end
    end
    return total
end

-- Spells the spellbook lists for a level the character has already reached.
function Data.TrainableSpells(level)
    local list = {}
    if not level or not allowed("spells") or not C_SpellBook or not Enum or not Enum.SpellBookSpellBank or not Enum.SpellBookItemType then return list end
    local bank = Enum.SpellBookSpellBank.Player
    for index = 1, call(C_SpellBook.GetNumSpellBookSkillLines) or 0 do
        local line = call(C_SpellBook.GetSpellBookSkillLineInfo, index)
        if line and not clean(line.shouldHide) and clean(line.itemIndexOffset) and clean(line.numSpellBookItems) then
            for slot = line.itemIndexOffset + 1, line.itemIndexOffset + line.numSpellBookItems do
                local spell = call(C_SpellBook.GetSpellBookItemInfo, slot, bank)
                if spell and clean(spell.itemType) == Enum.SpellBookItemType.FutureSpell and not clean(spell.isOffSpec) and clean(spell.name) then
                    local needed = call(C_SpellBook.GetSpellBookItemLevelLearned, slot, bank)
                    if type(needed) == "number" and needed > 0 and needed <= level then
                        list[#list + 1] = { name = spell.name, level = needed }
                    end
                end
            end
        end
    end
    return list
end

-- true / false, or nil when auras cannot be read (in combat they are hidden from add-ons).
function Data.HasAura(name)
    if not allowed("buffs") or not name or not C_UnitAuras or call(InCombatLockdown) then return end
    for index = 1, 40 do
        local aura = call(C_UnitAuras.GetAuraDataByIndex, "player", index, "HELPFUL")
        if not aura then return false end
        local found = clean(aura.name)
        if found == nil then return end
        if found == name then return true end
    end
    return false
end

function Data.Quests(limit)
    local list = {}
    if not allowed("quests") or not C_QuestLog then return list end
    for index = 1, call(C_QuestLog.GetNumQuestLogEntries) or 0 do
        local quest = call(C_QuestLog.GetInfo, index)
        if quest and not clean(quest.isHeader) and not clean(quest.isHidden) and clean(quest.questID) and clean(quest.title) and #list < (limit or 25) then
            list[#list + 1] = { id = quest.questID, title = quest.title, level = clean(quest.level) }
        end
    end
    return list
end

-- A list for the helper, or nil: an empty Lua table would read as an object there.
local function some(list) return #list > 0 and list or nil end
Data.Some = some

-- What the helper needs to tailor advice. No character name or GUID.
function Data.Profile()
    local token = Data.Class()
    local _, race = call(UnitRace, "player")
    local professions = {}
    for _, p in ipairs(Data.Professions()) do professions[#professions + 1] = { name = p.english, rank = p.rank, max = p.max } end
    return {
        class = token, race = allowed("character") and race or nil,
        faction = allowed("character") and call(UnitFactionGroup, "player") or nil,
        level = Data.Level(), professions = some(professions),
        zone = allowed("location") and call(GetRealZoneText) or nil,
    }
end

-- The class brief is shared by the check-up and the gold plan: one request per class and ten levels.
local function briefRequest()
    local profile = Data.Profile()
    if not profile.class or not profile.level then return end
    local from = math.floor(profile.level / 10) * 10
    local facts = { class = profile.class, race = profile.race, faction = profile.faction, level = profile.level,
        from = math.max(1, from), to = math.min(60, from + 9), professions = profile.professions }
    return "brief", table.concat({ profile.class, profile.faction or "", profile.race or "", from }, ":"), facts
end

function Data.CanBrief() return briefRequest() ~= nil end

function Data.Brief()
    local kind, key = briefRequest()
    if not kind then return end
    local state, found = T.Insights.State(kind, key)
    return state == "ready" and found.data or nil, state, found
end

function Data.AskBrief(options)
    local kind, key, facts = briefRequest()
    return kind and T.Insights.Request(kind, key, facts, options) or false
end

-- Which advice the brief has for a trainer row: "buy" or "skip" with its reason.
function Data.SpellAdvice(brief, name, rank)
    if not brief or type(name) ~= "string" then return end
    local number = type(rank) == "string" and tonumber(rank:match("%d+")) or nil
    for _, kind in ipairs({ "skip", "buy" }) do
        for _, spell in ipairs(brief[kind] or {}) do
            if type(spell.name) == "string" and spell.name:lower() == name:lower() and (spell.rank == 0 or not number or spell.rank == number) then
                return kind, spell.why
            end
        end
    end
end
