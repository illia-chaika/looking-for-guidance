local _, T = ...
-- "What changed for me": Blizzard's hotfix notes, filtered to this character by the helper.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local allowed = Hub.Allowed
local function L(...) return T.L(...) end
local KIND, KEY = "digest", "me"

local function facts()
    local profile = Data.Profile()
    profile.quests = Data.Some(Data.Quests(25))
    return profile
end

local function ask(options)
    if not allowed("character") then return false end
    return Insights.Request(KIND, KEY, facts(), options)
end

local function day(date)
    local y, m, d = tostring(date):match("^(%d+)-(%d+)-(%d+)$")
    return d and (tonumber(d) .. "." .. m) or tostring(date or "")
end

Hub.Extend("news", {
    tile = { label = "Что нового", subtitle = "Исправления игры для тебя", action = "Открыть изменения",
        icon = "Interface\\Icons\\INV_Misc_Note_01", order = 7 },
    open = function() ask({ auto = true }) end,
    build = function()
        local c = Hub.NewCard("news", L("Что нового"), L("Доступ выключен"))
        if not allowed("character") then return c end
        c.rows = {}
        local refresh = { kind = "actions", actions = { { label = L("Проверить сейчас"), click = function() ask({ force = true }) end } } }
        local state, found = Insights.State(KIND, KEY)
        if state == "pending" then
            c.reason = L("Советник читает свежие исправления Blizzard.")
            c.rows[1] = { kind = "pending", text = L("Сверяю исправления с твоим персонажем…"), since = found.since }
        elseif state == "failed" then
            c.reason = L("Не удалось проверить исправления.")
            c.rows[1] = { kind = "verdict", tone = "warn", title = c.reason, text = found.note }
            c.rows[2] = refresh
        elseif state == "ready" then
            local data = found.data or {}
            local items = data.items or {}
            c.title = data.headline or c.title
            c.kicker = data.newest and L("Исправления от {1}", day(data.newest)) or nil
            c.reason = #items > 0 and L("Изменений для тебя: {1}", #items) or L("В свежих исправлениях для тебя ничего нет.")
            c.summary = c.reason
            for _, group in ipairs({ { "direct", "Касается тебя напрямую" }, { "related", "Полезно знать" } }) do
                local titled = false
                for _, item in ipairs(items) do
                    if item.relevance == group[1] then
                        if not titled then c.rows[#c.rows + 1] = { kind = "section", text = L(group[2]) }; titled = true end
                        c.rows[#c.rows + 1] = { kind = "check", title = item.text, text = item.why, value = day(item.date) }
                    end
                end
            end
            local y, mo, d = tostring(data.newest or ""):match("^(%d+)-(%d+)-(%d+)$")
            if y and time() - time({ year = tonumber(y), month = tonumber(mo), day = tonumber(d), hour = 12 }) > 10 * 86400 then
                -- A list that stopped moving may have moved to a new address.
                c.rows[#c.rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Blizzard давно не дополняла этот список: возможно, он переехал.") }
            end
            if data.url then
                c.rows[#c.rows + 1] = { kind = "sources", list = { { title = "WoW: Forever Hotfixes", url = data.url } }, at = found.at }
            end
            c.rows[#c.rows + 1] = refresh
            if #items > 0 then
                -- News that came while the player was away is announced once, also after a reload.
                c.persist, c.priority = true, 60
                c.signature = (data.newest or "") .. ":" .. #items .. ":" .. (items[1].date or "")
                c.signal, c.change = c.signature, L("Вышли исправления, которые тебя касаются")
            end
        else
            c.reason = L("Советник сверит исправления Blizzard с твоим классом, профессиями и заданиями.")
            -- The first look may be answered from the helper's cache; "check now" above always researches again.
            c.rows[1] = { kind = "actions", actions = { { label = L("Проверить сейчас"), primary = true, click = function() ask() end } } }
        end
        return c
    end,
    discuss = function()
        return L("Объясни, что из свежих исправлений важно для моего персонажа и что мне стоит изменить в игре.")
    end,
})

-- Once per login, a little after the world loads, and only when research is set to automatic.
local asked, entered
local function greet()
    if asked or not entered or GetTime() - entered < 8 then return end
    if not Insights.Automatic() or T.Store.codex ~= "ready" then return end
    asked = ask({ background = true, auto = true }) == true
end
local frame = CreateFrame("Frame")
frame:RegisterEvent("PLAYER_ENTERING_WORLD")
frame:SetScript("OnEvent", function()
    entered = entered or GetTime()
    C_Timer.After(8, greet)
end)
-- A helper that connects later is asked as soon as it is ready.
T.On("StoreChanged", greet)

-- One quiet chat line, in the same manner as a finished reply.
local announced
T.On("JournalAdviceChanged", function()
    local state, found = Insights.State(KIND, KEY)
    local items = state == "ready" and found.data and found.data.items or {}
    local signature = #items > 0 and (found.data.newest or "") .. ":" .. #items
    local card = signature and signature ~= announced and Hub.Cards(Hub.Model({})).news
    if not card or not Hub.IsUnread(card) then return end
    announced = signature
    if T.settings.alertChat and not T.Window.IsShown() and DEFAULT_CHAT_FRAME and ChatTypeInfo then
        local tone = ChatTypeInfo.WHISPER
        DEFAULT_CHAT_FRAME:AddMessage("Looking for Guidance: " .. L("Вышли исправления, которые тебя касаются") .. " (" .. #items .. "). /ask",
            tone.r, tone.g, tone.b)
    end
end)
