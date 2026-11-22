local _, T = ...
-- "What changed": Blizzard's hotfix notes, the game's data changes, and Blizzard's blue posts and news about Forever
-- that concern this character, matched by game IDs in the helper without the adviser. The newest posts come with a
-- copyable link. The adviser writes a short explanation of the notes and data changes only when the player asks.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local allowed = Hub.Allowed
local function L(...) return T.L(...) end
local KIND, SUMMARY, KEY = "changes", "digest", "me"

local function facts()
    local profile = Data.Profile()
    profile.quests = Data.Some(Data.Quests(25))
    profile.map, profile.gear, profile.bags, profile.spells = Data.MapID(), Data.WornIDs(), Data.BagIDs(), Data.SpellIDs()
    return profile
end

local function ask(options)
    if not allowed("character") then return false end
    return Insights.Request(KIND, KEY, facts(), options)
end

-- The check after login; opening the section asks only when that did not happen.
local asked, entered

-- The explanation is asked again only when the matched list changed since the last one.
local explainedFor
local function explain()
    if not allowed("character") then return false end
    local _, found = Insights.State(KIND, KEY)
    local key = found and found.data and found.data.key or ""
    local force = explainedFor ~= nil and explainedFor ~= key
    explainedFor = key
    return Insights.Request(SUMMARY, KEY, facts(), { force = force })
end

local function day(date)
    local y, m, d = tostring(date):match("^(%d+)-(%d+)-(%d+)$")
    return d and (tonumber(d) .. "." .. m) or tostring(date or "")
end

local CLASSES = { Warrior = true, Paladin = true, Hunter = true, Rogue = true, Priest = true, Shaman = true, Mage = true, Warlock = true, Druid = true }
local REASONS = {
    quest = "Квест в журнале: {1}", zone = "Твоя зона: {1}", item = "Твой предмет: {1}", spell = "Твоё заклинание: {1}",
    profession = "Твоя профессия: {1}",
}

local function reason(item)
    if item.reason == "class" then
        if item.name and item.name ~= "" and not CLASSES[item.name] then return L("Заклинание твоего класса: {1}", item.name) end
        return L("Твой класс")
    elseif item.reason == "race" then
        return L("Твоя раса")
    end
    local name = item.name or ""
    if item.reason == "profession" then
        for _, p in ipairs(Data.Professions()) do
            if p.english == name then name = p.name end
        end
    end
    return REASONS[item.reason] and L(REASONS[item.reason], name) or nil
end

local FIELDS = {
    binding = "Привязка", quality = "Качество", item_level = "Уровень предмета", required_level = "Требуемый уровень",
    sell_price = "Цена у торговца", buy_price = "Цена у торговца", value = "Значение", coefficient = "Коэффициент",
    ap_coefficient = "Коэффициент", cast_time = "Время произнесения", duration = "Длительность", range = "Дальность",
    cooldown = "Восстановление", mana_cost = "Расход маны", level = "Уровень", stats = "Характеристики",
    armor = "Броня", weapon_damage = "Урон оружия", weapon_speed = "Скорость оружия",
}

local function value(field, v)
    if v == nil or v == "" then return "—" end
    if field == "binding" then
        local bind = { ["0"] = NONE, ["1"] = ITEM_BIND_ON_PICKUP, ["2"] = ITEM_BIND_ON_EQUIP, ["3"] = ITEM_BIND_ON_USE, ["4"] = ITEM_BIND_QUEST }
        return bind[v] or v
    elseif field == "quality" then
        return _G["ITEM_QUALITY" .. v .. "_DESC"] or v
    elseif (field == "sell_price" or field == "buy_price") and tonumber(v) and GetCoinTextureString then
        return GetCoinTextureString(tonumber(v))
    end
    return v
end

