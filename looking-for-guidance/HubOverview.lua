local _, T = ...
local Hub = {}
T.HubOverview = Hub
local function L(...) return T.L(...) end
local function clean(v)
    if issecretvalue and issecretvalue(v) then return nil end
    return v
end

local function call(fn, ...)
    if not fn then return end
    local ok, a, b, c, d, e, f = pcall(fn, ...)
    if ok then return clean(a), clean(b), clean(c), clean(d), clean(e), clean(f) end
end

local function allowed(key)
    return T.settings and T.settings.extras and T.settings.context and T.settings.context[key]
end

local function activeQuests()
    local quests = {}
    if not allowed("quests") or not C_QuestLog then return quests end
    local selected = call(C_QuestLog.GetSelectedQuest)
    local tracked = call(C_SuperTrack and C_SuperTrack.GetSuperTrackedQuestID)
    local section
    for i = 1, call(C_QuestLog.GetNumQuestLogEntries) or 0 do
        local q = call(C_QuestLog.GetInfo, i)
        if q and clean(q.isHeader) then
            section = clean(q.title)
        elseif q and not clean(q.isHidden) and clean(q.questID) and clean(q.title) then
            local id = q.questID
            local tag = call(C_QuestLog.GetQuestTagInfo, id)
            quests[#quests + 1] = {
                id = id, title = q.title, index = i, level = clean(q.level), header = section,
                complete = call(C_QuestLog.IsComplete, id) and true or false,
                selected = id == selected, tracked = id == tracked,
                watched = call(C_QuestLog.GetQuestWatchType, id) ~= nil or call(IsQuestWatched, i),
                dungeon = tag and clean(tag.tagID) == (Enum and Enum.QuestTag and Enum.QuestTag.Dungeon or 81)
            }
        end
    end
    return quests
end

local function watchScore(q)
    return (q.selected and 4 or 0) + (q.tracked and 2 or 0) + (q.watched and 1 or 0)
end

