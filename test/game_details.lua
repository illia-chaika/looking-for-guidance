-- Exercise the same WoW -> helper packet and helper -> game slot entry points as a real turn.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local checked = 0
local function check(label, condition)
    assert(condition, label)
    checked = checked + 1
end
looking_for_guidance_settings = { context = { spells = false }, extras = false }
local T = wow.Load(here .. "/../looking-for-guidance")
check("upgrade enables every category once", T.settings.extras and T.settings.context.gear and T.settings.context.spells)
for _, part in ipairs(T.Context.parts) do check("enabled " .. part.key, T.settings.context[part.key]) end
T.settings.context.buffs = false
local reload = {}
assert(loadfile(here .. "/../looking-for-guidance/Core.lua"))("looking-for-guidance", reload)
wow.Event("ADDON_LOADED", "looking-for-guidance")
check("an explicit later choice survives a fresh initialization", reload.settings.context.buffs == false)

local first, second = "item:15275:12", "item:15275:34"
GetInventoryItemLink = function(_, slot)
    return slot == 16 and ("|cff1eff00|H" .. first .. "|h[Клинок]|h|r") or nil
end
C_Container.GetContainerItemInfo = function(bag, slot)
    if bag ~= 0 or slot > 2 then return end
    local link = slot == 1 and first or second
    return { itemID = 15275, hyperlink = "|cff1eff00|H" .. link .. "|h[Клинок]|h|r", stackCount = 1 }
end
local loaded = {}
C_Item.GetItemInfo = function(key)
    if loaded[key] then return "Клинок", key, 2, 26, 21, "Weapon", "Sword", 1, "INVTYPE_WEAPON", 1001, 72 end
end
C_Item.GetItemStats = function(key)
    if key == first then return { INTELLECT = 3 } end
    if key == second then return { STAMINA = 4 } end
end
C_Item.RequestLoadItemDataByID = function(id)
    if id == 15275 then C_Timer.After(0.1, function() loaded[first], loaded[second] = true, true end) end
end
C_TooltipInfo = {
    GetHyperlink = function(key)
        if loaded[key] then return { lines = { { leftText = "|cff00ff00Клинок|r" }, { leftText = "+3 сила", rightText = "после зачарования" } } } end
    end,
    GetSpellByID = function(id) if id == 133 then return { lines = { { leftText = "Огненный шар" } } } end end,
}
C_Spell.GetSpellInfo = function(id) if id == 133 then return { name = "Огненный шар", castTime = 1500, minRange = 0, maxRange = 35 } end end
C_Spell.GetSpellDescription = function(id) if id == 133 then return "Наносит урон огнём." end end
C_Spell.GetSpellPowerCost = function() return { { name = "MANA", cost = 20 } } end
-- Some active talent ranks are granted without spending a point.
local captureNode, captureCost, captureCurrencies = C_Traits.GetNodeInfo, C_Traits.GetNodeCost, C_Traits.GetTreeCurrencyInfo
C_Traits.GetTreeCurrencyInfo = function() return {{traitCurrencyID=19, quantity=1, spent=0}} end
C_Traits.GetNodeCost = function() return {{ID=19, amount=2}} end
C_Traits.GetNodeInfo = function(...)
    local node = captureNode(...)
    node.ranksPurchased, node.canPurchaseRank = 0, true
    return node