-- "Item level: 61 → 60", one line per field.
local function fields(item)
    local lines = {}
    for _, f in ipairs(item.fields or {}) do
        local label = FIELDS[f[1]] and L(FIELDS[f[1]]) or tostring(f[1])
        if f[1] == "stats" and (f[2] or "") == "" and (f[3] or "") == "" then
            lines[#lines + 1] = L("Характеристики изменены")
        else
            lines[#lines + 1] = label .. ": " .. value(f[1], f[2]) .. " → " .. value(f[1], f[3])
        end
    end
    return table.concat(lines, "\n")
end

local POSTS = { blue = { "Синий пост", "Синий пост: {1}" }, news = { "Новость Blizzard", "Новость Blizzard: {1}" } }

local function row(item)
    local why = reason(item)
    if item.kind == "data" then
        local changed = fields(item)
        return { kind = "check", title = item.name, text = why and changed ~= "" and why .. "\n" .. changed or why or changed, value = day(item.date) }
    elseif item.kind == "post" then
        -- The line that concerns the character, under the post it is from.
        local label = POSTS[item.source] or POSTS.blue
        local line = type(item.line) == "string" and item.line ~= "" and item.line or nil
        local from = line and L(label[2], item.text or "") or L(label[1])
        return { kind = "check", title = line or item.text, text = why and from .. "\n" .. why or from, value = day(item.date) }
    end
    return { kind = "check", title = item.text, text = why, value = day(item.date) }
end

-- Links to the posts: the ones that concern the character first, then the newest. The game cannot open a browser.
local function postLinks(data)
    local list, seen = {}, {}
    for _, group in ipairs({ data.mine or {}, data.class or {}, data.posts or {} }) do
        for _, item in ipairs(group) do
            if #list < 5 and item.kind == "post" and type(item.url) == "string" and not seen[item.url] then
                seen[item.url] = true
                list[#list + 1] = { title = day(item.date) .. " · " .. tostring(item.text or ""), url = item.url }
            end
        end
    end
    return list
end

local function summary(c)
    local state, found = Insights.State(SUMMARY, KEY)
    if state == "pending" then
        c.rows[#c.rows + 1] = { kind = "pending", text = L("Советник пишет короткое объяснение…"), since = found.since }
    elseif state == "failed" then
        c.rows[#c.rows + 1] = { kind = "verdict", tone = "warn", title = L("Не удалось получить объяснение."), text = found.note }
    elseif state == "ready" and found.data then
        local data = found.data
        if data.headline and data.headline ~= "" then c.title = data.headline end
        if #(data.items or {}) > 0 then c.rows[#c.rows + 1] = { kind = "section", text = L("Коротко") } end
        for _, item in ipairs(data.items or {}) do
            c.rows[#c.rows + 1] = { kind = "check", title = item.text, text = item.why, value = day(item.date) }
        end
    end
end

Hub.Extend("news", {
    tile = { label = "Что нового", subtitle = "Исправления игры для тебя", action = "Открыть изменения",
        icon = "Interface\\Icons\\INV_Misc_Note_01", order = 7 },
    open = function()
        if not asked then asked = ask() == true end
    end,
    build = function()
        local c = Hub.NewCard("news", L("Что нового"), L("Доступ выключен"))
        if not allowed("character") then return c end
        c.rows = {}
        local refresh = { label = L("Проверить сейчас"), click = function() ask({ force = true }) end }
        local state, found = Insights.State(KIND, KEY)
        if state == "pending" then
            c.reason = L("Сверяю исправления с твоим персонажем…")
            c.rows[1] = { kind = "pending", text = c.reason, since = found.since }
        elseif state == "failed" then
            c.reason = L("Не удалось проверить исправления.")
            c.rows[1] = { kind = "verdict", tone = "warn", title = c.reason, text = found.note }
            c.rows[2] = { kind = "actions", actions = { refresh } }
        elseif state == "ready" then
            local data = found.data or {}
            local mine, class = data.mine or {}, data.class or {}
            c.kicker = data.newest and L("Исправления от {1}", day(data.newest)) or nil
            c.reason = #mine > 0 and L("Изменений для тебя: {1}", #mine) or L("В свежих исправлениях для тебя ничего нет.")
            c.summary = c.reason
            summary(c)
            for _, group in ipairs({ { mine, "Касается тебя напрямую" }, { class, "Для твоего класса" } }) do
                if #group[1] > 0 then c.rows[#c.rows + 1] = { kind = "section", text = L(group[2]) } end
                for _, item in ipairs(group[1]) do c.rows[#c.rows + 1] = row(item) end
            end
            if (data.more or 0) > 0 then
                c.rows[#c.rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("И ещё {1}: спроси советника, что изменилось.", data.more) }
            end
            local links = postLinks(data)
            if #links > 0 then
                c.rows[#c.rows + 1] = { kind = "section", text = L("Синие посты и новости") }
                c.rows[#c.rows + 1] = { kind = "sources", list = links }
            end
            if data.world_sign_in then
                c.rows[#c.rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("База мира придёт после разового входа через Battle.net в меню Looking for Guidance, так требуют условия Blizzard. Пока совпадения только по классу, профессиям и заданиям.") }
            elseif data.no_world then
                c.rows[#c.rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("База мира ещё не скачана: совпадения только по классу, профессиям и заданиям.") }
            end
            local y, mo, d = tostring(data.newest or ""):match("^(%d+)-(%d+)-(%d+)$")
            if y and time() - time({ year = tonumber(y), month = tonumber(mo), day = tonumber(d), hour = 12 }) > 10 * 86400 then
                -- A list that stopped moving may have moved to a new address.
                c.rows[#c.rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Blizzard давно не дополняла этот список: возможно, он переехал.") }
            end
            if data.url then
                c.rows[#c.rows + 1] = { kind = "sources", list = { { title = "WoW: Forever Hotfixes", url = data.url } }, at = found.at }
            end
            local actions = { refresh }
            -- The explanation reads the hotfix notes and data changes, not the posts.
            local explainable = 0
            for _, group in ipairs({ mine, class }) do
                for _, item in ipairs(group) do
                    if item.kind ~= "post" then explainable = explainable + 1 end
                end
            end
            if explainable > 0 then
                table.insert(actions, 1, { label = L("Объяснить коротко"), primary = true, click = explain })
            end
            c.rows[#c.rows + 1] = { kind = "actions", actions = actions }
            if #mine > 0 and data.key then
                -- News that came while the player was away is announced once, also after a reload.
                c.persist, c.priority = true, 60
                c.signature = data.key
                c.signal, c.change = c.signature, L("Вышли исправления, которые тебя касаются")
            end
        else
            c.reason = L("Сверю исправления Blizzard и данные игры с твоим классом, профессиями, заданиями и вещами.")
            c.rows[1] = { kind = "actions", actions = { { label = L("Проверить сейчас"), primary = true, click = function() ask() end } } }
        end
        return c
    end,
    discuss = function()
        return L("Объясни, что из свежих исправлений важно для моего персонажа и что мне стоит изменить в игре.")
    end,
})

-- After every login, a little after the world loads: the check is free, so it does not wait for the research setting.
local function greet()
    if asked or not entered or GetTime() - entered < 8 then return end
    if not T.Store.received then return end
    asked = ask({ background = true }) == true
end
local frame = CreateFrame("Frame")
frame:RegisterEvent("PLAYER_ENTERING_WORLD")
frame:SetScript("OnEvent", function()
    entered = entered or GetTime()
    C_Timer.After(8, greet)
end)
-- A helper that connects later is asked as soon as it is ready.
T.On("StoreChanged", greet)

-- A short notice once per new list: a toast that opens the card, and the chat line and sound the settings allow.
local announced
T.On("JournalAdviceChanged", function()
    local state, found = Insights.State(KIND, KEY)
    local data = state == "ready" and found.data or {}
    local key = #(data.mine or {}) > 0 and data.key
    local card = key and key ~= announced and Hub.Cards(Hub.Model({})).news
    if not card or not Hub.IsUnread(card) then return end
    announced = key
    if not T.Window.IsShown() then
        local count = #data.mine
        T.Alerts.Notice("news", L("Что нового"), L("Изменений для тебя: {1}", count),
            L("Вышли исправления, которые тебя касаются") .. " (" .. count .. "). /ask",
            function() T.Window.OpenAdvice() end)
    end
end)
