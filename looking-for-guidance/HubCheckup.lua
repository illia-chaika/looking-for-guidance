local _, T = ...
-- "Is my character all right?" — the question players ask the advisor most often.
local Hub, Data = T.HubOverview, T.HubData
local call, clean, allowed = Hub.Call, Hub.Clean, Hub.Allowed
local function L(...) return T.L(...) end

local WAND_CLASSES = { MAGE = true, PRIEST = true, WARLOCK = true }
local SHOOT, WELL_FED = 5019, 19705
local CAMP = { 20, 140, 300 }
local COOKING, CAMPFIRES = 185, { 90, 200 }

local function rangedSlot()
    local slot = call(GetInventorySlotInfo, "RangedSlot") or call(GetInventorySlotInfo, "RANGEDSLOT")
    return type(slot) == "number" and slot or nil
end

local function known(spell)
    if C_SpellBook and C_SpellBook.IsSpellKnown then return call(C_SpellBook.IsSpellKnown, spell) end
    return call(IsPlayerSpell, spell)
end

local function worn()
    local low
    for slot = 1, 19 do
        local now, max = call(GetInventoryItemDurability, slot)
        if type(now) == "number" and type(max) == "number" and max > 0 and now / max <= .2 then
            low = (low or 0) + 1
        end
    end
    return low
end

local function nextThreshold(skill)
    local marks = skill.id == COOKING and CAMPFIRES or CAMP
    for _, mark in ipairs(marks) do
        if skill.rank < mark then return mark end
    end
end

