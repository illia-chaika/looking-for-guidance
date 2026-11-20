local _, T = ...

local Context = {}
T.Context = Context

local REACTION = { "hostile", "hostile", "unfriendly", "neutral", "friendly", "friendly", "friendly", "friendly" }

local function plain(value)
    if issecretvalue and issecretvalue(value) then
        return nil
    end
    return value
end

local function list(items, describe)
    local out = {}
    for _, item in ipairs(items) do
        table.insert(out, describe(item))
    end
    return table.concat(out, ", ")
end

local function money(copper)
    return string.format("%dg %ds %dc", math.floor(copper / 10000), math.floor(copper / 100) % 100, copper % 100)
end

local function optional(fn, ...)
    if not fn then return end
    local ok, a, b, c = pcall(fn, ...)
    if ok then return plain(a), plain(b), plain(c) end
end

local REGIONS = { "US", "KR", "EU", "TW", "CN" }

-- Where this character trades. WoW: Forever has one auction house per region, ruleset and faction; the client
-- names the ruleset only through two game rules, the way AceDB-3.0 reads them.
function Context.Market()
    local market = { region = REGIONS[optional(GetCurrentRegion) or 0], faction = optional(UnitFactionGroup, "player"),
        hardcore = optional(C_GameRules and C_GameRules.IsHardcoreActive) or nil }
    local rules, enum = C_GameRules and C_GameRules.IsGameRuleActive, Enum and Enum.GameRule
    if rules and enum and enum.PvPRuleset and enum.RPRuleset then
        local pvp, rp = optional(rules, enum.PvPRuleset), optional(rules, enum.RPRuleset)
        if type(pvp) == "boolean" and type(rp) == "boolean" then
            market.ruleset = rp and pvp and "RP-PvP" or rp and "RP" or pvp and "PvP" or "PvE"
        end
    end
    return market
end

