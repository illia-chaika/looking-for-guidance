local _, T = ...
-- "Where do I level now?" Zones and dungeons for the character's levels, with quieter alternatives. The game
-- does not tell an add-on how crowded a zone is: the advice comes from current Forever guides and reports.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local call, allowed = Hub.Call, Hub.Allowed
local function L(...) return T.L(...) end
local KIND = "leveling"
local CROWD = { quiet = "по отзывам, спокойно", busy = "по отзывам, людно" }

local function request()
    local profile = Data.Profile()
    if not profile.level or not profile.faction or profile.level >= 60 then return end
    local from = math.max(1, math.floor(profile.level / 5) * 5)
    return (profile.faction .. ":" .. from .. ":" .. (profile.class or "")), {
        faction = profile.faction, class = profile.class, race = profile.race, level = profile.level,
        from = from, to = math.min(60, from + 4), zone = profile.zone }, from
end

local function ask(options)
    local key, facts = request()
    if key then Insights.Request(KIND, key, facts, options) end
end

Hub.Extend("leveling", {
    tile = { label = "Куда качаться", subtitle = "Зоны и подземелья под твой уровень", action = "Открыть зоны",
        icon = "Interface\\Icons\\INV_Misc_Map_01", order = 15 },
    open = function() ask() end,
    build = function(m)
        local c = Hub.NewCard("leveling", L("Куда качаться"), L("Доступ выключен"))
        if not allowed("character") then return c end
        local key, facts, from = request()
        if not key then
            c.reason = L("На максимальном уровне маршрут прокачки не нужен.")
            return c
        end
        local rows = {}
        c.rows = rows
        c.kicker = L("Уровни {1}", facts.from .. "–" .. facts.to)
        local function check(state, title, text, value) rows[#rows + 1] = { kind = "check", state = state, title = title, text = text, value = value } end
        -- What the client knows without research.
        local inRange = 0
        for _, q in ipairs(m.quests or {}) do
            if q.level and not q.complete and q.level >= facts.level - 3 and q.level <= facts.level + 2 then inRange = inRange + 1 end
        end
        if allowed("quests") then
            if inRange == 0 then
                check("warn", L("В журнале нет заданий твоего уровня"), L("Самое время выбрать новую зону."))
            else
                check(nil, L("Заданий твоего уровня в журнале"), nil, tostring(inRange))
            end
        end
        local rested = allowed("character") and call(GetXPExhaustion)
        if type(rested) == "number" and rested > 0 then
            check("done", L("Есть отдохнувший опыт"), L("Убийства сейчас дают больше опыта: хорошее время для заданий на истребление."))
        end

        local state, found = Insights.State(KIND, key)
        local again = { kind = "actions", actions = { { label = L("Проверить заново"), click = function() ask({ force = true }) end } } }
        if state == "pending" then
            c.reason = L("Советник подбирает зоны под твой уровень.")
            rows[#rows + 1] = { kind = "pending", text = L("Ищу зоны и подземелья для твоих уровней…"), since = found.since }
        elseif state == "failed" then
            c.reason = L("Не удалось подобрать зоны.")
            rows[#rows + 1] = { kind = "verdict", tone = "warn", title = c.reason, text = found.note }
            rows[#rows + 1] = again
        elseif state == "ready" then
            local data = found.data or {}
            local here = allowed("location") and (call(GetRealZoneText) or call(GetZoneText))
            c.reason = data.summary or ""
            c.title = data.zones and data.zones[1] and data.zones[1].name or c.title
            c.summary = c.title
            if #(data.zones or {}) > 0 then rows[#rows + 1] = { kind = "section", text = L("Зоны") } end
            for _, zone in ipairs(data.zones or {}) do
                local note = zone.why or ""
                if CROWD[zone.crowd] then note = note .. " · " .. L(CROWD[zone.crowd]) end
                local current = here and zone.name:lower() == tostring(here):lower()
                check(current and "done" or nil, zone.name .. (current and (" · " .. L("ты здесь")) or ""), note, zone.levels)
            end
            if #(data.dungeons or {}) > 0 then rows[#rows + 1] = { kind = "section", text = L("Подземелья") } end
            for _, dungeon in ipairs(data.dungeons or {}) do check(nil, dungeon.name, dungeon.why, dungeon.levels) end
            if #(data.tips or {}) > 0 then rows[#rows + 1] = { kind = "section", text = L("Полезно знать") } end
            for _, tip in ipairs(data.tips or {}) do check(nil, tip) end
            rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail",
                text = L(data.forever_verified and "Людность зон игра аддонам не сообщает: это данные гайдов и отзывов."
                    or "Источники не про Forever напрямую: сверь на месте.") }
            rows[#rows + 1] = { kind = "sources", list = data.sources, at = found.at }
            rows[#rows + 1] = again
        else
            c.reason = L("Советник подберёт зоны и подземелья под твой уровень, включая места спокойнее.")
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Подобрать зоны"), primary = true, click = function() ask() end } } }
        end
        -- Reaching a new five-level range is the moment the question comes up: one quiet badge then, none at login.
        c.priority = inRange == 0 and 45 or 20
        c.signature = "range:" .. from
        c.signal, c.change = c.signature, L("Новый диапазон уровней: посмотри, куда идти дальше")
        return c
    end,
    discuss = function()
        return L("Посоветуй, где мне сейчас качаться: зона, подземелье и запасной вариант, если там толпа.")
    end,
})
