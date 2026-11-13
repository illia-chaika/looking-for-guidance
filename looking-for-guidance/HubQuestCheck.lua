local _, T = ...
-- "Is this quest bugged, or am I doing it wrong?" The built-in card shows the journal's own facts; this adds
-- quick checks the client can answer and a researched verdict from Blizzard's notes and known issues.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local call, clean, allowed = Hub.Call, Hub.Clean, Hub.Allowed
local function L(...) return T.L(...) end
local KIND = "quest"

local VERDICTS = {
    bug = { "bad", "Известная ошибка" },
    fixed = { "good", "Blizzard это исправила" },
    changed = { "warn", "Условия задания изменились" },
    intended = { nil, "Задание работает как задумано" },
    unknown = { nil, "Подтверждений ошибки не нашлось" },
}
local DOUBT = { likely = "вероятно", unverified = "не подтверждено" }

local function key(q, profile)
    return "q" .. q.id .. ":" .. (profile.class or "") .. ":" .. (profile.faction or "")
end

local function facts(m, q, profile)
    local objectives, held = {}, {}
    for _, objective in ipairs(call(C_QuestLog.GetQuestObjectives, q.id) or {}) do
        local text = clean(objective.text)
        if type(text) == "string" and text ~= "" then
            objectives[#objectives + 1] = { text = text, done = clean(objective.numFulfilled), need = clean(objective.numRequired) }
        end
    end
    for _, item in ipairs(m.bag and m.bag.keep or {}) do
        if item.quest == q.title then held[#held + 1] = item.name end
    end
    local evidence = Hub.QuestEvidence(q)
    local goal = evidence[#evidence]
    return {
        id = q.id, title = q.title, level = q.level, category = q.header, complete = q.complete or nil,
        objectives = Data.Some(objectives), goal = type(goal) == "string" and goal:sub(1, 600) or nil, items = Data.Some(held),
        zone = profile.zone, class = profile.class, race = profile.race, faction = profile.faction, plevel = profile.level,
        target = allowed("target") and call(UnitName, "target") or nil,
    }
end

local function ask(m, q, options)
    if not q or not allowed("quests") then return end
    local profile = Data.Profile()
    Insights.Request(KIND, key(q, profile), facts(m, q, profile), options)
end

Hub.Extend("stuck", {
    build = function(m, c)
        local q = c and c.quest
        if not q then return c end
        local rows = {}
        c.rows = rows
        local function check(state, title, text) rows[#rows + 1] = { kind = "check", state = state, title = title, text = text } end
        -- What the client itself can tell, before any research.
        if q.complete then
            rows[#rows + 1] = { kind = "verdict", tone = "good", title = L("Задание выполнено"), text = L("Осталось сдать его.") }
        end
        if not q.complete and m.bag and m.bag.free == 0 then
            check("warn", L("Сумки полны"), L("Предмет задания некуда положить. Освободи ячейку."))
        end
        if m.level and q.level and q.level > m.level + 2 then
            check("warn", L("Задание выше твоего уровня"), L("Уровень задания {1}, твой {2}.", q.level, m.level))
        end
        if q.dungeon then check(nil, L("Задание для подземелья"), L("Цель находится в подземелье и рассчитана на группу.")) end
        if allowed("character") and call(UnitIsDeadOrGhost, "player") then
            check("warn", L("Ты в облике призрака"), L("Сначала вернись к телу."))
        end
        local digest = Insights.Get("digest", "me")
        for _, item in ipairs(digest and digest.items or {}) do
            if item.quest_id == q.id then
                rows[#rows + 1] = { kind = "verdict", tone = "good", title = L("Blizzard упоминает это задание в исправлениях"), text = item.text }
            end
        end

        local profile = Data.Profile()
        local state, found = Insights.State(KIND, key(q, profile))
        local again = function() ask(m, q, { force = true }) end
        if state == "pending" then
            rows[#rows + 1] = { kind = "pending", text = L("Ищу это задание в исправлениях и известных ошибках…"), since = found.since }
        elseif state == "failed" then
            rows[#rows + 1] = { kind = "verdict", tone = "warn", title = L("Не удалось проверить задание."), text = found.note }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Проверить заново"), click = again } } }
        elseif state == "ready" then
            local data = found.data or {}
            local verdict = VERDICTS[data.verdict] or VERDICTS.unknown
            local title = L(verdict[2])
            if DOUBT[data.confidence] then title = title .. " · " .. L(DOUBT[data.confidence]) end
            rows[#rows + 1] = { kind = "verdict", tone = verdict[1], title = title, text = data.summary }
            c.summary = L(verdict[2])
            if type(data.cause) == "string" and data.cause ~= "" then rows[#rows + 1] = { kind = "text", text = data.cause } end
            for _, step in ipairs(data.steps or {}) do check(nil, step.title, step.text) end
            local sources = {}
            for _, source in ipairs(data.sources or {}) do
                local dated = type(source.date) == "string" and source.date ~= "" and (source.date .. " · ") or ""
                sources[#sources + 1] = { title = dated .. (source.title or ""), url = source.url }
            end
            if #sources > 0 and not data.forever_verified then
                rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Источники не про Forever напрямую: сверь на месте.") }
            end
            rows[#rows + 1] = { kind = "sources", list = sources, at = found.at }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Проверить заново"), click = again } } }
            for index = #c.evidence, 1, -1 do
                if c.evidence[index].text == L("Исправления игры не проверялись.") then table.remove(c.evidence, index) end
            end
        else
            rows[#rows + 1] = { kind = "text", style = "t-ui", ink = "detail",
                text = L("Советник сверит задание с исправлениями Blizzard, списком известных ошибок и сообщениями игроков Forever.") }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Сверить с исправлениями"), primary = true, click = function() ask(m, q) end } } }
        end
        return c
    end,
})
