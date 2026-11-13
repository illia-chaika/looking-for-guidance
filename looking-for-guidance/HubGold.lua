local _, T = ...
-- Gold plan: this trainer's real prices, the class brief's buy/skip advice and the road to the first mount.
local Hub, Data = T.HubOverview, T.HubData
local call, allowed, money = Hub.Call, Hub.Allowed, Hub.Money
local function L(...) return T.L(...) end

-- Coin at the moment each level was reached, to see how fast this character saves.
local function ledger()
    local guid = T.CharacterIdentity and T.CharacterIdentity()
    if not guid then return end
    T.settings.goldLog = T.settings.goldLog or {}
    T.settings.goldLog[guid] = T.settings.goldLog[guid] or {}
    return T.settings.goldLog[guid]
end

local function note(level)
    local log, coin = ledger(), allowed("character") and call(GetMoney)
    if log and type(level) == "number" and type(coin) == "number" and not log[level] then log[level] = coin end
end

local function perLevel(level)
    local log = ledger()
    if not log or not level or not log[level] then return end
    for back = 3, 1, -1 do
        if log[level - back] then return math.floor((log[level] - log[level - back]) / back) end
    end
end

local function serviceName(service)
    return service.name .. (service.rank and service.rank ~= "" and " " .. service.rank or "")
end

