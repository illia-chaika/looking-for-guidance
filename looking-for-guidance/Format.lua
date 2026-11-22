local _, T = ...

local Format = {}
T.Format = Format

function Format.Escape(text)
    return (tostring(text or ""):gsub("|", "||"))
end

function Format.Agent(thread)
    return thread.agent == "claude" and "Claude" or "Codex"
end

function Format.Ago(seconds)
    seconds = math.max(0, math.floor(seconds))
    if seconds < 60 then
        return T.L("Только что")
    elseif seconds < 3600 then
        return T.L("{1} мин назад", math.floor(seconds / 60))
    elseif seconds < 86400 then
        return T.L("{1} ч назад", math.floor(seconds / 3600))
    end
    return T.L("{1} дн назад", math.floor(seconds / 86400))
end

function Format.Clock(seconds)
    seconds = math.max(0, math.floor(seconds))
    return string.format("%d:%02d", math.floor(seconds / 60), seconds % 60)
end

-- Older payloads only contain a tool name. Current helpers resolve the section at
-- the time of the call (including the section argument of the legacy tool).
local gameActivity = {
    get_quests = "Читаю журнал заданий", get_details = "Читаю игровые подсказки",
    get_entries = "Проверяю предметы и умения", get_context = "Проверяю данные персонажа",
    get_talents = "Проверяю таланты", search_spellbook = "Читаю книгу заклинаний",
    get_equipment = "Проверяю экипировку", search_bags = "Ищу в сумках",
    get_attached_links = "Смотрю вложения", get_skills = "Проверяю навыки",
    get_recipes = "Рецепты", get_auction_prices = "Аукцион", find_auction_upgrades = "Подбор на аукционе",
    search_game_data = "Поиск в базе мира", get_game_data = "База мира", get_changes = "Изменения в игре",
    get_installed_addons = "Аддоны", get_client_api_source = "Справка по API",
    get_buffs = "Проверяю эффекты", get_reputation = "Проверяю репутацию",
}

function Format.Activity(thread)
    if thread.compacting then return T.L("Сжимаю контекст…") end
    local activity = thread.activity or ""
    if activity == "Searching the web" then return T.L("Ищу в интернете") end
    local tool = activity:match(" · ([%w_]+)$")
    if tool and gameActivity[tool] then return T.L(gameActivity[tool]) end
    if activity == "" or activity == "Working" or activity == "Thinking" or activity == "Writing" then
        return T.L("{1} готовит ответ", Format.Agent(thread))
    end
    return T.L(activity)
end

