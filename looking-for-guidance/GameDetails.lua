local _, T = ...
local Details = {}
T.GameDetails = Details

local function plain(value)
    if issecretvalue and issecretvalue(value) then return nil end
    return value
end

local function safe(fn, ...)
    if fn then
        local ok, a, b = pcall(fn, ...)
        if ok then return plain(a), plain(b) end
    end
end

-- Use the full variant payload. Enchants, gems and random suffixes can differ
-- for two positions which share the same item ID.
function Details.Link(kind, id, link)
    link = plain(link)
    if type(link) ~= "string" or #link > 1024 then return nil end
    local payload = link:match("|H([^|]+)|h") or link
    local k, n, tail = payload:match("^(%a+):(%d+)(.*)$")
    if #payload <= 512 and k == kind and tonumber(n) == id and (tail == "" or tail:match("^:[%w:%.%-]*$")) then return payload end
end

local function text(value)
    value = plain(value)
    if type(value) ~= "string" or value == "" then return nil end
    value = value:gsub("|T.-|t", ""):gsub("|A.-|a", ""):gsub("|H.-|h(.-)|h", "%1"):
        gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""):gsub("[%c]+", " ")
    if value ~= "" then return value end
end

local function tooltip(data)
    local lines = data and plain(data.lines)
    if not lines then return end
    local result, size = {}, 0
    for _, line in ipairs(lines) do
        local left, right = text(line.leftText), text(line.rightText)
        local row = left and right and left .. " · " .. right or left or right
        if row then
            size = size + #row
            if size > 5200 then return result, true end
            result[#result + 1] = row
        end
    end
    if #result > 0 then return result end
end

function Details.Stats(key)
    local raw = safe(C_Item.GetItemStats, key)
    if type(raw) ~= "table" then return end
    local stats = {}
    for k, value in pairs(raw) do
        if type(plain(k)) == "string" and type(plain(value)) == "number" then stats[k] = value end
    end
    if next(stats) then return stats end
end

local function itemDetails(card)
    local key = card.link or ("item:" .. card.id)
    local name, _, quality, level, required, category, subtype, stack, equip, _, sell, _, _, bind,
        _, set, reagent, description = C_Item.GetItemInfo(key)
    card.name, card.quality, card.baseItemLevel = text(name), plain(quality), plain(level)
    card.requiredLevel, card.category, card.subtype = plain(required), text(category), text(subtype)
    card.equipLocation, card.stackLimit, card.vendorCopper = text(equip), plain(stack), plain(sell)
    card.bindType, card.setID, card.craftingReagent, card.description = plain(bind), plain(set), plain(reagent), text(description)
    card.itemLevel = safe(C_Item.GetDetailedItemLevelInfo, key)
    card.stats = Details.Stats(key)
    local spell, id = safe(C_Item.GetItemSpell, key)
    if id then card.useSpell = { id = id, name = text(spell) } end
    local data = safe(C_TooltipInfo and C_TooltipInfo.GetHyperlink, key)
    card.tooltip, card.tooltipTruncated = tooltip(data)
    if card.name and card.tooltip and not card.tooltipTruncated and safe(C_Item.IsItemDataCachedByID, card.id) ~= false then return "ready" end
    return (card.name or card.tooltip or card.stats) and "partial" or "unavailable"
end

local function spellDetails(card)
    local id = card.id
    local info = safe(C_Spell.GetSpellInfo, id)
    card.name = text(info and info.name) or text(safe(C_Spell.GetSpellName, id))
    card.description = text(safe(C_Spell.GetSpellDescription, id))
    card.rank = text(safe(C_Spell.GetSpellSubtext, id))
    if info then
        card.castMilliseconds = plain(info.castTime)
        card.minRange, card.maxRange = plain(info.minRange), plain(info.maxRange)
    end
    for _, cost in ipairs(safe(C_Spell.GetSpellPowerCost, id) or {}) do
        local part = { resource = text(cost.name), cost = plain(cost.cost), minimum = plain(cost.minCost),
            percent = plain(cost.costPercent), perSecond = plain(cost.costPerSec) }
        if next(part) then
            card.costs = card.costs or {}
            card.costs[#card.costs + 1] = part
        end
    end
    card.baseCooldownMilliseconds = safe(GetSpellBaseCooldown, id)
    local data = safe(C_TooltipInfo and C_TooltipInfo.GetSpellByID, id, false, true, true)
    card.tooltip, card.tooltipTruncated = tooltip(data)
    if card.name and (card.description or card.tooltip) and not card.tooltipTruncated and safe(C_Spell.IsSpellDataCached, id) ~= false then return "ready" end
    return (card.name or card.description or card.tooltip) and "partial" or "unavailable"
end

local explicit = {}
function Details.Remember(refs)
    local now = GetTime()
    for key, at in pairs(explicit) do
        if now - at > 3600 then explicit[key] = nil end
    end
    for _, ref in ipairs(refs or {}) do
        explicit[ref.link or (ref.kind .. ":" .. ref.id)] = now
    end
end

local function allowed(ref)
    if ref.section == "references" then
        return explicit[ref.link or (ref.kind .. ":" .. ref.id)] ~= nil
    end
    if not T.settings.extras then return false end
    local enabled = T.settings.context
    if ref.section == "catalog" then
        return ref.kind == "item" and (enabled.gear or enabled.bags) or ref.kind == "spell" and enabled.spells
    end
    return enabled[ref.section] == true
end

local function describe(ref, warm, character, login, bagCounts)
    local card = { kind = ref.kind, id = ref.id, link = ref.link, status = "unavailable" }
    if not allowed(ref) then card.reason = "category disabled"; return card end
    if ref.kind == "recipe" then
        local ok, result = pcall(T.Recipes.Probe, ref.id, character, login, bagCounts)
        if ok then return result end
        card.reason = "Recipe data unavailable."
        return card
    end
    if ref.link and ref.link ~= Details.Link(ref.kind, ref.id, ref.link) then
        card.reason = "invalid variant link"
        return card
    end
    local ok, status = pcall(ref.kind == "item" and itemDetails or spellDetails, card)
    card.status = ok and status or "unavailable"
    if not ok then card.reason = "client data unavailable" end
    if warm and card.status ~= "ready" then
        if ref.kind == "item" then safe(C_Item.RequestLoadItemDataByID, ref.id)
        else safe(C_Spell.RequestLoadSpellData, ref.id) end
    end
    return card
end

local function cut(value, bytes)
    if not value or #value <= bytes then return value end
    local last = bytes
    while last > 0 and value:byte(last + 1) >= 128 and value:byte(last + 1) < 192 do last = last - 1 end
    return value:sub(1, last)
end

local function compact(packet, pass)
    local lines, bytes = pass == 1 and 12 or 4, pass == 1 and 280 or 140
    for i, card in ipairs(packet.cards) do
        if pass == 3 then
            packet.cards[i] = { kind = card.kind, id = card.id, link = card.link, name = cut(card.name, 120),
                status = "partial", reason = "Oversized batch. Request this entry separately." }
        else
            local changed = false
            if card.tooltip then
                while #card.tooltip > lines do table.remove(card.tooltip); changed = true end
                for j, line in ipairs(card.tooltip) do
                    if #line > bytes then card.tooltip[j], changed = cut(line, bytes), true end
                end
                if changed then card.tooltipTruncated = true end
            end
            if card.description and #card.description > bytes * 4 then
                card.description, changed = cut(card.description, bytes * 4), true
            end
            if changed then
                card.status, card.reason = "partial", "Oversized batch. Request this entry separately."
            end
        end
    end
end

local current
local function answer(state, attempt)
    if current ~= state then return end
    local cards, pending = {}, false
    for _, ref in ipairs(state.refs) do
        local card = describe(ref, attempt == 1, state.character, state.login, state.bagCounts)
        cards[#cards + 1] = card
        pending = pending or (card.status ~= "ready" and not card.reason)
    end
    -- Wait for a cold client cache without delaying the game thread. Keep every
    -- requested identity in the final answer, including explicit misses.
    if pending and attempt < 4 then
        C_Timer.After(({ 0.25, 0.75, 2 })[attempt], function() answer(state, attempt + 1) end)
        return
    end
    for _, card in ipairs(cards) do
        if not card.reason and card.status ~= "ready" then
            card.reason = card.status == "partial" and "The client returned only part of this entry." or "No usable client data for this ID."
        end
    end
    -- At 375 compressed bytes/s, 6.5 KiB fits inside the MCP deadline
    -- even after the longest normal slot polling interval and a cold cache.
    local seq, _, body = T.Transport.Send({ type = "details", requestId = state.id,
        runId = T.Store.runId, character = state.character, login = state.login, cards = cards }, 6656, compact)
    state.seq, state.body = seq, body
    if seq then T.Store.Track(seq) end
end

function Details.Receive(request)
    -- One request at a time travels in the payload; an auction upgrade search has its own handler.
    local upgrade = type(request) == "table" and request.upgrade ~= nil
    T.AuctionUpgrades.Receive(upgrade and request or nil)
    if upgrade then request = nil end
    if type(request) ~= "table" then current = nil; return end
    if request.session ~= T.Transport.session or type(request.id) ~= "string" or #request.id > 80
        or type(request.refs) ~= "table" or #request.refs < 1 or #request.refs > 6 then return end
    if current and current.id == request.id then
        if current.seq and T.Store.Delivery(current.seq) == "failed" then
            T.Transport.Resend(current.seq, current.body)
            T.Store.Track(current.seq)
        end
        return
    end
    for _, ref in ipairs(request.refs) do
        if type(ref) ~= "table" or (ref.kind ~= "item" and ref.kind ~= "spell" and ref.kind ~= "recipe")
            or type(ref.id) ~= "number" or ref.id < 1 or ref.id % 1 ~= 0 or ref.id > 2147483647
            or type(ref.section) ~= "string" or (ref.link ~= nil and type(ref.link) ~= "string") then return end
        if ref.kind == "recipe" and (ref.section ~= "skills" or ref.link ~= nil or
            type(request.character)~="string" or type(request.login)~="string") then return end
    end
    current = { id = request.id, refs = request.refs, character=request.character, login=request.login, bagCounts=request.bagCounts == true }
    answer(current, 1)
end
