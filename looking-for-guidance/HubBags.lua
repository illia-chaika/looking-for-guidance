local _, T = ...
-- Bag triage beyond grey items. The client itself says what this character cannot use (the red lines of a
-- tooltip); the helper judges the rest from class, professions, level and the player's remembered wishes.
local Hub, Data, Insights = T.HubOverview, T.HubData, T.Insights
local call, clean, allowed, money = Hub.Call, Hub.Clean, Hub.Allowed, Hub.Money
local function L(...) return T.L(...) end
local KIND = "bags"
local SECTIONS = {
    { "sell", "Продать торговцу" }, { "unusable", "Тебе не подойдёт" }, { "auction", "Выставить на аукцион" },
    { "use", "Использовать сейчас" }, { "bank", "Убрать в банк" }, { "later", "Пригодится позже" }, { "keep", "Оставить" },
}

-- The scan reads a tooltip per slot, so it runs only for an opened card or an open merchant window, and only after bags changed.
local active, merchant, dirty, cache = false, false, true, nil

local function levelPattern()
    local format = type(ITEM_MIN_LEVEL) == "string" and ITEM_MIN_LEVEL or "Requires Level %d"
    local pattern = format:gsub("[%(%)%.%+%-%*%?%[%]%^%$]", "%%%0"):gsub("%%d", "(%%d+)")
    return "^" .. pattern .. "$"
end

local WORN = { INVTYPE_HEAD = 1, INVTYPE_NECK = 2, INVTYPE_SHOULDER = 3, INVTYPE_CHEST = 5, INVTYPE_ROBE = 5, INVTYPE_WAIST = 6,
    INVTYPE_LEGS = 7, INVTYPE_FEET = 8, INVTYPE_WRIST = 9, INVTYPE_HAND = 10, INVTYPE_FINGER = 11, INVTYPE_TRINKET = 13,
    INVTYPE_CLOAK = 15, INVTYPE_WEAPON = 16, INVTYPE_2HWEAPON = 16, INVTYPE_WEAPONMAINHAND = 16, INVTYPE_SHIELD = 17,
    INVTYPE_HOLDABLE = 17, INVTYPE_WEAPONOFFHAND = 17, INVTYPE_RANGED = 18, INVTYPE_RANGEDRIGHT = 18, INVTYPE_THROWN = 18 }

