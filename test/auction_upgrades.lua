-- Auction upgrades: the helper's candidates for one slot reach the game through the detail request, the game drops
-- what the character cannot wear and answers with exact variant stats, prices and the worn item.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
function UnitLevel() return 16 end

-- item ID → name, required level, tooltip red lines
local catalogue = {
    [14158] = { "Pagan Vest", 21 },
    [847] = { "Chainmail Armor", 12, right = "Mail" },
    [2000] = { "Archmage Robe", 30 },
    [200] = { "Thick Cloth Vest", 17 },
    [9999] = { "Unseen Robe", 15 },
}
local loaded = { [14158] = true, [847] = true, [2000] = true, [200] = false, [9999] = false }
C_Item.IsItemDataCachedByID = function(id) return loaded[id] end
C_Item.RequestLoadItemDataByID = function(id) if id == 200 then C_Timer.After(1, function() loaded[200] = true end) end end
local function idOf(key) return tonumber(key:match("item:(%d+)")) end
C_Item.GetItemInfo = function(key)
    local item = catalogue[idOf(key)]
    local suffix = key:find(":12718", 1, true) and " of the Eagle" or key:find(":12719", 1, true) and " of the Whale" or ""
    if item and loaded[idOf(key)] then return item[1] .. suffix, key, 2, 26, item[2] end
end
C_Item.GetDetailedItemLevelInfo = function(key) return idOf(key) == 14158 and 26 or 20 end
C_Item.GetItemStats = function(key)
    if key == "item:14158::::::::::::1:12718" then return { ITEM_MOD_STAMINA_SHORT = 6, ITEM_MOD_INTELLECT_SHORT = 6 } end
    if key == "item:14158::::::::::::1:12719" then return { ITEM_MOD_STAMINA_SHORT = 6, ITEM_MOD_SPIRIT_SHORT = 6 } end
    if key == "item:200" then return { ITEM_MOD_STAMINA_SHORT = 3 } end
    if idOf(key) == 6096 then return { ITEM_MOD_SPIRIT_SHORT = 1 } end
end
C_TooltipInfo = { GetHyperlink = function(key)
    local item = catalogue[idOf(key)]
    local lines = { { leftText = item[1] }, { leftText = "Chest", rightText = item.right or "Cloth",
        rightColor = item.right and { r = 1, g = 0.125, b = 0.125 } or { r = 1, g = 1, b = 1 } } }
    lines[#lines + 1] = { leftText = "Requires Level " .. item[2], leftColor = item[2] > 16 and { r = 1, g = 0.125, b = 0.125 } or { r = 1, g = 1, b = 1 } }
    return { lines = lines }
end }
ITEM_MIN_LEVEL = "Requires Level %d"
GetInventoryItemID = function(_, slot) return slot == 5 and 6096 or nil end
GetInventoryItemLink = function(_, slot) return slot == 5 and "|cffffffff|Hitem:6096::::::::|h[Apprentice's Shirt]|h|r" or nil end
catalogue[6096] = { "Apprentice's Shirt", 1 }
loaded[6096] = true

local function answers()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "details" and packet.upgrades then list[#list + 1] = packet end end
    return list
end
local request = { id = "upgrade-1", session = T.Transport.session, upgrade = { slot = "chest", ahead = 5, items = {
    { i = 14158, v = { { 1399, 12719 }, { 7999, 12718 } } }, { i = 847, v = { { 500 } } }, { i = 2000, v = { { 900 } } },
    { i = 200, v = { { 300 } } }, { i = 9999, v = { { 100 } } } } } }
-- The payload carries the request until it is answered; every load of it hands it over again.
wow.payload.detailRequest = request
T.GameDetails.Receive(request)
assert(#answers() == 0, "items the client has never seen get time to arrive")
wow.Run(9)
local sent = answers()
assert(#sent == 1 and sent[1].requestId == "upgrade-1", "one answer after the wait")
local found = sent[1].upgrades
assert(found.level == 16 and found.unusable == 1 and found.too_high == 1 and found.not_loaded == 1 and found.usable == 3,
    "mail is dropped by the tooltip, level 30 is out of reach, an unloaded item is counted, the rest is kept")
assert(found.items[1].link == "item:14158::::::::::::1:12719" and found.items[1].price == 1399 and found.items[1].name == "Pagan Vest of the Whale",
    "the highest item level first, the cheaper variant first among equals")
assert(found.items[2].stats.INTELLECT == 6 and found.items[2].stats.STAMINA == 6 and found.items[2].req == 21, "short stat names and the required level")
assert(found.items[3].link == "item:200" and found.items[3].price == 300, "an item that loaded during the wait is judged")
assert(#found.worn == 1 and found.worn[1].link == "item:6096::::::::" and found.worn[1].stats.SPIRIT == 1, "what is worn in the slot goes along")

T.GameDetails.Receive(request)
assert(#answers() == 1, "the same request is not answered twice")
T.GameDetails.Receive({ id = "x", session = "another", upgrade = { slot = "chest", items = {} } })
T.GameDetails.Receive({ id = "y", session = T.Transport.session, upgrade = { slot = "tail", items = {} } })
wow.Run(9)
assert(#answers() == 1, "a foreign session or an unknown slot is ignored")
-- A one-hand weapon for the off hand needs dual wield, which the tooltip does not show.
catalogue[2207] = { "Jambiya", 11 }
loaded[2207] = true
C_Item.GetItemInfoInstant = function(id) return id, "Weapon", "Daggers", id == 2207 and "INVTYPE_WEAPON" or "INVTYPE_SHIELD" end
CanDualWield = function() return false end
local offhand = { id = "upgrade-2", session = T.Transport.session, upgrade = { slot = "off_hand", items = { { i = 2207, v = { { 2500 } } } } } }
wow.payload.detailRequest = offhand
T.GameDetails.Receive(offhand)
wow.Run(1)
local last = answers()[#answers()]
assert(last.requestId == "upgrade-2" and last.upgrades.unusable == 1 and #last.upgrades.items == 0, "no off-hand dagger without dual wield")
print("Auction upgrades: wait for cold items, tooltip and level filters, variant stats, ranking and the worn item OK")