local function findings(m)
    local level, class = Data.Level(), Data.Class()
    local rows, issues = {}, 0
    local function add(state, title, text, value)
        rows[#rows + 1] = { kind = "check", state = state, title = title, text = text, value = value }
        if state == "warn" then issues = issues + 1 end
    end
    local points = Data.TalentPoints()
    if points and points > 0 then
        add("warn", L("Свободные очки талантов"), L("Открой окно талантов и вложи их. Советник подскажет выбор."), tostring(points))
    elseif points == 0 then
        add("done", L("Очки талантов вложены"))
    end
    local trainable = Data.TrainableSpells(level)
    if #trainable > 0 then
        local names = {}
        for i = 1, math.min(4, #trainable) do names[i] = trainable[i].name end
        add("warn", L("У тренера ждут новые заклинания"), table.concat(names, ", "), tostring(#trainable))
    end
    if allowed("gear") then
        local slot = rangedSlot()
        if slot and class and WAND_CLASSES[class] and known(SHOOT) and not call(GetInventoryItemID, "player", slot) then
            add("warn", L("Нет жезла"), L("Жезл добивает врагов без расхода маны. Его продают на аукционе и делают зачарователи."))
        end
        local broken = worn()
        if broken then add("warn", L("Снаряжение почти сломано"), L("Зайди к торговцу с ремонтом."), tostring(broken)) end
    end
    if level and level < 60 then
        local name = call(C_Spell and C_Spell.GetSpellName, WELL_FED)
        local fed = Data.HasAura(name)
        if fed == false then
            add("todo", L("Нет бонуса от еды"), L("В Forever сытость от приготовленной еды даёт +5% опыта за убийства. Эффект зависит от блюда."))
        elseif fed then
            add("done", L("Бонус от еды активен"))
        end
    end
    for _, skill in ipairs(Data.Professions()) do
        local value = skill.rank .. "/" .. skill.max
        local mark = nextThreshold(skill)
        if skill.rank >= skill.max and skill.max < 300 then
            add("warn", skill.name, L("Навык достиг предела. Следующий ранг открывает тренер профессии."), value)
        elseif mark then
            local what = skill.id == COOKING and L("До следующего костра для лагеря: {1}", mark - skill.rank)
                or L("До следующего предмета лагеря: {1}", mark - skill.rank)
            add(nil, skill.name, what, value)
        end
    end
    return rows, issues, points or 0, #trainable, level
end

Hub.Extend("checkup", {
    tile = { label = "Осмотр персонажа", subtitle = "Таланты, снаряжение, навыки", action = "Открыть осмотр",
        icon = "Interface\\Icons\\INV_Misc_Spyglass_03", order = 5 },
    build = function(m)
        local c = Hub.NewCard("checkup", L("Осмотр персонажа"), L("Доступ выключен"))
        if not allowed("character") then return c end
        local cache = m.ext.checkup
        if not cache then
            cache = {}
            cache.rows, cache.issues, cache.points, cache.trainable, cache.level = findings(m)
            m.ext.checkup = cache
        end
        c.rows = {}
        if cache.issues > 0 then
            c.title = L("Замечаний: {1}", cache.issues)
            for _, row in ipairs(cache.rows) do
                if row.state == "warn" then c.reason = row.title .. (row.value and " · " .. row.value or ""); break end
            end
            c.summary = c.title
            c.priority = 30
        else
            c.title, c.reason = L("Всё в порядке"), L("Срочных замечаний нет.")
            c.rows[1] = { kind = "verdict", tone = "good", title = c.title, text = c.reason }
        end
        c.kicker = cache.level and L("Уровень {1}", cache.level) or nil
        for _, row in ipairs(cache.rows) do c.rows[#c.rows + 1] = row end
        -- What a Forever levelling guide says about this class at these levels, prepared by the helper.
        local brief, state, found = Data.Brief()
        local rows = c.rows
        if brief then
            rows[#rows + 1] = { kind = "section", text = L("Советы для твоего класса") }
            rows[#rows + 1] = { kind = "text", text = brief.summary }
            if #(brief.stat_priority or {}) > 0 then
                rows[#rows + 1] = { kind = "check", title = L("Приоритет характеристик"), text = table.concat(brief.stat_priority, " › ") }
            end
            for _, quest in ipairs(brief.class_quests or {}) do
                local due = cache.level and quest.level <= cache.level
                local parts = {}
                for _, part in ipairs({ quest.where, quest.reward }) do
                    if type(part) == "string" and part ~= "" then parts[#parts + 1] = part end
                end
                rows[#rows + 1] = { kind = "check", state = due and "todo" or "skip", title = L("Классовое задание: {1}", quest.name),
                    text = table.concat(parts, " · "), value = L("Уровень {1}", quest.level) }
            end
            if type(brief.food) == "string" and brief.food ~= "" then
                rows[#rows + 1] = { kind = "check", title = L("Еда"), text = brief.food }
            end
            if not brief.forever_verified then
                rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Источники не про Forever напрямую: сверь на месте.") }
            end
            rows[#rows + 1] = { kind = "sources", list = brief.sources, at = found.at }
        elseif state == "pending" then
            rows[#rows + 1] = { kind = "pending", text = L("Собираю советы по твоему классу…"), since = found.since }
        elseif state == "failed" then
            rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Советы по классу сейчас недоступны.") .. " " .. (found.note or "") }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Проверить заново"), click = function() Data.AskBrief({ force = true }) end } } }
        end
        if cache.points > 0 or cache.trainable > 0 then
            -- A new level brings points and spells: worth one quiet badge.
            c.priority = 70
            c.signature = table.concat({ cache.level or 0, cache.points, cache.trainable }, ":")
            c.signal, c.change = c.signature, L("Появились новые возможности")
        end
        return c
    end,
    open = function() Data.AskBrief() end,
    discuss = function(_, card)
        return L("Осмотри моего персонажа: что стоит исправить в первую очередь и почему?")
    end,
})

-- The brief changes rarely, so it is prepared once after login for the cards that use it.
local greeted
local frame = CreateFrame("Frame")
frame:RegisterEvent("PLAYER_ENTERING_WORLD")
frame:SetScript("OnEvent", function()
    C_Timer.After(14, function()
        if greeted or not T.settings or T.settings.autoAdvice == false or T.Store.codex ~= "ready" then return end
        greeted = Data.AskBrief({ background = true }) == true
    end)
end)