Hub.Extend("trainer", {
    tile = { label = "Золото и обучение", subtitle = "Цены тренера и путь к маунту" },
    open = function() Data.AskBrief({ auto = true }) end,
    build = function(m, c)
        if not c or not allowed("spells") then return c end
        local rows = {}
        c.rows = rows
        local level, wallet, visit = m.level, m.wallet, m.visit
        note(level)
        local brief, state, found = Data.Brief()
        local function check(state, title, text, value) rows[#rows + 1] = { kind = "check", state = state, title = title, text = text, value = value } end

        local due, skippable, ahead = 0, 0, 0
        local riding = brief and brief.riding and brief.riding.level and brief.riding.level > 0 and brief.riding or nil
        local goal = riding and level and level < riding.level and riding.level or nil
        for _, service in ipairs(visit and visit.services or {}) do
            local advice = Data.SpellAdvice(brief, service.name, service.rank)
            due = due + (service.cost or 0)
            if advice == "skip" then skippable = skippable + (service.cost or 0) end
        end
        for _, service in ipairs(visit and visit.upcoming or {}) do
            if goal and service.level and service.level <= goal and Data.SpellAdvice(brief, service.name, service.rank) ~= "skip" then
                ahead = ahead + (service.cost or 0)
            end
        end

        -- The headline must agree with the advice: never lead with a rank the brief says to skip.
        if brief and visit and #visit.services > 0 and wallet then
            local pick, picked
            for _, service in ipairs(visit.services) do
                local advice, why = Data.SpellAdvice(brief, service.name, service.rank)
                if service.cost and service.cost <= wallet and advice ~= "skip" and (not pick or (advice == "buy" and picked ~= "buy")) then
                    pick, picked = { service = service, why = why }, advice
                end
            end
            if pick then
                c.title = serviceName(pick.service) .. " · " .. money(pick.service.cost)
                c.reason = pick.why or L("После покупки: {1}", money(wallet - pick.service.cost))
            else
                c.title, c.reason = L("Сейчас можно ничего не покупать"), L("Доступные ранги гайд советует пропустить или на них не хватает монет.")
            end
            c.signature = (visit.name or "") .. ":" .. c.title
            if c.signal then c.signal = c.signature end
        end
        -- The rows below list every rank with its price and advice, so the plain list is not repeated.
        if visit then c.steps = {} end

        if goal and wallet then
            local rate = perLevel(level)
            local need = (riding.cost_copper or 0) + ahead + math.max(0, due - skippable)
            if riding.cost_copper and riding.cost_copper > 0 and rate then
                local expected = wallet + rate * (goal - level)
                if expected >= need then
                    rows[#rows + 1] = { kind = "verdict", tone = "good", title = L("К {1} уровню золота хватит", goal),
                        text = L("При нынешнем темпе останется около {1}.", money(expected - need)) }
                else
                    rows[#rows + 1] = { kind = "verdict", tone = "warn", title = L("К {1} уровню не хватит около {2}", goal, money(need - expected)),
                        text = L("Пропускай отмеченные ранги и продавай лишнее.") }
                    c.summary = L("До маунта не хватает золота")
                end
            end
            rows[#rows + 1] = { kind = "section", text = L("Путь к верховой езде") }
            check(nil, L("Верховая езда · уровень {1}", goal), riding.note,
                riding.cost_copper and riding.cost_copper > 0 and money(riding.cost_copper) or L("цена не подтверждена"))
            check(nil, L("В кошельке"), nil, money(wallet))
            if ahead > 0 then check(nil, L("Обучение до {1} уровня", goal), L("По ценам твоего тренера, без пропускаемых рангов."), money(ahead)) end
            if rate then check(nil, L("Копишь за уровень"), nil, (rate < 0 and "−" or "") .. money(math.abs(rate))) end
        end

        if visit and #visit.services > 0 then
            rows[#rows + 1] = { kind = "section", text = L(m.liveTrainer and "Сейчас у тренера" or "Последний визит к тренеру") }
            for index, service in ipairs(visit.services) do
                if index > 12 then break end
                local advice, why = Data.SpellAdvice(brief, service.name, service.rank)
                local short = service.cost and wallet and service.cost > wallet and L("Не хватает: {1}", money(service.cost - wallet)) or nil
                check(advice == "buy" and "done" or advice == "skip" and "skip" or nil, serviceName(service),
                    advice == "skip" and (L("Можно пропустить.") .. " " .. (why or "")) or why or short, service.cost and money(service.cost))
            end
            if skippable > 0 then check("done", L("Экономия на пропущенных рангах"), nil, money(skippable)) end
        end
        local soon = {}
        for _, service in ipairs(visit and visit.upcoming or {}) do
            if service.level and level and service.level > level then soon[#soon + 1] = service end
        end
        table.sort(soon, function(a, b) if a.level ~= b.level then return a.level < b.level end return a.name < b.name end)
        if #soon > 0 then
            rows[#rows + 1] = { kind = "section", text = L("Скоро откроется") }
            for index = 1, math.min(6, #soon) do
                local service = soon[index]
                local advice, why = Data.SpellAdvice(brief, service.name, service.rank)
                check(advice == "skip" and "skip" or nil, serviceName(service),
                    L("Уровень {1}", service.level) .. (advice == "skip" and (" · " .. L("Можно пропустить.")) or why and (" · " .. why) or ""),
                    service.cost and money(service.cost))
            end
        end

        if brief then
            if not brief.forever_verified then
                rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Источники не про Forever напрямую: сверь на месте.") }
            end
            rows[#rows + 1] = { kind = "sources", list = brief.sources, at = found.at }
        elseif state == "pending" then
            rows[#rows + 1] = { kind = "pending", text = L("Собираю советы по твоему классу…"), since = found.since }
        elseif state == "failed" then
            rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Советы по классу сейчас недоступны.") .. " " .. (found.note or "") }
        elseif Data.CanBrief() then
            rows[#rows + 1] = { kind = "text", style = "t-ui", ink = "detail",
                text = L("Советник соберёт для твоего класса и уровня приоритет характеристик, классовые задания и ранги, которые стоит купить или пропустить.") }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Собрать советы по классу"), primary = true, click = function() Data.AskBrief() end } } }
        end
        return c
    end,
})

local frame = CreateFrame("Frame")
frame:RegisterEvent("PLAYER_LEVEL_UP")
frame:SetScript("OnEvent", function(_, _, level)
    -- The event carries the new level before UnitLevel reports it.
    if T.settings then note(type(level) == "number" and level or Data.Level()) end
end)