local function questEvidence(q)
    if q and q.evidence then return q.evidence end
    local lines = {}
    if q then
        for _, obj in ipairs(call(C_QuestLog.GetQuestObjectives, q.id) or {}) do
            local text = clean(obj.text)
            if type(text) == "string" and text ~= "" then lines[#lines + 1] = text end
        end
        -- The modern quest description API takes the ID directly. Some clients
        -- also expose a legacy getter that only reads the selected entry.
        local goal = call(C_QuestLog.GetQuestObjectiveText, q.id)
        if type(goal) == "string" and goal ~= "" then
            lines[#lines + 1] = goal
        else
            local _, objectives = call(GetQuestLogQuestText, q.index)
            if type(objectives) == "string" and objectives ~= "" and objectives ~= lines[#lines] then
                lines[#lines + 1] = objectives
            end
        end
        q.evidence = lines
    end
    return lines
end

local function waypoint(q)
    if not allowed("location") or not C_QuestLog.GetNextWaypoint then return end
    local mapID, x, y = call(C_QuestLog.GetNextWaypoint, q.id)
    if type(mapID) == "number" and mapID > 0 and type(x) == "number" and type(y) == "number"
        and x >= 0 and x <= 1 and y >= 0 and y <= 1 and (x > 0 or y > 0) then
        local map = call(C_Map and C_Map.GetMapInfo, mapID)
        return mapID, x, y, map and clean(map.name)
    end
end

-- Match client-localized map names only in the journal's short objective.
-- A narrative description can mention places the player has already left.
local placesByContinent = {}
local function placeNames(current)
    local info = call(C_Map and C_Map.GetMapInfo, current)
    local zoneType = Enum and Enum.UIMapType and Enum.UIMapType.Zone or 3
    for i = 1, 8 do
        if not info or not clean(info.mapID) then return {} end
        if clean(info.mapType) == (Enum and Enum.UIMapType and Enum.UIMapType.Continent or 2) then break end
        local parent = clean(info.parentMapID)
        if not parent or parent == 0 or parent == info.mapID then return {} end
        info = call(C_Map.GetMapInfo, parent)
    end
    if not info or not info.mapID then return {} end
    local id = info.mapID
    if not placesByContinent[id] then
        local names = {}
        local children = call(C_Map.GetMapChildrenInfo, id, zoneType, true)
        if not children or #children == 0 then return names end
        for _, child in ipairs(children) do
            local name = clean(child.name)
            if type(name) == "string" and #name >= 4 then names[name] = true end
        end
        placesByContinent[id] = names
    end
    return placesByContinent[id]
end

local function distance(x, y, ox, oy)
    if type(x) == "number" and type(y) == "number" and type(ox) == "number" and type(oy) == "number" then
        return (x-ox)^2 + (y-oy)^2
    end
end

local function itinerary(quests, level)
    local current = allowed("location") and call(C_Map and C_Map.GetBestMapForUnit, "player")
    local pos = current and call(C_Map.GetPlayerMapPosition, current, "player")
    local px, py = call(pos and pos.GetXY, pos)
    local names = current and placeNames(current) or {}
    local currentInfo = current and call(C_Map.GetMapInfo,current)
    local region = currentInfo and clean(currentInfo.name)
    local focus, focusedScore, ties, pick, best
    for _, q in ipairs(quests) do
        local watch = watchScore(q)
        if not focusedScore or watch > focusedScore then focus, focusedScore, ties = q, watch, 1
        elseif watch == focusedScore then ties = ties + 1 end
        q.mapID, q.x, q.y, q.mapName = waypoint(q)
        q.destination, q.coordinates, q.nearby = q.mapName, nil, nil
        if q.mapID then q.coordinates = string.format("%.1f, %.1f", q.x*100, q.y*100) end
        if current and not q.destination then
            local _, aim = call(GetQuestLogQuestText, q.index)
            aim = type(aim) == "string" and aim or table.concat(questEvidence(q), " ")
            for name in pairs(names) do
                if aim:find(name, 1, true) then
                    if q.destination then q.destination = nil; break end
                    q.destination = name
                end
            end
        end
        local d = q.mapID == current and distance(q.x, q.y, px, py)
        local localArea = current and ((q.mapID and q.mapID == current) or (region and q.destination==region))
        local proximity = d and math.max(0,15-18*math.sqrt(d)) or (localArea and 9 or 0)
        q.score = watch + (watch>0 and 3 or 0) + (q.complete and 3 or 0) + proximity
        if level and q.level and q.level > level + 2 and not q.complete then q.score = q.score - 6 end
    end
    for _, q in ipairs(quests) do
        local otherScore
        for _, other in ipairs(quests) do
            if q ~= other and q.destination and q.destination == other.destination then
                local d = q.mapID and q.mapID == other.mapID and distance(q.x,q.y,other.x,other.y)
                local relevant = d or focusedScore==0 or (watchScore(q)>0 and watchScore(other)>0)
                -- Two pins on opposite sides of a zone are not one stop.
                -- A shared textual destination is a common district/zone, not an exact pin.
                if relevant and (not d or d <= 0.10^2) and (not otherScore or other.score > otherScore) then
                    q.nearby, otherScore = other, other.score
                end
            end
        end
        local weight = q.score + (q.nearby and 8 or 0)
        if not pick or weight > best or (weight == best and q.id < pick.id) then pick, best = q, weight end
    end
    return pick, (ties == 1 and (focusedScore > 0 or #quests == 1)) and focus or nil
end

local function maxStack(link)
    local info = C_Item and C_Item.GetItemInfo or GetItemInfo
    if not info then return end
    local ok, count = pcall(function() return select(8, info(link)) end)
    if ok then return clean(count) end
end

local function bagInfo(quests)
    local r = {sellSlots = 0, sellNames = {}, sellCounts = {}, keep = {}}
    if not allowed("bags") or not C_Container then return r end
    local stacks, recorded, protected = {}, {}, {}
    local function activeQuest(name, id)
        for _, q in ipairs(quests or {}) do
            if id and id == q.id then return q end
            for _, line in ipairs(questEvidence(q)) do
                if name and line:find(name, 1, true) then return q end
            end
        end
    end
    for bag = 0, NUM_BAG_SLOTS or 4 do
        local free, kind = call(C_Container.GetContainerNumFreeSlots, bag)
        if type(free) == "number" and kind == 0 then r.free = (r.free or 0) + free end
        for slot = 1, call(C_Container.GetContainerNumSlots, bag) or 0 do
            local slotInfo = call(C_Container.GetContainerItemInfo, bag, slot)
            local link = slotInfo and clean(slotInfo.hyperlink)
            if type(link) == "string" and not clean(slotInfo.isLocked) then
                local count = clean(slotInfo.stackCount)
                if type(count) == "number" and count > 0 then
                    local p = stacks[link] or {}
                    stacks[link] = p
                    if not p.a or count < p.a then p.a, p.b = count, p.a
                    elseif not p.b or count < p.b then p.b = count end
                    if not r.merge and p.b and p.a + p.b <= (maxStack(link) or 0) then
                        r.merge = {link = link, name = link:match("|h%[(.-)%]|h"), a = p.a, b = p.b}
                    end
                end
                local name = link:match("|h%[(.-)%]|h")
                local quest = call(C_Container.GetContainerItemQuestInfo, bag, slot)
                local assigned = activeQuest(name, quest and clean(quest.questID))
                if assigned or (quest and (clean(quest.isQuestItem) or clean(quest.questID))) then
                    if name and not protected[name] then
                        r.keep[#r.keep+1] = {name=name, quest=assigned and assigned.title}
                        protected[name] = true
                    end
                elseif name and clean(slotInfo.quality) == 0 and not clean(slotInfo.hasNoValue) then
                    local info = C_Item and C_Item.GetItemInfo or GetItemInfo
                    local ok, price = pcall(function() return select(11, info(link)) end)
                    if (ok and type(clean(price)) == "number" and price > 0) or clean(slotInfo.hasNoValue) == false then
                        r.sellSlots = r.sellSlots + 1
                        r.sellCounts[name] = (r.sellCounts[name] or 0) + 1
                        r.junk = r.junk or name
                        if not recorded[name] then r.sellNames[#r.sellNames+1] = name; recorded[name] = true end
                    end
                end
            end
        end
    end
    return r
end

local function money(v)
    return string.format("%d%s %d%s %d%s", math.floor(v / 10000), GOLD_AMOUNT_SYMBOL or "g",
        math.floor(v / 100) % 100, SILVER_AMOUNT_SYMBOL or "s", v % 100, COPPER_AMOUNT_SYMBOL or "c")
end

local function characterID()
    return T.CharacterIdentity and T.CharacterIdentity() or call(UnitGUID, "player")
end

local activeTrainer
local function forgetTrainer() activeTrainer = nil end

local function trainerVisit()
    local guid = characterID()
    local bank = guid and T.settings.trainingVisits and T.settings.trainingVisits[guid]
    local locale = call(GetLocale)
    if type(bank) ~= "table" then return end
    -- The previous version stored just the last visible trainer.
    if bank.locale == locale and type(bank.services) == "table" then return bank end
    local records = bank[locale]
    if type(records) == "table" then
        return records[activeTrainer or records.classTrainer or records.last], activeTrainer ~= nil
    end
end

local function rememberTrainer()
    local guid = characterID()
    if not guid or not allowed("spells") or not GetTrainerServiceInfo then return end
    local count = call(GetNumTrainerServices)
    if type(count) ~= "number" then return end
    local locale = call(GetLocale)
    local name = call(UnitName,"npc")
    if not locale or not name then return end
    local trainerType = call(C_Trainer and C_Trainer.GetTrainerType)
    local key = tostring(trainerType or "")..":"..name
    local npcGuid = call(UnitGUID, "npc")
    if type(npcGuid) == "string" and npcGuid:match("^Creature%-") then
        local _, _, _, _, _, npcID = strsplit("-", npcGuid)
        if npcID then key = npcID end
    end
    local visit = { locale=locale, at=time(), name=name, services={}, upcoming={} }
    local bar = {}
    if GetActionInfo and C_Spell and C_Spell.GetSpellName then
        for slot = 1, 120 do
            local kind, id = call(GetActionInfo, slot)
            if kind == "spell" and id then
                local spell = call(C_Spell.GetSpellName, id)
                if spell then bar[spell] = true end
            end
        end
    end
    for i = 1, count do
        local name, availability, legacy, _, rank = call(GetTrainerServiceInfo, i)
        if type(name) == "string" and (availability == "available" or legacy == "available") then
            local sub = legacy == "available" and availability or rank
            visit.services[#visit.services+1] = {name=name,rank=type(sub) == "string" and sub or nil,
                used=bar[name] or nil,cost=call(GetTrainerServiceCost,i)}
        elseif type(name) == "string" and (availability == "unavailable" or legacy == "unavailable") and #visit.upcoming < 40 then
            -- Ranks for later levels carry this trainer's real prices: the base of a gold plan.
            local sub = legacy == "unavailable" and availability or rank
            local needed = call(GetTrainerServiceLevelReq, i)
            visit.upcoming[#visit.upcoming+1] = {name=name,rank=type(sub) == "string" and sub or nil,
                cost=call(GetTrainerServiceCost,i),level=type(needed) == "number" and needed > 0 and needed or nil}
        end
    end
    table.sort(visit.services, function(a,b)
        if a.used ~= b.used then return a.used == true end
        if a.cost ~= b.cost then return (a.cost or math.huge) < (b.cost or math.huge) end
        return a.name < b.name
    end)
    T.settings.trainingVisits = T.settings.trainingVisits or {}
    local bank = T.settings.trainingVisits[guid] or {}
    if bank.services and bank.locale then
        local oldkey = bank.name or "last"
        bank = {[bank.locale]={[oldkey]=bank,last=oldkey}}
    end
    T.settings.trainingVisits[guid] = bank
    local records = bank[locale] or {}
    bank[locale] = records
    records[key], records.last, activeTrainer = visit, key, key
    if trainerType == (Enum and Enum.TrainerType and Enum.TrainerType.General or 0) then records.classTrainer = key end
end

local function nextSpell()
    if not allowed("spells") or not C_SpellBook or not Enum or not Enum.SpellBookSpellBank or not Enum.SpellBookItemType then return end
    local bank = Enum.SpellBookSpellBank.Player
    local best, nextLevel = nil, math.huge
    for index = 1, call(C_SpellBook.GetNumSpellBookSkillLines) or 0 do
        local line = call(C_SpellBook.GetSpellBookSkillLineInfo, index)
        if line and not clean(line.shouldHide) and clean(line.itemIndexOffset) and clean(line.numSpellBookItems) then
            for slot = line.itemIndexOffset + 1, line.itemIndexOffset + line.numSpellBookItems do
                local s = call(C_SpellBook.GetSpellBookItemInfo, slot, bank)
                if s and clean(s.itemType) == Enum.SpellBookItemType.FutureSpell and not clean(s.isOffSpec) and clean(s.name) then
                    local needed = call(C_SpellBook.GetSpellBookItemLevelLearned, slot, bank)
                    if not best or (type(needed) == "number" and needed > 0 and needed < nextLevel) then
                        nextLevel = needed and needed > 0 and needed or math.huge
                        best = {name = s.name, level = nextLevel < math.huge and nextLevel or nil}
                    end
                end
            end
        end
    end
    return best
end

-- Only use specific dungeons advertised by this client. Queue restrictions,
-- missing levels, random queues, raids and other expansions are not candidates.
local function dungeonChoices(level)
    if not level then return {} end
    local ids = call(GetLFDChoiceOrder) or {}
    local choices, seen = {}, {}
    for _, id in ipairs(ids) do
        if type(id) == "number" and id > 0 then
            local ok, name, typeID, subtype, minimum, maximum, recommended, low, high, expansion, _, _, _, maxPlayers =
                pcall(GetLFGDungeonInfo, id)
            if ok and clean(name) and clean(typeID) == 1 and clean(subtype) == 1 and clean(expansion) == 0
                and type(clean(minimum)) == "number" and type(clean(maximum)) == "number" and minimum > 0
                and level >= minimum and level <= maximum and not seen[name]
                and (not clean(maxPlayers) or maxPlayers <= 5) then
                seen[name] = true
                local recLow, recHigh = clean(low), clean(high)
                local target = clean(recommended)
                local distance = recLow and recLow > level and recLow - level or recHigh and recHigh > 0 and recHigh < level and level - recHigh or 0
                    choices[#choices + 1] = {name = name, score = distance, difference = target and target > 0 and math.abs(level - target) or 0}
            end
        end
    end
    -- Forever's group board uses the dungeon activity category even on clients
    -- where the legacy dungeon finder exposes no choices.
    if C_LFGList then
        local groups = call(C_LFGList.GetAvailableActivityGroups, 2) or {}
        local function activities(group)
            for _, id in ipairs(call(C_LFGList.GetAvailableActivities, 2, group) or {}) do
                local info = call(C_LFGList.GetActivityInfoTable,id)
                if info and clean(info.categoryID)==2 and type(clean(info.fullName))=="string" then
                    local min, max = clean(info.minLevelSuggestion), clean(info.maxLevelSuggestion)
                    if min and min > 0 and max and max >= min and level>=min and level<=max and not seen[info.fullName] then
                        seen[info.fullName]=true
                        choices[#choices+1]={name=info.fullName,score=0,difference=math.abs(level-(min+max)/2)}
                    end
                end
            end
        end
        activities(nil)
        for _, group in ipairs(groups) do activities(group) end
    end
    table.sort(choices, function(a,b)
        if a.score ~= b.score then return a.score < b.score end
        if a.difference ~= b.difference then return a.difference < b.difference end
        return a.name < b.name
    end)
    return choices
end

local function model(cache)
    cache.level = allowed("character") and call(UnitLevel, "player")
    if not cache.quests then
        cache.quests = activeQuests()
        cache.route, cache.selected = itinerary(cache.quests, cache.level)
        cache.bag = nil -- item protection also consults active quest objectives
    end
    cache.bag = cache.bag or bagInfo(cache.quests)
    if not cache.haveBook then
        cache.futureSpell, cache.haveBook = nextSpell(), true
    end
    cache.dungeons = cache.dungeons or dungeonChoices(cache.level)
    cache.party = allowed("character") and call(GetNumGroupMembers)
    cache.wallet = allowed("character") and call(GetMoney)
    cache.visit, cache.liveTrainer = nil, nil
    if allowed("spells") then cache.visit, cache.liveTrainer = trainerVisit() end
    local name, kind = call(GetInstanceInfo)
    cache.instance = allowed("location") and kind == "party" and name
    cache.listing = nil
    if allowed("character") and C_LFGList then
        local listing = call(C_LFGList.GetActiveEntryInfo)
        local ids = listing and clean(listing.activityIDs)
        if ids and #ids==1 then
            local activity = call(C_LFGList.GetActivityInfoTable, clean(ids[1]))
            if activity and clean(activity.categoryID)==2 then cache.listing=clean(activity.fullName) end
        end
    end
    return cache
end

local topics = {
    {id="route", label="Маршрут", icon="Interface\\Icons\\INV_Misc_Map_01"},
    {id="trainer", label="Обучение", icon="Interface\\Icons\\INV_Misc_Book_09"},
    {id="bags", label="Сумки", icon="Interface\\Icons\\INV_Misc_Bag_10"},
    {id="dungeon", label="Подземелья", icon="Interface\\Icons\\Achievement_Dungeon_RagefireChasm"},
    {id="stuck", label="Проверка квеста", icon="Interface\\Icons\\INV_Misc_QuestionMark"},
}
local function status(q)
    return q.complete and L("Готово к сдаче") or q.tracked and L("Отслеживается") or L("Журнал заданий")
end

local function questLink(q)
    if not q then return end
    return call(GetQuestLink, q.id) or ("|Hquest:" .. q.id .. ":" .. (q.level or 0) .. "|h[" .. q.title .. "]|h")
end

local function currentPlan(topic)
    local memory = T.Store.memory
    if not memory or not T.CharacterIdentity then return end
    local guid, login = T.CharacterIdentity()
    if not guid or memory.character ~= guid or memory.login ~= login then return end
    local fallback
    for _, n in ipairs(memory.notes or {}) do
        if (not topic or n.topic == topic) and type(n.text) == "string" and (not n.expires or n.expires == 0 or n.expires > time()) then
            if n.scope == "day" or n.scope == "session" then return n end
            fallback = fallback or n
        end
    end
    return fallback
end

-- Cards defined in their own files register here: Hub.Extend(id, spec).
local extensions, extensionOrder = {}, {}

local function discussion(id, m, card)
    local texts, refs = {}, {}
    local ext = extensions[id]
    if ext and ext.discuss then
        texts[1], refs = ext.discuss(m, card)
        refs = refs or {}
    elseif id == "route" then
        texts[1] = L("Составь маршрут по моему журналу заданий.")
        if m.route then refs[#refs+1] = questLink(m.route) end
        if m.route and m.route.nearby then refs[#refs+1] = questLink(m.route.nearby) end
    elseif id == "trainer" then
        texts[1] = L("Что мне выучить следующим с учётом моего плана и денег?")
        if m.futureSpell then texts[#texts+1] = m.futureSpell.name end
        if m.visit then
            texts[#texts+1] = L("Последний визит к тренеру") .. " · " .. date("%x %H:%M", m.visit.at)
            for i = 1, math.min(5, #m.visit.services) do
                local s = m.visit.services[i]
                texts[#texts+1] = s.name .. (s.rank and " " .. s.rank or "") .. (s.cost and " · " .. money(s.cost) or "")
            end
        end
    elseif id == "bags" then
        texts[1] = L("Что можно объединить или продать в моих сумках с учётом квестов и моих планов?")
        if m.bag.merge then refs[#refs+1] = m.bag.merge.link end
    elseif id == "dungeon" then
        texts[1] = L("Помоги выбрать подземелье моего уровня и подготовить задания, снаряжение и маршрут до входа.")
        if card.dungeon then texts[#texts+1] = card.dungeon end
        for _, q in ipairs(card.matches or {}) do refs[#refs+1] = questLink(q) end
    elseif id == "stuck" then
        texts[1] = L("Проверь цель задания и мой прогресс. Если они расходятся, поищи известные проблемы WoW Forever.")
        if card.quest then refs[#refs+1] = questLink(card.quest) end
    end
    texts[#texts+1] = card.title
    texts[#texts+1] = card.reason
    for _, fact in ipairs(card.steps) do texts[#texts+1] = fact.title .. " · " .. fact.text end
    for _, row in ipairs(card.rows or {}) do
        local parts = {}
        for _, value in ipairs({row.title, row.value, row.text}) do
            if type(value) == "string" and value ~= "" then parts[#parts+1] = value end
        end
        if row.kind == "sources" then
            for _, source in ipairs(row.list or {}) do parts[#parts+1] = source.url end
        end
        if #parts > 0 and row.kind ~= "pending" then texts[#texts+1] = table.concat(parts, " · ") end
    end
    local plan = currentPlan(id)
    if plan then texts[#texts+1] = L("Мой план:") .. " " .. plan.text end
    return table.concat(texts, "\n"), refs
end

local function initCard(id, title, reason)
    local note = currentPlan(id)
    return {id=id, title=title, reason=reason, signature="", priority=0, steps={}, evidence={},
        plan=note and note.text, planScope=note and note.scope}
end

local function step(card, label, title, text)
    card.steps[#card.steps+1] = {label=label, title=title, text=text or ""}
end

local function proof(card, name, text)
    if text and text ~= "" then card.evidence[#card.evidence+1] = {name=name, text=text} end
end

local function questSteps(card,q)
    local aim = table.concat(questEvidence(q), "\n")
    step(card, status(q), q.title, aim)
    proof(card, L("Журнал заданий"), q.title .. (aim ~= "" and " · "..aim or ""))
end

local function routeCard(m)
    local q = m.route
    local c = initCard("route", L("Маршрут"), L("Выбери задание в игровом журнале."))
    if not q then return c end
    c.quest, c.nearby, c.kicker, c.priority = q, q.nearby, status(q), 25
    c.title = q.complete and L("Сдать „{1}“",q.title) or q.title
    c.reason = q.destination and L("Следующая остановка — {1}.",q.destination)
        or (q.complete and L("Готово к сдаче") or status(q))
    c.signature = tostring(q.id)..":"..tostring(q.complete)
    if q.nearby then
        c.title = L("Один поход: {1}", q.destination)
        c.reason = L("Два задания по пути: {1} и {2}.", q.title, q.nearby.title)
        c.change, c.priority = L("Открылся общий маршрут"), 65
        c.signature = math.min(q.id,q.nearby.id) .. ":" .. math.max(q.id,q.nearby.id) .. ":" .. q.destination
        c.signal = c.signature
    end
    questSteps(c,q)
    if q.nearby then questSteps(c,q.nearby) end
    if q.mapName and q.coordinates then proof(c,L("Местоположение"),q.mapName.." · "..q.coordinates) end
    if q.nearby then proof(c,L("Совпадает район назначения"),q.destination) end
    return c
end

local function bagCard(m)
    local b = m.bag
    local c = initCard("bags", L("Сумки"), L("Доступ выключен"))
    if not allowed("bags") then return c end
    c.kicker = b.free and L("Свободных ячеек: {1}", b.free) or L("Сумки")
    c.reason = c.kicker
    if b.free then proof(c,L("Сумки"),c.kicker) end
    if b.merge then
        local g = b.merge
        c.title = L("Освободи ячейку на месте")
        c.reason = L("Объедини две стопки: {1} ({2} + {3}).",g.name or g.link,g.a,g.b)
        step(c,L("На месте: +1"),g.name or g.link,c.reason)
    end
    local sellSlots, sellNames = 0, {}
    for _,name in ipairs(b.sellNames) do
        if not c.plan or not c.plan:lower():find(name:lower(),1,true) then
            sellSlots = sellSlots + b.sellCounts[name]
            sellNames[#sellNames+1] = name
        end
    end
    if sellSlots>0 then
        local names = table.concat(sellNames,", ",1,math.min(4,#sellNames))
        local sale = L("У торговца: +{1}",sellSlots)
        step(c,sale,names,L("Серый предмет для проверки у торговца: {1}",names))
        if not b.merge then c.title,c.reason=sale,names end
    end
    for i=1,math.min(#b.keep,4) do
        local item=b.keep[i]
        step(c,L("Оставить для задания"),item.name,item.quest or L("Журнал заданий"))
    end
    if not b.merge and sellSlots==0 then c.reason=L("Подходящих стопок и серых предметов не найдено.") end
    if b.free==0 or (b.free and b.free<=2 and b.merge) then
        c.priority = b.free==0 and 100 or 55
        if b.free==0 and not b.merge and sellSlots==0 then c.title=L("Сумки заполнены") end
        c.change = b.free==0 and L("Сумки заполнены") or c.kicker
        -- Plain loot churn does not re-notify a player who deferred full bags.
        c.signature=(b.free==0 and "full" or "low")..":"..(b.merge and "merge" or sellSlots>0 and "sell" or "review")
        c.signal=c.signature
    end
    return c
end

local function serviceName(service)
    return service.name .. (service.rank and service.rank ~= "" and " "..service.rank or "")
end

local function trainerCard(m)
    local c = initCard("trainer", L("Обучение"), L("Цены появятся после визита к тренеру."))
    if not allowed("spells") then c.reason=L("Доступ выключен");return c end
    if m.futureSpell then
        step(c,L("Книга заклинаний"),m.futureSpell.name,m.futureSpell.level and L("Уровень {1}",m.futureSpell.level))
    end
    if m.wallet then proof(c,L("Монеты"),money(m.wallet)) end
    local v = m.visit
    if not v then return c end
    local source = L(m.liveTrainer and "Сейчас у тренера" or "Последний визит к тренеру")
    c.kicker = source .. " · " .. date("%x %H:%M",v.at)
    proof(c,source,(v.name or "").." · "..date("%x %H:%M",v.at))
    local offer, score
    for _, s in ipairs(v.services) do
        if s.cost and m.wallet and s.cost<=m.wallet then
            local matchesGoal = c.plan and c.plan:lower():find(s.name:lower(),1,true)
            local weight = (s.used and 2 or 0) + (matchesGoal and 4 or 0)
            if not offer or weight>score then offer,score=s,weight end
        end
    end
    if offer then
        c.title=serviceName(offer) .. " · " .. money(offer.cost)
        c.reason=L("После покупки: {1}",money(m.wallet-offer.cost))
        step(c,offer.used and L("На панели действий") or source,serviceName(offer),c.reason)
    elseif #v.services==0 then c.reason=L("Нет доступных услуг в последнем списке.")
    else c.title=L("Сверь доступные ранги");c.reason=source end
    for _, s in ipairs(v.services) do
        if s~=offer and #c.steps<4 then
            local label = s.cost and m.wallet and s.cost>m.wallet and L("Не хватает: {1}", money(s.cost-m.wallet)) or source
            step(c,label,serviceName(s),s.cost and money(s.cost))
        end
    end
    if #v.services>0 then
        c.priority=m.liveTrainer and 85 or 0
        c.signature=(v.name or "")..":"..(offer and (serviceName(offer)..":"..offer.cost) or "none")
        if m.liveTrainer then c.signal=c.signature;c.change=L("Предложения тренера обновились") end
    end
    return c
end

-- The first Horde dungeon stays a candidate when the client's group board lists nothing for the level.
-- Its quests, entrance and tips come from the dungeon research (HubDungeon.lua), like for every other dungeon.
local function rfcName()
    local name = call(GetRealZoneText,389)
    return (name and name ~= "") and name or "Ragefire Chasm"
end

local function entranceOnMap(m,target)
    if not allowed("location") or not C_Map or not C_EncounterJournal or not C_EncounterJournal.GetDungeonEntrancesForMap then return end
    if not m.entrances then
        m.entrances = {}
        local id=call(C_Map.GetBestMapForUnit,"player")
        local seen={}
        while id and id>0 and not seen[id] do
            seen[id]=true
            local map=call(C_Map.GetMapInfo,id)
            if not map then break end
            for _, entry in ipairs(call(C_EncounterJournal.GetDungeonEntrancesForMap,id) or {}) do
                local name,pos=clean(entry.name),clean(entry.position)
                local x,y=call(pos and pos.GetXY,pos)
                if type(name)=="string" and type(x)=="number" and type(y)=="number"
                    and x>=0 and x<=1 and y>=0 and y<=1 and (x>0 or y>0) and clean(map.name) then
                    m.entrances[name]=m.entrances[name] or (map.name.." · "..string.format("%.1f, %.1f",x*100,y*100))
                end
            end
            id=clean(map.parentMapID)
        end
    end
    return m.entrances[target]
end
local function dungeonCard(m, choice)
    local c = initCard("dungeon", L("Подземелья"), L("Выбери подземелье"))
    c.choices, c.matches = {}, {}
    if not allowed("character") and not allowed("quests") and not allowed("location") then c.reason=L("Доступ выключен");return c end
    local names, seen = {}, {}
    local function add(name)
        if type(name)=="string" and name~="" and not seen[name] then
            names[#names+1],seen[name]=name,true
        end
    end
    local rfc = rfcName()
    local plan = c.plan and c.plan:lower() or ""
    local wantsRFC = plan:find(rfc:lower(),1,true) or plan:find("ragefire chasm",1,true) or plan:match("%f[%a]rfc%f[%A]")
    add(m.instance)
    add(m.listing)
    if wantsRFC then add(rfc) end
    for _, d in ipairs(m.dungeons or {}) do add(d.name) end
    if allowed("character") and call(UnitFactionGroup,"player")=="Horde" and m.level and m.level>=9 and m.level<=18 then add(rfc) end
    local target = choice and seen[choice] and choice or m.instance or m.listing or (wantsRFC and rfc)
    if not target and c.plan then
        for _, name in ipairs(names) do if plan:find(name:lower(),1,true) then target=name;break end end
    end
    if not target and #names==1 then target=names[1] end
    for _,name in ipairs(names) do if name~=target then c.choices[#c.choices+1]={id=name,label=name} end end
    if m.level then proof(c,L("Данные персонажа"),L("Уровень {1}",m.level)) end
    if not target then return c end
    c.dungeon,c.title=target,target
    local grouped=m.party and m.party>0
    c.reason=grouped and L("Согласуйте место встречи") or L("Задания и вход")
    if grouped then
        c.kicker=L("Игроков в группе: {1}",m.party)
        step(c,c.kicker,L("Согласуйте место встречи"),m.level and L("Уровень {1}",m.level))
        if c.plan or m.instance or m.listing then
            c.priority,c.signature,c.change=90,target,L("Группа собралась")
            c.signal=c.signature
        end
    end
    for _,q in ipairs(m.quests) do
        if q.header==target then
            c.matches[#c.matches+1]=q
            step(c,L("Задания для {1}",target),q.title,table.concat(questEvidence(q)," "))
        end
    end
    if allowed("quests") and #c.matches==0 then proof(c,L("Журнал заданий"),L("Подтверждённых заданий в журнале нет.")) end
    local entrance = entranceOnMap(m,target)
    if entrance then
        step(c,L("Вход"),target,entrance)
        proof(c,L("Местоположение"),entrance)
    end
    return c
end

local function stuckCard(m,id)
    local c=initCard("stuck",L("Проверка квеста"),L("Выбери задание для проверки"))
    if not allowed("quests") then c.reason=L("Доступ выключен");return c end
    c.choices={}
    local q=m.selected
    for _,entry in ipairs(m.quests) do if entry.id==id then q=entry end end
    for _,entry in ipairs(m.quests) do if entry~=q then c.choices[#c.choices+1]={id=entry.id,label=entry.title} end end
    if not q then return c end
    c.quest,c.title,c.kicker=q,q.title,status(q)
    local details=questEvidence(q)
    c.reason=details[1] or status(q)
    for i=2,#details do step(c,L("Журнал заданий"),q.title,details[i]) end
    for _,item in ipairs(m.bag.keep) do
        if item.quest==q.title then step(c,L("Сумки"),item.name,q.title) end
    end
    if allowed("target") then
        local target=call(UnitName,"target")
        if target then step(c,L("Выбранная цель"),target,L("Журнал заданий").." · "..c.reason) end
    end
    local place=allowed("location") and call(GetRealZoneText)
    if place then proof(c,L("Местность"),place) end
    proof(c,L("Журнал заданий"),table.concat(details," "))
    proof(c,L("Источники"),L("Исправления игры не проверялись."))
    return c
end

local function journalCards(m, questID, dungeon)
    local cards = {route=routeCard(m),bags=bagCard(m),trainer=trainerCard(m),dungeon=dungeonCard(m,dungeon),stuck=stuckCard(m,questID)}
    local choice = {quest=questID, dungeon=dungeon}
    m.ext = m.ext or {}
    for _, id in ipairs(extensionOrder) do
        local card = extensions[id].build(m, cards[id], choice)
        if card then
            card.id = id
            cards[id] = card
        end
    end
    return cards
end

local trackedGUID, previous, unread, snoozed, seen = nil, {}, {}, {}, {}
local lastReportedCount = 0
function Hub.UnreadCount()
    if not T.settings or T.settings.autoAdvice==false then return 0 end
    local count=0
    for _ in pairs(unread) do count=count+1 end
    return count
end

local function notify()
    local now=Hub.UnreadCount()
    if now~=lastReportedCount then
        lastReportedCount=now
        T.Fire("JournalAdviceChanged",now)
    end
end

function Hub.Observe(cards)
    local guid=characterID()
    if not guid then return end
    if guid~=trackedGUID then
        trackedGUID,previous,unread=guid,{},{}
        T.settings.deferredAdvice=T.settings.deferredAdvice or {}
        T.settings.deferredAdvice[guid]=T.settings.deferredAdvice[guid] or {}
        snoozed=T.settings.deferredAdvice[guid]
        T.settings.seenAdvice=T.settings.seenAdvice or {}
        T.settings.seenAdvice[guid]=T.settings.seenAdvice[guid] or {}
        seen=T.settings.seenAdvice[guid]
    end
    for id,card in pairs(cards) do
        if snoozed[id] and snoozed[id]~=card.signature then snoozed[id]=nil end
        local signal = card.signal or false
        if card.persist then
            -- News that arrived while the player was away is new on first sight, and stays read across reloads.
            local fresh = signal and seen[id]~=signal and T.settings.autoAdvice~=false and not snoozed[id]
            unread[id]=fresh and card.signature or nil
        elseif previous[id]~=signal then
            unread[id]=T.settings.autoAdvice~=false and previous[id]~=nil and signal and not snoozed[id] and card.signature or nil
        end
        previous[id]=signal
        if not card.signal or T.settings.autoAdvice==false then unread[id]=nil end
    end
    notify()
end

function Hub.IsUnread(c) return c and unread[c.id]==c.signature end
function Hub.Read(c)
    if c and c.persist and c.signal then seen[c.id]=c.signal end
    if c and unread[c.id] then
        unread[c.id]=nil
        notify()
    end
end
function Hub.Snooze(c)
    if c then snoozed[c.id]=c.signature;Hub.Read(c) end
end

local function bestCard(cards, onlyUnread)
    local best
    for _,t in ipairs(topics) do
        local c=cards[t.id]
        if c and c.priority>0 and snoozed[c.id]~=c.signature and (not onlyUnread or Hub.IsUnread(c))
            and (not best or c.priority>best.priority) then best=c end
    end
    return best
end
function Hub.TopUnread(cards) return bestCard(cards,true) end
function Hub.Lead(cards) return Hub.TopUnread(cards) or bestCard(cards,false) end
Hub.Model, Hub.Cards = model, journalCards
Hub.NewCard, Hub.Allowed, Hub.Call, Hub.Clean = initCard, allowed, call, clean
Hub.Step, Hub.Proof, Hub.Money, Hub.QuestLink, Hub.QuestEvidence = step, proof, money, questLink, questEvidence

local tiles = {
    {id="route", subtitle="Задания по пути", action="Посмотреть маршрут"},
    {id="bags", subtitle="Место и полезные вещи", action="Посмотреть сумки"},
    {id="trainer", subtitle="Ранги и стоимость", action="Посмотреть обучение"},
    {id="dungeon", subtitle="Вход и подготовка", action="Проверить подготовку"},
    {id="stuck", subtitle="Цель или ошибка?", action="Проверить задание"},
}
local tileByID = {}
for index, tile in ipairs(tiles) do
    for _, spec in ipairs(topics) do if spec.id == tile.id then tile.label, tile.icon = spec.label, spec.icon end end
    tile.order = index * 10
    tileByID[tile.id] = tile
end

-- spec.build(m, card, choice) returns the card for this topic; card is the built-in one, if any.
-- spec.tile = {label, subtitle, action, icon, order} adds a topic of its own.
-- spec.open(m, card) runs when the player opens the topic; spec.discuss(m, card) words the chat request.
function Hub.Extend(id, spec)
    if not extensions[id] then extensionOrder[#extensionOrder+1] = id end
    extensions[id] = spec
    if spec.tile and tileByID[id] then
        -- A built-in topic may be renamed by the card that replaces its content.
        local tile = tileByID[id]
        tile.label, tile.subtitle, tile.action = spec.tile.label or tile.label, spec.tile.subtitle or tile.subtitle, spec.tile.action or tile.action
    elseif spec.tile then
        local tile = {id=id, label=spec.tile.label, subtitle=spec.tile.subtitle, action=spec.tile.action,
            icon=spec.tile.icon, order=spec.tile.order or 100}
        topics[#topics+1] = {id=id, label=tile.label}
        tiles[#tiles+1], tileByID[id] = tile, tile
        table.sort(tiles, function(a, b) return a.order < b.order end)
    end
end

function Hub.Create(parent, handlers)
    local f = T.HubPage.Create(parent)
    local snapshot, cards, flags, baselineCharacter
    local dirty, queued = true, false
    local topic, chosenQuest, chosenDungeon
    local evidence, exact, picking, badgeTopic = false, false, false, nil

    local function selectTopic(id)
        topic, evidence, exact, picking, badgeTopic = id, false, false, false, nil
        chosenQuest, chosenDungeon = nil, nil
        if id and cards and cards[id] then
            Hub.Read(cards[id])
            local ext = extensions[id]
            if ext and ext.open then ext.open(snapshot, cards[id]) end
        end
        f:Refresh()
        f:Top()
    end
    local function drawRows(card)
        for _, row in ipairs(card.rows or {}) do
            if row.kind == "verdict" then f:Verdict(row.tone, row.title, row.text)
            elseif row.kind == "section" then f:Section(row.text)
            elseif row.kind == "check" then f:Check(row)
            elseif row.kind == "pending" then f:Pending(row.text, row.since)
            elseif row.kind == "sources" then f:Sources(row.list, row.at)
            elseif row.kind == "actions" then f:Actions(row.actions)
            else f:Text(row.text, row.style or "t-ui", row.ink or "prose") end
        end
    end
    local function discuss(card)
        local text, refs = discussion(card.id, snapshot, card)
        Hub.Read(card)
        handlers.discuss(text, refs, card.id, card.title)
    end
    local function discussControl(card, primary)
        return {label=L(primary and "Разобрать в текущем чате" or "В текущий чат"),
            primary=primary, click=function() discuss(card) end}
    end
    local function showEvidence(card)
        f:Actions({{label=L(evidence and "Скрыть источник" or "Источники"), click=function()
            evidence = not evidence
            f:Refresh()
        end}})
        if evidence then
            for _, value in ipairs(card.evidence or {}) do
                f:Text(value.name, "t-ui-strong", "strong")
                f:Text(value.text, "t-ui", "detail")
            end
        end
    end
    local function showChoices(card)
        if not card.choices or #card.choices == 0 then return end
        if card.quest or card.dungeon then
            local change = card.quest and "Сменить задание" or "Другое подземелье"
            f:Actions({{label=L(change),click=function() picking = not picking; f:Refresh() end}})
            if not picking then return end
        end
        local options = {}
        for _, option in ipairs(card.choices) do
            local choice = option.id
            options[#options+1] = {label=option.label, click=function()
                if topic == "stuck" then chosenQuest = choice else chosenDungeon = choice end
                exact, evidence, picking = false, false, false
                local ext = extensions[topic]
                if ext and ext.open then ext.open(snapshot, journalCards(snapshot, chosenQuest, chosenDungeon)[topic]) end
                f:Refresh()
                f:Top()
            end}
        end
        f:Actions(options)
    end
    local function drawDetail()
        local card = (chosenQuest or chosenDungeon) and journalCards(snapshot, chosenQuest, chosenDungeon)[topic] or cards[topic]
        local tile = tileByID[topic]
        f:Actions({{label=L("К обзору"), click=function() selectTopic(nil) end}})
        local kicker = L(tile.label)
        if card.kicker and card.kicker ~= kicker then kicker = kicker .. "  ·  " .. card.kicker end
        f:DetailTitle(tile.icon, kicker, card.title)
        f:Text(card.reason)
        local actions = {discussControl(card, true)}
        local q = card.quest
        if topic == "route" and q then
            actions[#actions+1] = {label=L(exact and "Только намёк" or "Показать подробности"),click=function()
                exact = not exact
                f:Refresh()
            end}
            if allowed("location") and T.Context.Waypoint(q.id) then
                actions[#actions+1] = {label=L("Метка на карте"), click=function()
                    if not T.Context.MarkQuest(q.id) then T.Fire("Problem","noWaypoint") end
                end}
            end
        end
        f:Actions(actions)
        showChoices(card)
        drawRows(card)
        if topic ~= "route" or exact or not q then
            for index, step in ipairs(card.steps or {}) do f:Step(index, step) end
            if q then f:Actions({{label=L("Журнал заданий"),click=function() T.Context.OpenQuest(q.id) end}}) end
        end
        if card.plan then
            f:Text(L("Мой план:") .. " " .. card.plan, "t-ui", "detail")
        end
        showEvidence(card)
    end
    local function drawOverview()
        local auto = T.settings.autoAdvice ~= false
        local mode = L(auto and "Автосоветы включены" or "Автосоветы выключены")
        f:Heading(L("Записки путешественника"), L("Сейчас полезно"),
            {label=mode,click=handlers.settings or function() T.settings.autoAdvice = not auto; f:Refresh() end})
        local requested = badgeTopic and cards[badgeTopic]
        local card = auto and ((requested and Hub.IsUnread(requested) and requested) or Hub.Lead(cards))
        if card then
            local tile = tileByID[card.id]
            local updated = card.change and Hub.IsUnread(card)
            f:Lead(card, tile.icon, {
                {label=L(tile.action),primary=true, click=function() selectTopic(card.id) end},
                discussControl(card, false),
                {label=L("Позже"),trailing=true,click=function()
                    Hub.Snooze(card)
                    f:Refresh()
                end},
            }, updated and (L("Что изменилось") .. " · " .. card.change))
        else
            f:Text(L("Выбери задачу ниже."), "t-ui", "detail")
        end
        f:Section(L("Все задачи"))
        local entries = {}
        for _, t in ipairs(tiles) do
            local id = t.id
            local card = cards[id]
            entries[#entries+1] = {icon=t.icon, label=L(t.label), summary=card and card.summary or L(t.subtitle),
                unread=Hub.IsUnread(card), click=function() selectTopic(id) end}
        end
        f:Grid(entries)
    end

    local function sample()
        local enabled = {}
        for _, key in ipairs({"character","location","quests","spells","bags","target","talents"}) do
            enabled[#enabled+1] = allowed(key) and "1" or "0"
        end
        local key = table.concat(enabled)
        local guid = characterID()
        if flags ~= key or not snapshot or baselineCharacter ~= guid then
            snapshot, dirty, flags, baselineCharacter = {}, true, key, guid
        end
        if dirty then
            snapshot, dirty = model(snapshot), false
        end
        -- Memory and interface language can change on an ordinary StoreChanged.
        local current = journalCards(snapshot)
        Hub.Observe(current)
        cards = current
    end
    function f:Refresh()
        if not self:IsVisible() then return end
        sample()
        self:Begin()
        if topic then drawDetail() else drawOverview() end
        self:Finish()
    end
    function f:OpenAdvice()
        sample()
        local preferred = Hub.TopUnread(cards) or Hub.Lead(cards)
        if preferred then
            topic, chosenQuest, chosenDungeon = nil, nil, nil
            evidence, exact, picking = false, false, false
            -- Opening the badge lands on the same overview the badge described.
            -- Keep its change explanation available until the player opens it.
            badgeTopic = preferred.id
        end
        self:Refresh()
        self:Top()
    end

    function f:Invalidate(event)
        dirty = true
        if snapshot then
            snapshot.ext = nil
            if event == "BAG_UPDATE_DELAYED" or event == "GET_ITEM_INFO_RECEIVED" then snapshot.bag = nil
            elseif event == "SPELLS_CHANGED" then snapshot.haveBook = nil
            elseif event == "LFG_UPDATE" then snapshot.dungeons = nil
            elseif event == "QUEST_LOG_UPDATE" or event == "SUPER_TRACKING_CHANGED" or event == "QUEST_WATCH_UPDATE"
                or event == "QUEST_WATCH_LIST_CHANGED" then snapshot.quests = nil
            elseif event == "PLAYER_ENTERING_WORLD" or event == "PLAYER_LEVEL_UP" or event == "ZONE_CHANGED_NEW_AREA"
                or event == "ZONE_CHANGED" or event == "ZONE_CHANGED_INDOORS" then snapshot = nil end
        end
        if queued then return end
        if not self:IsVisible() and T.settings.autoAdvice == false then return end
        queued = true
        C_Timer.After(.25, function()
            queued = false
            if self:IsVisible() then self:Refresh()
            elseif T.settings.autoAdvice ~= false then sample() end
        end)
    end
    for _, event in ipairs({"PLAYER_ENTERING_WORLD","QUEST_LOG_UPDATE","QUEST_WATCH_UPDATE","QUEST_WATCH_LIST_CHANGED",
        "SUPER_TRACKING_CHANGED","BAG_UPDATE_DELAYED","PLAYER_LEVEL_UP","PLAYER_MONEY","SPELLS_CHANGED","GROUP_ROSTER_UPDATE","PLAYER_TARGET_CHANGED",
        "ZONE_CHANGED","ZONE_CHANGED_INDOORS","ZONE_CHANGED_NEW_AREA","LFG_UPDATE","GET_ITEM_INFO_RECEIVED","TRAINER_SHOW",
        "TRAINER_UPDATE","TRAINER_CLOSED","PLAYER_INTERACTION_MANAGER_FRAME_SHOW","PLAYER_INTERACTION_MANAGER_FRAME_HIDE",
        "PLAYER_REGEN_ENABLED","PLAYER_EQUIPMENT_CHANGED","SKILL_LINES_CHANGED","TRAIT_CONFIG_UPDATED","QUEST_TURNED_IN",
        "MERCHANT_SHOW","MERCHANT_CLOSED"}) do
        pcall(f.RegisterEvent, f, event)
    end
    f:SetScript("OnEvent", function(self, event, interaction)
        -- Target changes matter only while the player is checking a quest.
        -- Entering the page reads the current selection directly.
        if event == "PLAYER_TARGET_CHANGED" and (topic ~= "stuck" or not self:IsVisible()) then return end
        local trainer = Enum and Enum.PlayerInteractionType and interaction == Enum.PlayerInteractionType.Trainer
        if event == "TRAINER_SHOW" or event == "TRAINER_UPDATE" or (event == "PLAYER_INTERACTION_MANAGER_FRAME_SHOW" and trainer) then
            rememberTrainer()
        elseif event == "TRAINER_CLOSED" or event == "PLAYER_ENTERING_WORLD" or (event == "PLAYER_INTERACTION_MANAGER_FRAME_HIDE" and trainer) then
            forgetTrainer()
        end
        self:Invalidate(event)
    end)
    f:SetScript("OnShow", function(self) dirty = true; self:Refresh() end)
    f:SetScript("OnSizeChanged", function(self) if self:IsVisible() then self:Refresh() end end)
    return f
end