-- What a tooltip says in short: its effect and stat lines, and the first red line with whether that is only a level requirement.
local function tooltip(data, limit)
    local parts, red, levelOnly = {}, nil, nil
    for index, line in ipairs(data and data.lines or {}) do
        local text, color = clean(line.leftText), line.leftColor
        local r, g, b = color and clean(color.r), color and clean(color.g), color and clean(color.b)
        if type(text) == "string" and text ~= "" then
            if type(r) == "number" and r > .9 and g < .2 and b < .2 then
                if not red then red, levelOnly = text, text:match(levelPattern()) ~= nil end
            elseif index > 1 then
                parts[#parts + 1] = text
            end
        end
    end
    local summary = table.concat(parts, "; ")
    if #summary > limit then summary = summary:sub(1, limit):gsub("[\128-\191]*$", "") end
    return summary ~= "" and summary or nil, red, levelOnly
end

local function bagTooltip(bag, slot)
    if not C_TooltipInfo or not C_TooltipInfo.GetBagItem then return end
    return tooltip(call(C_TooltipInfo.GetBagItem, bag, slot), 120)
end

-- The piece worn in the slot a spare item would take, so the helper can compare instead of guessing.
local function wornText(equip)
    local slot = equip and WORN[equip]
    if not slot or not allowed("gear") or not C_TooltipInfo or not C_TooltipInfo.GetInventoryItem then return end
    local data = call(C_TooltipInfo.GetInventoryItem, "player", slot)
    local name = data and data.lines and data.lines[1] and clean(data.lines[1].leftText)
    if type(name) ~= "string" then return "nothing" end
    local stats = tooltip(data, 90)
    return name .. (stats and ": " .. stats or "")
end

local function scan(m)
    local items, order = {}, {}
    local protected = {}
    for _, item in ipairs(m.bag and m.bag.keep or {}) do protected[item.name] = item.quest or true end
    local info = C_Item and C_Item.GetItemInfo or GetItemInfo
    for bag = 0, NUM_BAG_SLOTS or 4 do
        for slot = 1, call(C_Container.GetContainerNumSlots, bag) or 0 do
            local stack = call(C_Container.GetContainerItemInfo, bag, slot)
            local link, id = stack and clean(stack.hyperlink), stack and clean(stack.itemID)
            local name = type(link) == "string" and link:match("|h%[(.-)%]|h")
            id = id or (type(link) == "string" and tonumber(link:match("item:(%d+)")))
            if id and name then
                local item = items[id]
                if not item then
                    local ok, _, _, quality, _, required, kind, sub, _, equip, _, price, classID = pcall(info, link)
                    item = { id = id, name = name, link = link, count = 0, slots = 0,
                        quality = clean(stack.quality) or (ok and clean(quality)) or nil,
                        required = ok and clean(required) or nil, kind = ok and clean(kind) or nil, sub = ok and clean(sub) or nil,
                        equip = ok and type(clean(equip)) == "string" and equip ~= "" and equip or nil,
                        price = ok and type(clean(price)) == "number" and price or 0, classID = ok and clean(classID) or nil,
                        noValue = clean(stack.hasNoValue) == true }
                    items[id], order[#order + 1] = item, item
                    if protected[name] then
                        item.verdict = "keep"
                        item.reason = type(protected[name]) == "string" and L("Нужно для задания: {1}", protected[name]) or L("Предмет для задания")
                    elseif item.quality == 0 and not item.noValue and item.price > 0 then
                        item.verdict, item.reason = "sell", L("Серый предмет: нужен только торговцу.")
                    else
                        local text, red, levelOnly = bagTooltip(bag, slot)
                        item.tip = text
                        if red and levelOnly then
                            item.verdict, item.reason = "later", red
                        elseif red then
                            item.verdict, item.reason = "unusable", red
                        end
                    end
                end
                item.count = item.count + (clean(stack.stackCount) or 1)
                item.slots = item.slots + 1
            end
        end
    end
    return order
end

local function current(m)
    if dirty or not cache then cache, dirty = scan(m), false end
    return cache
end

-- The items the client could not judge, in the short form the helper needs.
local function request(m, items)
    local undecided, sum = {}, 0
    for _, item in ipairs(items) do
        if not item.verdict and #undecided < 30 then
            undecided[#undecided + 1] = { id = item.id, name = item.name, q = item.quality, type = item.kind, sub = item.sub,
                req = item.required and item.required > 1 and item.required or nil, sell = item.price > 0 and item.price or nil,
                n = item.count, slot = item.equip, tip = item.tip, worn = wornText(item.equip) }
            sum = (sum + item.id * 31) % 1000000007
        end
    end
    if #undecided == 0 then return end
    local profile = Data.Profile()
    local band = profile.level and math.floor(profile.level / 10) * 10 or 0
    local key = (profile.class or "") .. ":" .. band .. ":" .. #undecided .. "-" .. sum
    return key, { class = profile.class, level = profile.level, professions = profile.professions, items = undecided }
end

local function ask(m, options)
    if not allowed("bags") then return end
    local key, facts = request(m, current(m))
    if key then Insights.Request(KIND, key, facts, options) end
end

Hub.Extend("bags", {
    tile = { subtitle = "Что продать, убрать и оставить" },
    open = function(m)
        active, dirty = true, true
        ask(m, { auto = true })
    end,
    build = function(m, c)
        if not c or not allowed("bags") or not C_Container then return c end
        if not (active or merchant) then return c end
        local items = current(m)
        local key = request(m, items)
        local state, found
        if key then state, found = Insights.State(KIND, key) end
        local judged = {}
        for _, verdict in ipairs(state == "ready" and found.data and found.data.items or {}) do judged[verdict.item_id] = verdict end

        local rows, groups = {}, {}
        local plan = c.plan and c.plan:lower() or ""
        for _, item in ipairs(items) do
            local verdict, reason = item.verdict, item.reason
            if not verdict and judged[item.id] then verdict, reason = judged[item.id].verdict, judged[item.id].reason end
            -- A wish the player asked to remember beats any advice to part with the item.
            if verdict and verdict ~= "keep" and plan ~= "" and plan:find(item.name:lower(), 1, true) then
                verdict, reason = "keep", L("Ты просил это оставить.")
            end
            if verdict then
                groups[verdict] = groups[verdict] or {}
                table.insert(groups[verdict], { item = item, reason = reason })
            end
        end
        local freed, coin = 0, 0
        for _, kind in ipairs({ "sell", "unusable" }) do
            for _, entry in ipairs(groups[kind] or {}) do
                freed = freed + entry.item.slots
                coin = coin + entry.item.price * entry.item.count
            end
        end
        if m.bag and m.bag.merge then
            local g = m.bag.merge
            rows[#rows + 1] = { kind = "check", state = "todo", title = L("Освободи ячейку на месте"),
                text = L("Объедини две стопки: {1} ({2} + {3}).", g.name or g.link, g.a, g.b) }
        end
        if freed > 0 then
            rows[#rows + 1] = { kind = "verdict", tone = merchant and "good" or nil, title = L("У торговца освободится ячеек: {1}", freed),
                text = coin > 0 and L("Выручка около {1}.", money(coin)) or nil }
        end
        for _, section in ipairs(SECTIONS) do
            local list = groups[section[1]]
            if list and #list > 0 then
                rows[#rows + 1] = { kind = "section", text = L(section[2]) }
                for index, entry in ipairs(list) do
                    if index > 10 then break end
                    local item = entry.item
                    local total = item.price * item.count
                    rows[#rows + 1] = { kind = "check", state = section[1] == "keep" and "done" or nil, link = item.link,
                        title = item.name .. (item.count > 1 and " ×" .. item.count or ""), text = entry.reason,
                        value = section[1] ~= "keep" and total > 0 and money(total) or nil }
                end
            end
        end
        if state == "pending" then
            rows[#rows + 1] = { kind = "pending", text = L("Советник оценивает остальные предметы…"), since = found.since }
        elseif state == "failed" then
            rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = L("Остальные предметы оценить не удалось.") .. " " .. (found.note or "") }
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Проверить заново"), click = function() ask(m, { force = true }) end } } }
        elseif state == "ready" then
            if found.data.summary then rows[#rows + 1] = { kind = "text", style = "t-meta", ink = "detail", text = found.data.summary } end
            rows[#rows + 1] = { kind = "sources", list = {}, at = found.at }
        elseif key then
            rows[#rows + 1] = { kind = "actions", actions = { { label = L("Оценить остальные предметы"), click = function() ask(m) end } } }
        end
        c.rows, c.steps = rows, {}
        if freed > 0 then
            c.summary = L("Можно освободить ячеек: {1}", freed)
            if not (m.bag and m.bag.merge) then c.title, c.reason = L("У торговца освободится ячеек: {1}", freed), c.kicker or c.reason end
        end
        if merchant and freed > 0 then
            -- At a merchant the list is worth a glance before the window closes.
            c.priority = math.max(c.priority or 0, 80)
            c.signature = "merchant:" .. freed
            c.signal, c.change = c.signature, L("Ты у торговца: можно продать лишнее")
        end
        return c
    end,
})

local frame = CreateFrame("Frame")
for _, event in ipairs({ "BAG_UPDATE_DELAYED", "PLAYER_LEVEL_UP", "SKILL_LINES_CHANGED", "MERCHANT_SHOW", "MERCHANT_CLOSED", "GET_ITEM_INFO_RECEIVED" }) do
    pcall(frame.RegisterEvent, frame, event)
end
frame:SetScript("OnEvent", function(_, event)
    if event == "MERCHANT_SHOW" then merchant = true elseif event == "MERCHANT_CLOSED" then merchant = false end
    dirty = true
end)
