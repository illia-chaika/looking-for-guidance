-- Bag triage: what the client can tell (grey, quest, the tooltip's red lines), the helper's verdicts for the
-- rest, and the player's remembered wishes above both.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local ready = wow.Samples(here .. "/../samples/samples.json").payloads.ready
for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
wow.payload = ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
T.CharacterIdentity = function() return "Player-1-121A", "login" end
T.settings.locale = "enUS"
T.HubData.AskBrief = function() return false end
function UnitClass() return "Warlock", "WARLOCK", 9 end
function UnitLevel() return 10 end

-- name, quality, required level, type, subtype, equip slot, vendor price, class id, red tooltip line
local stock = {
    { 2589, "Linen Cloth", 1, 0, "Trade Goods", "Cloth", "", 13, 7, nil, 20 },
    { 2589, "Linen Cloth", 1, 0, "Trade Goods", "Cloth", "", 13, 7, nil, 6 },
    { 4562, "Severing Axe of the Monkey", 2, 9, "Weapon", "Two-Handed Axes", "INVTYPE_2HWEAPON", 310, 2, "Two-Handed Axes", 1, "10:1490:::1:12721" },
    { 117, "Broken Fang", 0, 0, "Miscellaneous", "Junk", "", 4, 15, nil, 3 },
    { 3238, "Johaan's Findings", 1, 0, "Quest", "Quest", "", 0, 12, nil, 1 },
    { 250366, "Recipe: Minor Cleric's Elixir", 1, 0, "Recipe", "Alchemy", "", 25, 9, "Requires Alchemy (45)", 1 },
    { 2449, "Earthroot", 1, 0, "Trade Goods", "Herb", "", 10, 7, nil, 5 },
    { 15275, "Staff of the Sage", 2, 20, "Weapon", "Staves", "INVTYPE_2HWEAPON", 900, 2, "Requires Level 20", 1 },
    { 4604, "Forest Mushroom Cap", 1, 0, "Consumable", "Food & Drink", "", 1, 0, nil, 1 },
}
local function link(s) return "|cffffffff|Hitem:" .. s[1] .. "::::::::" .. (s[12] or "") .. "|h[" .. s[2] .. "]|h|r" end
C_Container = {
    GetContainerNumSlots = function(b) return b == 0 and #stock or 0 end,
    GetContainerNumFreeSlots = function(b) return b == 0 and 1 or 0, 0 end,
    GetContainerItemInfo = function(_, i)
        local s = stock[i]
        return s and { itemID = s[1], hyperlink = link(s), stackCount = s[11], quality = s[3], hasNoValue = s[8] == 0 }
    end,
    GetContainerItemQuestInfo = function(_, i) return { isQuestItem = stock[i] and stock[i][1] == 3238 } end,
}
C_Item.GetItemInfo = function(itemLink)
    for _, s in ipairs(stock) do
        if itemLink == link(s) then return s[2], itemLink, s[3], 1, s[4], s[5], s[6], 20, s[7], 1, s[8], s[9] end
    end
end
ITEM_MIN_LEVEL = "Requires Level %d"
C_TooltipInfo = { GetBagItem = function(_, i)
    local s = stock[i]
    local lines = { { leftText = s[2], leftColor = { r = 1, g = 1, b = 1 } } }
    if s[1] == 4604 then lines[2] = { leftText = "Use: Restores 58 health over 18 sec.", leftColor = { r = 0, g = 1, b = 0 } } end
    if s[10] then lines[2] = { leftText = s[10], leftColor = { r = 1, g = 0.125, b = 0.125 } } end
    return { lines = lines }
end }

local function card() return hub.Cards(hub.Model({})).bags end
local function row(c, title)
    for _, r in ipairs(c.rows or {}) do if r.title == title then return r end end
end
local function section(c, title)
    local list, inside = {}, false
    for _, r in ipairs(c.rows or {}) do
        if r.kind == "section" then inside = r.text == title elseif inside and r.kind == "check" then list[#list + 1] = r.title end
    end
    return table.concat(list, "; ")
end
local function sent()
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" and packet.kind == "bags" then list[#list + 1] = packet end end
    return list
end

assert(card().rows == nil and #sent() == 0, "tooltips are not read until the card or a merchant is opened")
T.Window.Show()
local function button(text)
    for _, f in ipairs(wow.frames) do
        if f:IsVisible() and f.label and f.label.GetText and f.label:GetText() == text and f:GetScript("OnClick") then return f end
    end
end
for _, f in ipairs(wow.texts) do
    if f:IsVisible() and f:GetText() == "Bags" then f:GetParent():Fire("OnClick") break end
end
local c = card()
assert(section(c, "Sell to a vendor") == "Broken Fang ×3" and row(c, "Broken Fang ×3").value == "0g 0s 12c")
assert(section(c, "Not usable by you") == "Severing Axe of the Monkey; Recipe: Minor Cleric's Elixir", section(c, "Not usable by you"))
assert(row(c, "Severing Axe of the Monkey").text == "Two-Handed Axes", "the reason is the game's own red line, already in the player's language")
assert(section(c, "Useful later") == "Staff of the Sage" and row(c, "Staff of the Sage").text == "Requires Level 20")
assert(row(c, "Johaan's Findings").state == "done" and row(c, "Johaan's Findings").text == "Quest item")
assert(c.rows[1].kind == "verdict" and c.rows[1].title == "A vendor will free 3 slots" and c.rows[1].text == "About 0g 3s 47c in coin.")
assert(c.summary == "Slots you can free: 3" and row(c, "Broken Fang ×3").link:find("item:117", 1, true))

assert(#sent() == 0 and c.rows[#c.rows].kind == "actions", "with research on click, opening the card asks nothing and offers a button")
button("Judge the remaining items"):Fire("OnClick")
local asked = sent()
assert(#asked == 1 and #asked[1].facts.items == 3, "the button sends only what the client could not judge")
local first = asked[1].facts.items[1]
assert(first.id == 2589 and first.n == 26 and first.type == "Trade Goods" and first.sell == 13 and asked[1].facts.class == "WARLOCK")
assert(asked[1].facts.items[3].tip == "Use: Restores 58 health over 18 sec.", "the item's own tooltip text travels with it, so the helper judges its real effect")
assert(card().rows[#card().rows].kind == "pending")
local priced = asked[1].facts.priced
assert(#priced == 2 and priced[1].id == 4562 and priced[1].link == "item:4562::::::::10:1490:::1:12721" and priced[2].link == nil,
    "items the client ruled out go along by id for their price, a random-suffix one with its exact variant")
assert(first.link == nil, "an item without random bonuses needs no link")

local payload = CopyTable(ready)
payload.rev = ready.rev + 1
payload.insights = { { kind = "bags", key = asked[1].key, state = "ready", at = time(), data = { summary = "Frees one more slot.", items = {
    { item_id = 2589, verdict = "sell", reason = "Cheap cloth." }, { item_id = 2449, verdict = "bank", reason = "For Alchemy later." },
    { item_id = 4604, verdict = "use", reason = "Eat it and free the slot." } } } } }
wow.payload = payload
T.Sync.Check()
c = card()
assert(section(c, "Sell to a vendor") == "Linen Cloth ×26; Broken Fang ×3" and section(c, "Put in the bank") == "Earthroot ×5")
assert(section(c, "Use now") == "Forest Mushroom Cap" and c.rows[1].title == "A vendor will free 5 slots")

-- With auction prices the helper adds what each item asks for on the character's market.
payload = CopyTable(payload)
payload.rev = payload.rev + 1
local data = payload.insights[1].data
data.items[2] = { item_id = 2449, verdict = "auction", reason = "Herbalists pay well." }
data.prices, data.prices_at = { { id = 2449, unit = 40, listed = 120 }, { id = 4562, unit = 25000, listed = 2 } }, 1794600000
wow.payload = payload
T.Sync.Check()
c = card()
local herb, axe = row(c, "Earthroot ×5"), row(c, "Severing Axe of the Monkey")
assert(section(c, "List on the auction house") == "Earthroot ×5" and herb.value == "≈0g 2s 0c", herb.value)
assert(herb.text == "Herbalists pay well. On the auction house from 0g 0s 40c each, 120 on sale.", herb.text)
assert(axe.value == "0g 3s 10c" and axe.text == "Two-Handed Axes On the auction house from 2g 50s 0c each, 2 on sale.", axe.text)
assert(row(c, "Linen Cloth ×26").text == "Cheap cloth.", "an item nobody sells keeps its reason alone")
local stamp
for _, r in ipairs(c.rows) do if r.kind == "text" and r.text:find("Blizzard", 1, true) then stamp = r.text end end
assert(stamp == "Auction prices: Blizzard's snapshot of " .. date("%H:%M", 1794600000) .. ".", stamp)

-- A remembered wish wins over the advice to sell.
T.Store.memory = { character = "Player-1-121A", login = "login", notes = { { scope = "character", topic = "bags", text = "Keep my Linen Cloth for bandages." } } }
c = card()
assert(section(c, "Sell to a vendor") == "Broken Fang ×3" and row(c, "Linen Cloth ×26").text == "You asked to keep this.")

-- At a merchant the card raises one badge; new loot is scanned again and asked about once.
wow.Event("MERCHANT_SHOW")
c = card()
assert(c.signal == "merchant:3" and c.priority >= 80 and c.rows[1].tone == "good")
wow.Event("MERCHANT_CLOSED")
assert(not card().signal or card().signal ~= "merchant:3")
stock[#stock + 1] = { 765, "Silverleaf", 1, 0, "Trade Goods", "Herb", "", 8, 7, nil, 12 }
wow.Event("BAG_UPDATE_DELAYED")
wow.Run(1)
assert(not row(card(), "Silverleaf ×12") and #sent() == 1, "a changed bag is not sent again by itself")
button("Judge the remaining items"):Fire("OnClick")
assert(#sent() == 2 and sent()[2].key ~= asked[1].key)
C_TooltipInfo = nil
wow.Event("BAG_UPDATE_DELAYED")
assert(section(card(), "Not usable by you") == "" and section(card(), "Sell to a vendor") == "Broken Fang ×3", "without tooltip data only certain verdicts remain")
print("Bags: lazy scan, grey and quest items, red tooltip lines, helper verdicts, remembered wishes, merchant badge and rescans OK")