-- The helper serializes tool counters and known scope names for older clients.
-- Translate their display components, preserving game names and quoted search text.
local traceCounters = {
    { "^Заданий: (%d+)$", "Заданий: {1}" }, { "^заданий: (%d+)$", "Заданий: {1}" },
    { "^найдено: (%d+)$", "найдено: {1}" }, { "^смещение (%d+)$", "смещение {1}" },
    { "^лимит (%d+)$", "лимит {1}" }, { "^слот (%d+)$", "слот {1}" },
    { "^ещё (%d+)$", "ещё {1}" }, { "^Инструменты: (%d+)$", "Инструменты: {1}" },
    { "^Интернет: (%d+)$", "Интернет: {1}" }, { "^скрыто: (%d+)$", "скрыто: {1}" },
    { "^записи (%d+)–(%d+) из (%d+)$", "записи {1}–{2} из {3}" },
    { "^Полных карточек: (%d+) из (%d+)$", "Полных карточек: {1} из {2}" },
    { "^допустимо от 1 до (%d+) записей$", "допустимо от 1 до {1} записей" },
}
local traceScopes = {
    ["персонаж"]="Персонаж", ["местность"]="Местность", ["версия игры"]="Версия игры",
    ["задания"]="Задания", ["экипировка"]="Экипировка", ["сумки"]="Сумки", ["таланты"]="Таланты",
    ["навыки"]="Навыки", ["заклинания"]="Заклинания", ["эффекты"]="Эффекты", ["цель"]="Цель",
    ["репутация"]="Репутация", ["вложения"]="Вложения", ["аддоны"]="Аддоны",
}
local function tracePart(part)
    for _, pair in ipairs(traceCounters) do
        local a,b,c = part:match(pair[1])
        if a then return T.L(pair[2],a,b,c) end
    end
    local absent = part:match("^Недоступно: (.+)$")
    if absent then
        local labels = {}
        for scope in absent:gmatch("[^,]+") do
            local name = scope:match("^%s*(.-)%s*$")
            labels[#labels+1] = T.L(traceScopes[name] or name)
        end
        return T.L("Недоступно: {1}",table.concat(labels,", "))
    end
    return T.L(traceScopes[part] or part)
end
function Format.ToolTrace(text)
    if not text or T.GetUILocale() == "ruRU" then return text end
    local parts = {}
    -- The separator is literal; periods and spaces in NPC names remain intact.
    local pos = 1
    while true do
        local start, finish = text:find(" · ",pos,true)
        parts[#parts+1] = tracePart(text:sub(pos,start and start-1 or -1))
        if not finish then break end
        pos = finish + 1
    end
    return table.concat(parts," · ")
end

function Format.Status(thread, now)
    local state = thread.state
    if state == "needs_you" then
        if not thread.approval then
            return T.L("Жду ответа")
        end
        return T.L(thread.approval.kind == "file" and "Запрос на изменение файлов" or "Запрос на запуск команды")
    elseif state == "error" then
        return T.L("Остановлено")
    elseif state == "active" then
        local activity = Format.Activity(thread)
        return activity .. " · " .. Format.Clock(now - (thread.since or thread.updated))
    elseif state == "queued" then
        if thread.id and thread.id:match("^local:") and not T.Store.Pending(thread) then
            return T.L("Нет подтверждения")
        end
        return T.L("Запускаю…")
    end
    return Format.Ago(now - thread.updated)
end

local waiting, failed = {}, {}

local LOOKUP = {
    item = function(id)
        local name, link = C_Item.GetItemInfo(id)
        if not name and not failed["item:" .. id] then
            waiting["item:" .. id] = true
            C_Item.RequestLoadItemDataByID(id)
        end
        return name, link
    end,
    spell = function(id)
        local name = C_Spell.GetSpellName(id)
        if not name and not failed["spell:" .. id] then
            waiting["spell:" .. id] = true
            C_Spell.RequestLoadSpellData(id)
        end
        return name, name and C_Spell.GetSpellLink(id)
    end,
}

function Format.Loaded(key, success)
    local was = waiting[key]
    waiting[key] = nil
    if success == false then
        failed[key] = true
    end
    return was and success ~= false
end

function Format.SourceHost(url)
    if type(url) ~= "string" then return end
    if #url > 4096 or url:find('[%s%c|\\"<>]') then return end
    local host = url:match("^https://([%w%.%-]+)/") or url:match("^https://([%w%.%-]+)[?#]") or url:match("^https://([%w%.%-]+)$")
    if host and host:match("^[%w%-]+%.[%w%.%-]+$") and not host:find("..", 1, true) then return host end
end

local function linkify(text, sources)
    return (text:gsub("%[([^%[%]]+)%](%b())", function(label, destination)
        local url = destination:sub(2, -2)
        local host = sources and Format.SourceHost(url)
        if host and not label:find("|", 1, true) then
            table.insert(sources, { url = url, host = host })
            return "|c" .. T.Kit.Hex("link") .. "|Haddon:looking-for-guidance:source:" .. #sources .. "|h[" .. label .. "]|h|r"
        end
        local kind, id = url:match("^(%a+):(%d+)$")
        if kind == "quest" then
            local quest = T.Context.Quest(tonumber(id))
            if not quest or Format.Escape(quest.title):lower() ~= label:lower() then return label end
            local link = "|c" .. T.Kit.Hex("quest") .. "|Hquest:" .. id .. ":" .. (quest.level or 0) .. "|h[" .. label .. "]|h|r"
            if T.Context.Waypoint(tonumber(id)) then
                link = link .. "  |c" .. T.Kit.Hex("link") .. "|Haddon:looking-for-guidance:waypoint:" .. id .. "|h[" .. T.L("Метка на карте") .. "]|h|r"
            end
            return link
        end
        local lookup = LOOKUP[kind]
        local name, link
        if lookup then
            name, link = lookup(tonumber(id))
        end
        if not link or not name or name:lower() ~= label:lower() then return label end
        if T.Kit.Reader() == T.Readers.journal then
            link = link:gsub("^|c%x%x%x%x%x%x%x%x", "|c" .. T.Kit.Hex("link"))
        end
        return link
    end))
end

local function emphasise(text, colour, sources)
    text = Format.Escape(text):gsub("%*%*([^*]+)%*%*", "|c" .. colour .. "%1|r")
    return linkify((text:gsub("`([^`]+)`", "|c" .. colour .. "%1|r")), sources)
end

local function cells(line)
    local row = {}
    line = line:gsub("^%s*|", ""):gsub("|%s*$", "")
    for cell in (line .. "|"):gmatch("(.-)|") do
        row[#row + 1] = cell:match("^%s*(.-)%s*$")
    end
    return row
end

-- The line under a Markdown table's header: |---|:---:|---:|
local function ruling(line)
    return line ~= nil and line:find("|", 1, true) and line:find("-", 1, true) and not line:find("[^%s|:%-]")
end

-- Both advisers write tables the same way: a header row, the ruling, then one row per line.
local function tabulate(lines, at, colour, sources)
    local head, marks = cells(lines[at]), cells(lines[at + 1])
    if #head ~= #marks then return end
    local block = { kind = "table", head = {}, rows = {}, align = {}, colour = colour }
    local seen = {}
    for column, cell in ipairs(head) do
        block.head[column] = emphasise(cell, colour, sources)
        block.align[column] = marks[column]:find(":$") and (marks[column]:find("^:") and "CENTER" or "RIGHT") or "LEFT"
        seen[#seen + 1] = block.head[column]
    end
    local nextLine = at + 2
    while lines[nextLine] and lines[nextLine]:find("|", 1, true) do
        local source, row = cells(lines[nextLine]), {}
        for column = 1, #head do
            row[column] = emphasise(source[column] or "", colour, sources)
            seen[#seen + 1] = row[column]
        end
        block.rows[#block.rows + 1] = row
        nextLine = nextLine + 1
    end
    block.text = table.concat(seen, " ")
    return block, nextLine
end

-- A table too wide for the window reads as a list: the first cell names the row, the rest follow with their headers.
function Format.TableRows(block)
    local rows = {}
    for _, row in ipairs(block.rows) do
        local name, rest = row[1], {}
        if name ~= "" and not name:find("|c", 1, true) then name = "|c" .. block.colour .. name .. "|r" end
        for column = 2, #row do
            if row[column] ~= "" then
                rest[#rest + 1] = (block.head[column] ~= "" and block.head[column] .. ": " or "") .. row[column]
            end
        end
        rows[#rows + 1] = name .. (#rest > 0 and name ~= "" and " · " or "") .. table.concat(rest, " · ")
    end
    return rows
end

function Format.Blocks(source, colour, sources)
    local lines = {}
    for line in (source .. "\n"):gmatch("(.-)\r?\n") do lines[#lines + 1] = line end
    local blocks, fenced, at = {}, false, 1
    while at <= #lines do
        local line, block, after = lines[at], nil, nil
        if not fenced and line:find("|", 1, true) and ruling(lines[at + 1]) then
            block, after = tabulate(lines, at, colour, sources)
        end
        if block then
            table.insert(blocks, block)
            at = after - 1
        elseif line:find("^```") then
            fenced = not fenced
        elseif fenced then
            table.insert(blocks, { kind = "code", text = "|c" .. colour .. Format.Escape(line) .. "|r" })
        elseif line:find("%S") then
            local heading = line:match("^%s?%s?%s?#+%s+(.-)%s*#*%s*$")
            local number, step = line:match("^%s*(%d+)[%.%)] (.*)$")
            local item = line:match("^%s*[%-%*] (.*)$")
            table.insert(blocks, {
                kind = heading and "heading" or (item or step) and "item" or "paragraph",
                number = number and number .. ".",
                text = emphasise(heading or step or item or line, colour, sources),
            })
        end
        at = at + 1
    end
    return blocks
end
