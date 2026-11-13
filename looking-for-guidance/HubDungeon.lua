local _, T = ...
-- Dungeon preparation for any dungeon: the quests to pick up before going in, checked against the quest log.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local allowed = Hub.Allowed
local function L(...) return T.L(...) end
local KIND = "dungeon"

local function key(name, profile)
    return (name:gsub("[%c/\\|\"<>%[%]{}*?#%%&=+;@!$^~`]", "") .. ":" .. (profile.faction or "")):sub(1, 110)
end

local function ask(m, c, options)
    if not c or not c.dungeon then return end
    local profile = Data.Profile()
    local held = {}
    for _, q in ipairs(c.matches or {}) do held[#held + 1] = q.title end
    Insights.Request(KIND, key(c.dungeon, profile), { name = c.dungeon, faction = profile.faction, class = profile.class,
        plevel = profile.level, in_log = Data.Some(held) }, options)
end

Hub.Extend("dungeon", {
    open = function(m, c) ask(m, c) end,
    build = function(m, c)
        if not c or not c.dungeon then return c end
        local rows = {}
        c.rows = rows
        local function check(state, title, text, value) rows[#rows + 1] = { kind = "check", state = state, title = title, text = text, value = value } end
        local profile = Data.Profile()
        local state, found = Insights.State(KIND, key(c.dungeon, profile))
        local again = { kind = "actions", actions = { { label = L("Проверить заново"), click = function() ask(m, c, { force = true }) end } } }
        if state == "pending" then
            rows[1] = { kind = "pending", text = L("Собираю задания и вход для этого подземелья…"), since = found.since }
        elseif state == "failed" then
            rows[1] = { kind = "verdict", tone = "warn", title = L("Не удалось собрать сведения о подземелье."), text = found.note }
            rows[2] = again
        elseif state == "ready" then
            local data, level = found.data or {}, m.level
            if level and (data.level_min or 0) > 0 then
                local range = data.level_min .. "–" .. data.level_max
                if level < data.level_min then
                    rows[#rows + 1] = { kind = "verdict", tone = "warn", title = L("Подземелье рассчитано на уровни {1}", range),
                        text = L("Твой уровень {1}: предупреди группу или вернись позже.", level) }
                elseif level > data.level_max then
                    check(nil, L("Ты выше уровня подземелья"), L("Рассчитано на уровни {1}: опыта будет мало.", range))
                else
                    check("done", L("Уровень подходит"), nil, range)
                end
            end
            local have = {}
            for _, q in ipairs(m.quests or {}) do have[q.title:lower()] = true end
            local missing, before, inside = 0, {}, {}
            for _, quest in ipairs(data.quests or {}) do
                if quest.inside then inside[#inside + 1] = quest else before[#before + 1] = quest end
            end
            local function place(quest)
                local parts = {}
                for _, part in ipairs({ quest.giver, quest.zone, quest.note }) do
                    if type(part) == "string" and part ~= "" then parts[#parts + 1] = part end
                end
                return table.concat(parts, " · ")
            end
            if #before > 0 then rows[#rows + 1] = { kind = "section", text = L("Взять до входа") } end
            for _, quest in ipairs(before) do
                if allowed("quests") and have[quest.name:lower()] then
                    check("done", quest.name, L("Уже в журнале"))
                elseif level and (quest.min_level or 0) > level then
                    check("skip", quest.name, L("С уровня {1}", quest.min_level) .. " · " .. place(quest))
                else
                    missing = missing + 1
                    check("todo", quest.name, place(quest))
                end
            end
            if #inside > 0 then rows[#rows + 1] = { kind = "section", text = L("Берутся внутри") } end
            for _, quest in ipairs(inside) do check(nil, quest.name, place(quest)) end
            local entrance = false
            for _, step in ipairs(c.steps or {}) do entrance = entrance or step.label == L("Вход") end
            if not entrance and type(data.entrance) == "string" and data.entrance ~= "" then check(nil, L("Вход"), data.entrance) end
            if #(data.tips or {}) > 0 then rows[#rows + 1] = { kind = "section", text = L("Перед походом") } end
            for _, tip in ipairs(data.tips or {}) do check(nil, tip) end
            if not data.forever_verified then
                rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Источники не про Forever напрямую: сверь на месте.") }
            end
            rows[#rows + 1] = { kind = "sources", list = data.sources, at = found.at }
            rows[#rows + 1] = again
            if #before > 0 then
                c.summary = missing > 0 and L("Заданий взять до входа: {1}", missing) or L("Задания собраны")
                c.reason = c.summary
                if c.signal and missing > 0 then c.change = c.summary end
            end
        else
            rows[1] = { kind = "text", style = "t-ui", ink = "detail",
                text = L("Советник соберёт задания, которые стоит взять до входа, место входа и советы для первого похода.") }
            rows[2] = { kind = "actions", actions = { { label = L("Подготовить поход"), primary = true, click = function() ask(m, c) end } } }
        end
        return c
    end,
})
