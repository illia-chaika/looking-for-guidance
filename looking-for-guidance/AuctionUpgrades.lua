local _, T = ...
-- Auction upgrades. The helper sends the auctions of one equipment slot within the player's budget; the game drops
-- what this character cannot wear (red lines in the item's own tooltip, a level within reach excepted), reads the
-- exact stats of each random-suffix variant and answers with the highest item levels and what is worn there now.
local Upgrades = {}
T.AuctionUpgrades = Upgrades

local WORN = { head = { 1 }, neck = { 2 }, shoulder = { 3 }, chest = { 5 }, waist = { 6 }, legs = { 7 }, feet = { 8 },
    wrist = { 9 }, hands = { 10 }, finger = { 11, 12 }, trinket = { 13, 14 }, back = { 15 }, main_hand = { 16 },
    off_hand = { 17 }, ranged = { 18 } }
local SHOWN, WAIT_STEPS, BUDGET = 10, 16, 4096

local function plain(value)
    if issecretvalue and issecretvalue(value) then return nil end
    return value
end

local function safe(fn, ...)
    if fn then
        local ok, a, b, c, d, e = pcall(fn, ...)
        if ok then return plain(a), plain(b), plain(c), plain(d), plain(e) end
    end
end

-- The same layout as the links of auction variants: twelve empty fields, then the bonus list.
local function link(id, bonus)
    if #bonus == 0 then return "item:" .. id end
    return "item:" .. id .. string.rep(":", 12) .. #bonus .. ":" .. table.concat(bonus, ":")
end

local function short(stats)
    if not stats then return nil end
    local out = {}
    for key, value in pairs(stats) do out[(key:gsub("^ITEM_MOD_", ""):gsub("_SHORT$", ""))] = value end
    return out
end

local function levelPattern()
    local format = type(ITEM_MIN_LEVEL) == "string" and ITEM_MIN_LEVEL or "Requires Level %d"
    return "^" .. format:gsub("[%(%)%.%+%-%*%?%[%]%^%$]", "%%%0"):gsub("%%d", "(%%d+)") .. "$"
end

local function red(text, color)
    local r, g, b = color and plain(color.r), color and plain(color.g), color and plain(color.b)
    return type(text) == "string" and type(r) == "number" and r > .9 and g < .2 and b < .2
end

-- A red line other than the level requirement means this character cannot wear it: armour or weapon type, class.
local function blocked(id)
    local data = safe(C_TooltipInfo and C_TooltipInfo.GetHyperlink, "item:" .. id)
    local level = levelPattern()
    for _, line in ipairs(data and plain(data.lines) or {}) do
        local left, right = plain(line.leftText), plain(line.rightText)
        if (red(left, line.leftColor) and not left:match(level)) or red(right, line.rightColor) then return true end
    end
    return false
end

local function card(key, extra)
    extra.link, extra.name = key, (safe(C_Item.GetItemInfo, key))
    extra.ilvl, extra.stats = safe(C_Item.GetDetailedItemLevelInfo, key), short(T.GameDetails.Stats(key))
    return extra
end

local function compact(packet, pass)
    local items = packet.upgrades.items
    local keep = ({ 6, 3, 0 })[pass] or 0
    while #items > keep do table.remove(items) end
    if pass >= 2 then
        for _, item in ipairs(items) do item.name = nil end
        for _, item in ipairs(packet.upgrades.worn) do item.name = nil end
    end
end

local current
local function finish(state)
    local level = safe(UnitLevel, "player") or 1
    local found, unusable, high, cold = {}, 0, 0, 0
    for _, item in ipairs(state.items) do
        if safe(C_Item.IsItemDataCachedByID, item.id) == false then
            cold = cold + 1
        else
            local _, _, _, _, required = safe(C_Item.GetItemInfo, "item:" .. item.id)
            if blocked(item.id) then
                unusable = unusable + 1
            elseif type(required) == "number" and required > level + state.ahead then
                high = high + 1
            else
                for _, variant in ipairs(item.variants) do
                    found[#found + 1] = card(link(item.id, variant.bonus), { price = variant.price, req = required })
                end
            end
        end
    end
    table.sort(found, function(a, b)
        if (a.ilvl or 0) ~= (b.ilvl or 0) then return (a.ilvl or 0) > (b.ilvl or 0) end
        return a.price < b.price
    end)
    local items, worn = {}, {}
    for index = 1, math.min(#found, SHOWN) do items[index] = found[index] end
    for _, slot in ipairs(WORN[state.slot]) do
        local key = T.GameDetails.Link("item", safe(GetInventoryItemID, "player", slot) or 0, safe(GetInventoryItemLink, "player", slot))
        if key then worn[#worn + 1] = card(key, {}) end
    end
    local seq, _, body = T.Transport.Send({ type = "details", requestId = state.id, runId = T.Store.runId,
        upgrades = { items = items, worn = worn, level = level, usable = #found, unusable = unusable, too_high = high,
            not_loaded = cold } }, BUDGET, compact)
    state.seq, state.body = seq, body
    if seq then T.Store.Track(seq) end
end

-- Items the client has never seen arrive from the server; give them a few seconds, then answer with what is there.
local function wait(state, step)
    if current ~= state then return end
    local cold = false
    for _, item in ipairs(state.items) do
        if safe(C_Item.IsItemDataCachedByID, item.id) == false then cold = true break end
    end
    if cold and step < WAIT_STEPS then
        C_Timer.After(0.5, function() wait(state, step + 1) end)
        return
    end
    finish(state)
end

local function whole(n) return type(n) == "number" and n >= 0 and n % 1 == 0 and n <= 2147483647 end

function Upgrades.Receive(request)
    if type(request) ~= "table" then current = nil return end
    local query = request.upgrade
    if request.session ~= T.Transport.session or type(request.id) ~= "string" or #request.id > 80 or type(query) ~= "table"
        or not WORN[query.slot] or type(query.items) ~= "table" or #query.items > 400 then return end
    if current and current.id == request.id then
        if current.seq and T.Store.Delivery(current.seq) == "failed" then
            T.Transport.Resend(current.seq, current.body)
            T.Store.Track(current.seq)
        end
        return
    end
    local items = {}
    for _, item in ipairs(query.items) do
        if type(item) ~= "table" or not whole(item.i) or item.i < 1 or type(item.v) ~= "table" then return end
        local variants = {}
        for _, v in ipairs(item.v) do
            if type(v) ~= "table" or not whole(v[1]) then return end
            local bonus = {}
            for k = 2, #v do
                if not whole(v[k]) then return end
                bonus[#bonus + 1] = v[k]
            end
            variants[#variants + 1] = { price = v[1], bonus = bonus }
        end
        items[#items + 1] = { id = item.i, variants = variants }
    end
    current = { id = request.id, slot = query.slot, ahead = math.max(0, math.min(5, tonumber(query.ahead) or 0)), items = items }
    for _, item in ipairs(items) do safe(C_Item.RequestLoadItemDataByID, item.id) end
    wait(current, 0)
end