local function vitals(unit)
    local fields = {}
    local health, maxHealth = optional(UnitHealth, unit), optional(UnitHealthMax, unit)
    if health and maxHealth then fields[#fields + 1] = string.format("HP %d/%d", health, maxHealth) end
    local _, token = optional(UnitPowerType, unit)
    local power, maxPower = optional(UnitPower, unit), optional(UnitPowerMax, unit)
    if token and power and maxPower then fields[#fields + 1] = string.format("%s %d/%d", token, power, maxPower) end
    if optional(UnitIsDeadOrGhost, unit) then fields[#fields + 1] = "dead/ghost" end
    return table.concat(fields, ", ")
end

local function character()
    local xp, max = plain(UnitXP("player")), plain(UnitXPMax("player"))
    local fields = { string.format("Character: level %d %s %s, %s", UnitLevel("player"), UnitRace("player"), UnitClass("player"), UnitFactionGroup("player")) }
    if xp and max then
        fields[#fields + 1] = string.format("XP %d / %d, rested %d", xp, max, optional(GetXPExhaustion) or 0)
    end
    fields[#fields + 1] = money(GetMoney())
    local hp = vitals("player")
    if hp ~= "" then fields[#fields + 1] = hp end
    local names = { "Strength", "Agility", "Stamina", "Intellect", "Spirit" }
    for i, label in ipairs(names) do
        local _, stat = optional(UnitStat, "player", i)
        if stat then fields[#fields + 1] = label .. " " .. stat end
    end
    local _, armor = optional(UnitArmor, "player")
    if armor then fields[#fields + 1] = "Armor " .. armor end
    if optional(UnitExists, "pet") then
        local name = optional(UnitName, "pet") or "present"
        local family = optional(UnitCreatureFamily, "pet")
        local level = optional(UnitLevel, "pet")
        local state = vitals("pet")
        fields[#fields + 1] = "Pet " .. name .. (family and ", " .. family or "") ..
            (level and " level " .. level or "") .. (state ~= "" and ", " .. state or "")
    else
        fields[#fields + 1] = "No active pet"
    end
    return { table.concat(fields, " · ") }
end

local function location()
    local map = optional(C_Map.GetBestMapForUnit, "player")
    local position = map and optional(C_Map.GetPlayerMapPosition, map, "player")
    local zone, sub = GetZoneText(), GetSubZoneText()
    local _, kind, _, difficulty = GetInstanceInfo()
    local line = "Location: " .. ((sub ~= "" and sub ~= zone) and (sub .. ", " .. zone) or zone)
    if position then
        local x, y = optional(position.GetXY, position)
        if x and y then line = line .. string.format(" %.1f, %.1f", x * 100, y * 100) end
    end
    if kind ~= "none" then
        line = line .. " · " .. kind .. (difficulty ~= "" and (", " .. difficulty) or "")
    end
    return { line .. (IsResting() and " · resting" or "") }
end

local function target()
    if not UnitExists("target") then
        return { "Target: none selected" }
    end
    local kind, _, _, _, _, npc = strsplit("-", plain(UnitGUID("target")) or "")
    local who = (kind == "Creature" or kind == "Vehicle") and (" (npc:" .. npc .. ")") or (kind == "Player" and " (player)" or "")
    local name, level, class = plain(UnitName("target")), plain(UnitLevel("target")), plain(UnitClassification("target"))
    if not name and who == "" then
        return nil
    end
    local detail = vitals("target")
    local family = optional(UnitCreatureFamily, "target") or optional(UnitCreatureType, "target")
    return {
        string.format("Target:%s%s%s%s, %s%s%s", name and " " .. name or "", who, level and " level " .. level or "",
            (class and class ~= "normal") and (" " .. class) or "", REACTION[plain(UnitReaction("target", "player")) or 4] or "neutral",
            family and ", " .. family or "", detail ~= "" and " · " .. detail or ""),
    }
end

local function questInfo(id)
    if not id then return end
    for i = 1, C_QuestLog.GetNumQuestLogEntries() do
        local info = C_QuestLog.GetInfo(i)
        if info and not info.isHeader and not info.isHidden and info.questID == id then
            return info, i
        end
    end
end
Context.Quest = questInfo

function Context.OpenQuest(id)
    local info, index = questInfo(id)
    if not info then return false end
    if OpenQuestLogToQuest then
        OpenQuestLogToQuest(id)
    elseif QuestLog_OpenToQuest then
        QuestLog_OpenToQuest(index, true)
    elseif QuestMapFrame_OpenToQuestDetails then
        QuestMapFrame_OpenToQuestDetails(id)
    else
        return false
    end
    return true
end

function Context.Waypoint(id)
    if not C_QuestLog.GetNextWaypoint or not C_Map.SetUserWaypoint or not UiMapPoint
        or not UiMapPoint.CreateFromCoordinates or not questInfo(id) then return end
    local map, x, y = C_QuestLog.GetNextWaypoint(id)
    if not issecretvalue(map) and not issecretvalue(x) and not issecretvalue(y)
        and type(map) == "number" and map > 0 and type(x) == "number" and x >= 0 and x <= 1
        and type(y) == "number" and y >= 0 and y <= 1 and (x ~= 0 or y ~= 0)
        and (not C_Map.CanSetUserWaypointOnMap or C_Map.CanSetUserWaypointOnMap(map)) then
        return map, x, y
    end
end

function Context.MarkQuest(id)
    local map, x, y = Context.Waypoint(id)
    if not map then return false end
    C_Map.SetUserWaypoint(UiMapPoint.CreateFromCoordinates(map, x, y))
    if OpenWorldMap then OpenWorldMap(map) end
    return true
end

local function inline(str)
    return str and str:gsub("[%c]+", " "):gsub("|[nN]", " ")
end

local function quests()
    local lines = {}
    local selected = C_QuestLog.GetSelectedQuest and C_QuestLog.GetSelectedQuest()
    if (not selected or selected == 0) and GetQuestLogSelection then
        local index = GetQuestLogSelection()
        local info = index and index > 0 and C_QuestLog.GetInfo(index)
        selected = info and info.questID
    end
    for index = 1, C_QuestLog.GetNumQuestLogEntries() do
        local info = C_QuestLog.GetInfo(index)
        if info and not info.isHeader and not info.isHidden then
            local objectives = list(C_QuestLog.GetQuestObjectives(info.questID) or {}, function(objective)
                return string.format("%d/%d %s", objective.numFulfilled, objective.numRequired, ((objective.text or ""):gsub("^%d+/%d+%s*", "")))
            end)
            local description, objective = optional(GetQuestLogQuestText, index)
            if objective and objective ~= "" then
                objectives = (objectives ~= "" and objectives .. "; " or "") .. inline(objective)
            end
            if selected == info.questID and description and description ~= "" then
                objectives = objectives .. " · Journal: " .. inline(description)
            end
            local tracked = C_QuestLog.GetQuestWatchType and C_QuestLog.GetQuestWatchType(info.questID) ~= nil
                or IsQuestWatched and IsQuestWatched(index)
            table.insert(lines, string.format("  %s (quest:%d) level %d%s%s%s%s", info.title, info.questID, info.level,
                selected == info.questID and " · selected" or "", tracked and " · tracked" or "",
                C_QuestLog.IsComplete(info.questID) and " · ready to turn in" or "", objectives ~= "" and (" · " .. objectives) or ""))
        end
    end
    table.insert(lines, 1, string.format("Quests (%d):", #lines))
    return lines
end

local slots = { "head", "neck", "shoulder", "shirt", "chest", "waist", "legs", "feet", "wrists",
    "hands", "finger 1", "finger 2", "trinket 1", "trinket 2", "back", "main hand", "off hand", "ranged", "tabard" }

local function itemRef(id, fullLink)
    local link = T.GameDetails.Link("item", id, plain(fullLink))
    local name = fullLink and fullLink:match("|h%[(.-)%]|h")
    return { id = id, kind = "item", link = link, name = name or optional(C_Item.GetItemNameByID, id) }
end

local function gear(cards)
    local equipped = {}
    for slot = 1, 19 do
        local id = optional(GetInventoryItemID, "player", slot)
        if id then
            local card = itemRef(id, optional(GetInventoryItemLink, "player", slot))
            card.slot, card.slotName = slot, slots[slot]
            local key = card.link or "item:" .. id
            card.stats, card.itemLevel = T.GameDetails.Stats(key), optional(C_Item.GetDetailedItemLevelInfo, key)
            equipped[#equipped + 1] = (card.name or "item:" .. id) .. " (" .. slots[slot] .. ")"
            cards[#cards + 1] = card
        end
    end
    return { "Equipped: " .. table.concat(equipped, ", ") }
end

local function bags(cards)
    local variants = {}
    for bag = 0, NUM_BAG_SLOTS do
        for slot = 1, C_Container.GetContainerNumSlots(bag) do
            local info = plain(C_Container.GetContainerItemInfo(bag, slot))
            local id = info and plain(info.itemID)
            if id then
                local card = itemRef(id, plain(info.hyperlink))
                local key = card.link or tostring(id)
                if not variants[key] then
                    card.count = 0
                    variants[key] = card
                    cards[#cards + 1] = card
                end
                variants[key].count = variants[key].count + (plain(info.stackCount) or 1)
            end
        end
    end
    return { "Bags: " .. list(cards, function(c)
        return (c.name or "item:" .. c.id) .. (c.count > 1 and (" ×" .. c.count) or "")
    end) }
end

local function talentAffordable(configID, nodeID, balances)
    local costs = optional(C_Traits.GetNodeCost, configID, nodeID)
    if not costs or #costs == 0 then return nil end
    local known = true
    for _, cost in ipairs(costs) do
        local id, amount = plain(cost.ID), plain(cost.amount)
        local balance = id and balances[id]
        if amount == nil or balance == nil then
            known = false
        elseif balance < amount then
            return false
        end
    end
    if known then return true end
end

local function talents(cards)
    local group = optional(C_SpecializationInfo.GetActiveSpecGroup)
    local configID = group and optional(C_SpecializationInfo.GetCombatConfigIDForSpecGroup, group)
    configID = configID or optional(C_ClassTalents and C_ClassTalents.GetActiveConfigID)
    local config = configID and optional(C_Traits.GetConfigInfo, configID)
    if not config or not config.treeIDs or #config.treeIDs == 0 then return { "Talents: talent tree unavailable" } end
    local summary = {}
    for _, treeID in ipairs(config.treeIDs) do
        local balances = {}
        for _, currency in ipairs(optional(C_Traits.GetTreeCurrencyInfo, configID, treeID, true) or {}) do
            local id, quantity, spent = plain(currency.traitCurrencyID), plain(currency.quantity), plain(currency.spent)
            if id and quantity then balances[id] = quantity end
            if id and quantity and spent then
                summary[#summary + 1] = string.format("tree %d currency %d: %d unspent, %d spent", treeID, id, quantity, spent)
            end
        end
        for _, nodeID in ipairs(optional(C_Traits.GetTreeNodes, treeID) or {}) do
            local node = optional(C_Traits.GetNodeInfo, configID, nodeID)
            local entries = node and (node.entryIDs or (node.activeEntry and { node.activeEntry.entryID }))
            local canAfford
            if node and plain(node.canPurchaseRank) and node.isVisible ~= false then
                canAfford = talentAffordable(configID, nodeID, balances)
            end
            for _, entryID in ipairs(entries or {}) do
                local entry = optional(C_Traits.GetEntryInfo, configID, entryID)
                local definition = entry and entry.definitionID and optional(C_Traits.GetDefinitionInfo, entry.definitionID)
                local id = definition and definition.spellID
                if id and id > 0 and node.isVisible ~= false then
                    local active = node.activeEntry and node.activeEntry.entryID == entryID
                    local rank = active and (node.activeRank or 0) or 0
                    local name = definition.overrideName
                    if not name or name == "" then name = optional(C_Spell.GetSpellName, id) end
                    local card = { kind = "spell", id = id, name = name, node = nodeID, entry = entryID,
                        rank = rank, maxRank = entry.maxRanks or node.maxRanks, available = entry.isAvailable, canPurchase = node.canPurchaseRank,
                        description = definition.overrideDescription ~= "" and definition.overrideDescription or nil }
                    -- An active rank can be granted by the game. Record what the
                    -- player paid when the client makes that distinction.
                    card.canAfford = canAfford
                    if active and rank > 0 then card.purchasedRank = plain(node.ranksPurchased) end
                    cards[#cards + 1] = card
                    if rank > 0 then summary[#summary + 1] = string.format("%s (Rank %d)", name or "spell " .. id, rank) end
                end
            end
        end
    end
    if #summary == 0 then summary[1] = #cards > 0 and "no allocated ranks reported" or "talent data unavailable" end
    return { "Talents: " .. table.concat(summary, ", ") }
end

local function skills(cards)
    local labels, named, lines, whole = {}, {}, {}, true
    for index = 1, optional(C_SkillInfo and C_SkillInfo.GetNumSkillLines) or 0 do
        local skill = optional(C_SkillInfo.GetSkillLineInfo, index)
        if skill and not skill.isHeader then
            lines[skill.skillID] = skill
            -- The Forever client lists a profession under two skill IDs with one name and one rank. One is enough,
            -- and the lower ID is the one the profession's own book goes by.
            local twin = named[skill.name]
            if twin and twin.rank == skill.rank and twin.maxRank == skill.maxRank then
                twin.id = math.min(twin.id, skill.skillID)
            else
                local card = { kind = "skill", id = skill.skillID, name = skill.name, rank = skill.rank,
                    maxRank = skill.maxRank, modifier = skill.modifier, tempPoints = skill.tempPoints,
                    description = skill.description ~= "" and skill.description or nil, minLevel = skill.minLevel }
                cards[#cards + 1], named[skill.name] = card, card
                labels[#labels + 1] = string.format("%s %d/%d", skill.name, skill.rank, skill.maxRank)
            end
        elseif skill and skill.isCollapsed then
            whole = false
            labels[#labels + 1] = skill.name .. " (collapsed in client)"
        end
    end
    -- With every header open the list is the whole truth about the character's professions.
    T.Recipes.Append(cards, whole and next(lines) and lines or nil)
    if C_TradeSkillUI and type(C_TradeSkillUI.GetRecipeInfo) == "function" then
        cards[#cards+1] = {kind="recipe_lookup",name="own_recipe_by_id"}
    end
    return { "Skills: " .. table.concat(labels, ", ") }
end

local function spells(cards)
    local labels = {}
    local function readSlot(slot, bank, category, isPet)
        local item = C_SpellBook.GetSpellBookItemInfo(slot, bank)
        if not item or item.isOffSpec then return end
        local future = item.itemType == Enum.SpellBookItemType.FutureSpell
        local id = item.spellID or (future and item.actionID)
        if not id or id <= 0 or (not future and item.itemType ~= Enum.SpellBookItemType.Spell) then return end
        local card = { kind = "spell", id = id, known = not future, name = item.name, rank = item.subName ~= "" and item.subName or nil,
            category = category, pet = isPet or nil, passive = item.isPassive or nil }
        if future then card.requiredLevel = optional(C_SpellBook.GetSpellBookItemLevelLearned, slot, bank) end
        if isPet then
            local allowed, enabled = optional(C_SpellBook.GetSpellBookItemAutoCast, slot, bank)
            if allowed then card.autoCast = enabled end
        end
        cards[#cards + 1] = card
        labels[#labels + 1] = (future and "not learned: " or "") .. (isPet and "pet: " or "") .. (item.name or "spell:" .. id) ..
            (card.rank and " (" .. card.rank .. ")" or "")
    end
    for line = 1, C_SpellBook.GetNumSpellBookSkillLines() do
        local info = C_SpellBook.GetSpellBookSkillLineInfo(line)
        if info and not info.shouldHide then
            for slot = info.itemIndexOffset + 1, info.itemIndexOffset + info.numSpellBookItems do
                readSlot(slot, Enum.SpellBookSpellBank.Player, info.name, false)
            end
        end
    end
    local bank = Enum.SpellBookSpellBank.Pet
    if bank then
        for slot = 1, optional(C_SpellBook.HasPetSpells) or 0 do readSlot(slot, bank, nil, true) end
    end
    return { "Spells known: " .. table.concat(labels, ", ") }
end

local function reputation(cards)
    local factions = {}
    for index = 1, C_Reputation.GetNumFactions() do
        local faction = C_Reputation.GetFactionDataByIndex(index)
        if faction and (not faction.isHeader or faction.isHeaderWithRep) then
            cards[#cards + 1] = { kind = "faction", id = faction.factionID, name = faction.name, standing = faction.currentStanding,
                reaction = faction.reaction, currentThreshold = faction.currentReactionThreshold, nextThreshold = faction.nextReactionThreshold,
                atWar = faction.atWarWith, watched = faction.isWatched }
            table.insert(factions, string.format("%s (faction:%d) %d", faction.name, faction.factionID, faction.currentStanding))
        end
    end
    return { "Reputation: " .. table.concat(factions, ", ") }
end

local function buffs(cards)
    local labels = {}
    for _, filter in ipairs({ "HELPFUL", "HARMFUL" }) do
        for index = 1, 40 do
            local aura = optional(C_UnitAuras.GetAuraDataByIndex, "player", index, filter)
            if not aura then break end
            local id, name = plain(aura.spellId), plain(aura.name)
            if name then labels[#labels + 1] = (filter == "HARMFUL" and "debuff: " or "") .. name end
            if id then
                local expiration = plain(aura.expirationTime)
                cards[#cards + 1] = { kind = "spell", id = id, name = name, effect = filter, stacks = plain(aura.applications),
                    durationSeconds = plain(aura.duration), remainingSeconds = expiration and expiration > 0 and math.max(0, math.floor(expiration - GetTime())) or nil,
                    dispel = plain(aura.dispelName) }
            end
        end
    end
    return { "Buffs: " .. table.concat(labels, ", ") }
end

Context.parts = {
    { key = "character", label = "Персонаж", lines = character },
    { key = "location", label = "Местность", lines = location },
    { key = "quests", label = "Журнал заданий", lines = quests },
    { key = "gear", label = "Экипировка", lines = gear },
    { key = "bags", label = "Сумки", lines = bags },
    { key = "talents", label = "Таланты", lines = talents },
    { key = "skills", label = "Навыки", lines = skills },
    { key = "spells", label = "Заклинания", lines = spells },
    { key = "target", label = "Цель", lines = target },
    { key = "reputation", label = "Репутация", lines = reputation },
    { key = "buffs", label = "Эффекты", lines = buffs },
}

-- Reuse complete, identical sections of a snapshot only after the helper
-- publishes the durable snapshot ID for this addon session and packet number.
local reusable = { gear = true, bags = true, spells = true, skills = true, talents = true, reputation = true, quests = true }
local cacheRun, base
local awaitingBase = {}

function Context.ConfirmBase(runID, ack)
    if cacheRun ~= runID then
        cacheRun, base, awaitingBase = runID, nil, {}
    end
    if not runID or type(ack) ~= "table" or type(ack.seq) ~= "number" or type(ack.id) ~= "string"
        or #ack.id ~= 32 or not ack.id:match("^%x+$") then
        base = nil
        return
    end
    if base and base.id == ack.id and base.seq == ack.seq then return end
    local blocks = awaitingBase[ack.seq]
    base = blocks and { seq = ack.seq, id = ack.id, blocks = blocks } or nil
    for seq in pairs(awaitingBase) do
        if seq < ack.seq then awaitingBase[seq] = nil end
    end
end

function Context.Sent(seq, runID, blocks)
    if seq and runID and runID == cacheRun and blocks then
        awaitingBase[seq] = blocks
        -- A delayed/unknown ACK merely costs one full send. Keep recent section
        -- text in memory and never write inventory data to SavedVariables.
        for n in pairs(awaitingBase) do
            if n < seq - 6 then awaitingBase[n] = nil end
        end
    end
end

local function addCard(lines, section, card)
    card.section = section
    lines[#lines + 1] = "Card: " .. C_EncodingUtil.SerializeJSON(card)
end

local function capture(refs, preview, settings, previous)
    settings = settings or T.settings
    local lines, blocks, same = {}, {}, {}
    if settings.extras then
        for _, part in ipairs(Context.parts) do
            if settings.context[part.key] then
                local cards = {}
                local ok, rows = pcall(part.lines, cards)
                if ok and rows then
                    if not preview then
                        for _, card in ipairs(cards) do addCard(rows, part.key, card) end
                    end
                    local block = table.concat(rows, "\n")
                    if reusable[part.key] and not preview then blocks[part.key] = block end
                    if previous and blocks[part.key] and previous.blocks[part.key] == block and #block >= 64 then
                        same[#same + 1] = part.key
                    else
                        lines[#lines + 1] = block
                    end
                end
            end
        end
    end
    if refs and #refs > 0 then
        lines[#lines + 1] = "References: " .. #refs
        for _, ref in ipairs(refs) do
            addCard(lines, "references", { kind = ref.kind, id = ref.id, name = ref.name, link = ref.link })
        end
    end
    if #same > 0 then
        lines[#lines + 1] = "Index base: " .. previous.id
        lines[#lines + 1] = "Reuse: " .. table.concat(same, ",")
    end
    if #lines == 0 then return end
    if not preview then lines[#lines + 1] = "Detail lookup: supported" end
    local version, build = GetBuildInfo()
    local market = Context.Market()
    table.insert(lines, 1, string.format("Game: WoW Forever %s.%s%s%s%s", version, build,
        market.region and " · " .. market.region or "", market.ruleset and " · " .. market.ruleset or "",
        market.hardcore and " · hardcore" or ""))
    return table.concat(lines, "\n"), blocks
end

function Context.Text(refs)
    if refs then T.GameDetails.Remember(refs) end
    local full = capture(refs, false)
    return full
end

function Context.ForQuestion(runID, refs)
    if refs then T.GameDetails.Remember(refs) end
    return capture(refs, false, nil, cacheRun == runID and base or nil)
end

function Context.Preview(settings)
    local text = capture(nil, true, settings)
    return text
end

function Context.Summary()
    if not T.settings.extras then
        return T.L("Только вопрос")
    end
    local enabled = T.settings.context
    local parts = {}
    if enabled.character then
        table.insert(parts, UnitClass("player") .. " " .. UnitLevel("player"))
    end
    if enabled.location then
        local sub = GetSubZoneText()
        table.insert(parts, sub ~= "" and sub or GetZoneText())
    end
    if enabled.quests then
        local _, total = C_QuestLog.GetNumQuestLogEntries()
        if total then
            table.insert(parts, T.L("Заданий: {1}", total))
        else
            table.insert(parts, T.L("Журнал"))
        end
    end
    if #parts == 0 then
        for _, part in ipairs(Context.parts) do
            if enabled[part.key] then
                return T.L(part.label)
            end
        end
        return T.L("Выбрать данные")
    end
    return table.concat(parts, " · ")
end