end
local indexCards = {}
local serialize = C_EncodingUtil.SerializeJSON
C_EncodingUtil.SerializeJSON = function(value)
    if value.section and value.kind then indexCards[#indexCards + 1] = value end
    return serialize(value)
end
local snapshot = T.Context.Text()
C_EncodingUtil.SerializeJSON = serialize
C_Traits.GetNodeInfo, C_Traits.GetNodeCost, C_Traits.GetTreeCurrencyInfo = captureNode, captureCost, captureCurrencies
local granted
for _, card in ipairs(indexCards) do
    if card.section == "talents" then granted = card end
end
check("granted rank is distinct from spent talent points", granted and granted.rank == 2 and granted.purchasedRank == 0)
check("an eligible node can still cost more points than the player has", granted and granted.canPurchase and granted.canAfford == false)
local variants, equipped = {}, nil
for _, card in ipairs(indexCards) do
    if card.section == "gear" then equipped = card end
    if card.section == "bags" then variants[card.link] = card end
end
check("snapshot names exact equipped variant and includes stats, even when bags share the ID", equipped.link == first and equipped.name == "Клинок" and equipped.slot == 16 and equipped.stats.INTELLECT == 3)
check("summing stacks preserves distinct variants", variants[first].count == 1 and variants[second].count == 1)
check("default snapshot can resolve details", snapshot:find("Detail lookup: supported", 1, true))
check("snapshot omits long on-demand tooltips", not snapshot:find("после зачарования", 1, true))
local function request(id, refs)
    wow.payload.detailRequest = { id = id, session = T.Transport.session, refs = refs }
    looking_for_guidance_receive("slot")
end
local function response(id)
    for i = #wow.sent, 1, -1 do
        if wow.sent[i].type == "details" and wow.sent[i].requestId == id then return wow.sent[i] end
    end
end
request("compare", { { kind = "item", id = 15275, section = "gear", link = second }, { kind = "item", id = 15275, section = "gear", link = first },
    { kind = "spell", id = 133, section = "spells" }, { kind = "spell", id = 9999, section = "spells" } })
check("cold cache queues a bounded asynchronous read", not response("compare"))
for _ = 1, 37 do wow.Run(0.1) end
local res = assert(response("compare"), "no details reply")
check("ordered batch returns both exact variants", res.cards[1].link == second and res.cards[1].stats.STAMINA == 4 and res.cards[2].link == first and res.cards[2].stats.INTELLECT == 3)
check("WoW markup is removed but both tooltip columns survive", res.cards[1].tooltip[1] == "Клинок" and res.cards[1].tooltip[2] == "+3 сила · после зачарования")
check("spell description, cast time, range and costs are preserved", res.cards[3].status == "ready" and res.cards[3].description == "Наносит урон огнём." and res.cards[3].castMilliseconds == 1500 and res.cards[3].costs[1].cost == 20)
check("misses stay in position", res.cards[4].status == "unavailable" and res.cards[4].id == 9999)
local before = #wow.sent
looking_for_guidance_receive("same slot")
check("replayed request while sending doesn't enqueue another answer", #wow.sent == before)
T.settings.extras = false
local linked = T.Context.Text({ { kind = "item", id = 15275, link = second, name = "Клинок" } })
check("explicit references work with ambient context off", linked:find("References: 1", 1, true) and not linked:find("Equipped:", 1, true))
request("extras-off", { { kind = "item", id = 15275, section = "references", link = second }, { kind = "item", id = 15275, section = "gear", link = first },
    { kind = "item", id = 15275, section = "references", link = first } })
local off = assert(response("extras-off")).cards
check("explicit link bypasses only its own category", off[1].status == "ready" and off[2].status == "unavailable" and off[3].status == "unavailable")
check("a plain unlinked question carries no snapshot", T.Context.Text() == nil)
check("malformed or mismatched links are never used as an item variant", not T.GameDetails.Link("item", 1, "item:10:123") and not T.GameDetails.Link("item", 1, "item:1:/bad"))
-- Force enough unique localized lines to cross the actual transport budget.
-- The emulator leaves data uncompressed, so even a modest batch exercises the cap.
T.settings.extras = true
C_TooltipInfo.GetHyperlink = function()
    local rows = {}
    for i = 1, 18 do rows[i] = { leftText = string.rep("П", 75) .. i } end
    return { lines = rows }
end
local batch = {}
for i = 1, 6 do
    batch[i] = { kind = "item", id = 15275, link = i % 2 == 0 and first or second, section = "gear" }
end
request("long-reply", batch)
local bounded = assert(response("long-reply"))
check("oversized responses retain each identity and indicate a precise retry", #bounded.cards == 6 and bounded.cards[1].status == "partial" and bounded.cards[2].link == first and bounded.cards[1].reason)
check("oversized responses fit the MCP delivery deadline", #serialize(bounded) <= 6656)
if utf8 and bounded.cards[1].tooltip then
    for _, str in ipairs(bounded.cards[1].tooltip) do assert(utf8.len(str), "truncated Cyrillic codepoint") end
end
-- Transport delivery by itself never proves a snapshot can be referenced.
wow.payload.runId = "run-A"
wow.payload.detailRequest = nil
looking_for_guidance_receive("new session")
local thread = T.Store.Find("019a7c2e-0002")
local function ask()
    local seq = assert(T.Actions.Reply(thread, "Что теперь?"))
    for i = #wow.sent, 1, -1 do
        if wow.sent[i].seq == seq then return wow.sent[i] end
    end
end
local original = ask()
check("first question includes in-game cards and quests", original.contextText:find("Card:", 1, true) and original.contextText:find("Quests (1):", 1, true))
wow.payload.acks = { [T.Transport.session] = { original.seq } }
looking_for_guidance_receive("transport acknowledgement only")
check("delivery ack alone cannot shorten a snapshot", not ask().contextText:find("Index base:", 1, true))
local baselineID = string.rep("a", 32)
wow.payload.snapshotBases = { [T.Transport.session] = { seq = original.seq, id = baselineID } }
looking_for_guidance_receive("durable snapshot ack")
local compact = ask()
local same = assert(compact.contextText:match("Reuse: ([^\n]+)"))
check("snapshot ack reuses unchanged equipped/character catalog sections", compact.contextText:find("Index base: " .. baselineID,1,true) and same:find("gear",1,true) and same:find("spells",1,true))
check("volatile player and location still travel with each question", compact.contextText:find("Character:",1,true) and compact.contextText:find("Location:",1,true))
check("UI preview always displays a full snapshot", T.Context.Preview():find("Equipped:") and not T.Context.Preview():find("Index base:"))
GetInventoryItemLink = function() return "|H" .. second .. "|h[Клинок]|h" end
T.settings.context.spells = false
local changed = ask().contextText
same = assert(changed:match("Reuse: ([^\n]+)"))
check("a changed instance is sent in full and disabled sections cannot leak from a base", changed:find("Equipped:",1,true) and not same:find("gear",1,true) and not same:find("spells",1,true) and not changed:find("Spells known:",1,true))
wow.payload.snapshotBases = {}
looking_for_guidance_receive("base no longer available")
check("clearing a base forces full data in the same session", not ask().contextText:find("Index base:",1,true))
wow.payload.runId, wow.payload.snapshotBases = "run-B", { [T.Transport.session] = { seq = original.seq, id = baselineID } }
looking_for_guidance_receive("helper restarted")
check("a base from a previous helper run cannot be inherited", not ask().contextText:find("Index base:",1,true))
print(string.format("%d game data checks passed", checked))
