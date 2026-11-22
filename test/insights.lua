-- Journal research: nothing unasked by default except the free changes check after login, a small load budget,
-- the news card with its ID matches, its short explanation on request, its notice and its badge.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local samples
local function start(saved)
    local wow = dofile(here .. "/wow.lua")
    samples = samples or wow.Samples(here .. "/../samples/samples.json")
    looking_for_guidance_settings = saved or { research = "auto" }
    wow.slots = 512
    local ready = CopyTable(samples.payloads.ready)
    for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
    wow.payload = ready
    local T = wow.Load(here .. "/../looking-for-guidance")
    T.CharacterIdentity = function() return "Player-1-121A", "login" end
    T.settings.locale = "enUS"
    -- The class brief has its own test (test/gold_plan.lua); here it would add a second request.
    T.HubData.AskBrief = function() return false end
    return wow, T, ready
end
local function sentInsights(wow)
    local list = {}
    for _, packet in ipairs(wow.sent) do if packet.type == "insight" then list[#list + 1] = packet end end
    return list
end

local wow, T, ready = start(nil)
local hub = T.HubOverview
wow.Event("PLAYER_ENTERING_WORLD", true)
assert(#sentInsights(wow) == 0, "nothing is asked while the world is still loading")
local loads = #wow.loaded
wow.Run(9)
local asked = sentInsights(wow)
assert(#asked == 1 and asked[1].kind == "changes" and asked[1].key == "me", "one changes check follows the login")
assert(asked[1].facts.class == "MAGE" and asked[1].facts.level == 33 and asked[1].facts.quests[1].id == 676, "it carries the character's class, level and quests")
assert(asked[1].facts.map == 1417 and asked[1].facts.gear == "15275" and asked[1].facts.bags == "8079",
    "and game IDs as strings, which the packet trimming cannot cut: the map, worn and carried items")
assert(asked[1].facts.name == nil and asked[1].locale == "enUS" and asked[1].gameLocale == "ruRU")
assert(asked[1].cap == 60, "the daily cap from the settings travels with the request")
assert(asked[1].market.faction == "Alliance" and asked[1].market.region == nil, "the market goes along, with what the client knows of it")
assert(T.Store.Tracked(asked[1].seq), "a background request is confirmed like a chat message, so a lost one can be repeated")

local pending = CopyTable(ready)
pending.rev = ready.rev + 1
pending.acks = { [T.Transport.session] = { asked[1].seq } }
pending.insights = { { kind = "changes", key = "me", state = "pending", since = time() } }
wow.payload = pending
wow.Run(400)
assert(#wow.loaded - loads == 3, "a background request spends three loads at most, however long the helper takes: " .. (#wow.loaded - loads))
assert(not T.Store.Retried(asked[1].seq), "a confirmed request is not sent twice")
assert(#sentInsights(wow) == 1, "and is not sent again")

local card = hub.Cards(hub.Model({})).news
assert(card.rows[1].kind == "pending" and not card.signal, "the card shows that the advisor is working")

local done = CopyTable(pending)
done.rev = pending.rev + 1
done.insights = { { kind = "changes", key = "me", state = "ready", at = time(), data = {
    newest = "2026-11-11", url = "https://news.blizzard.com/en-us/article/24315281", key = "k1", general = 3, data = true,
    mine = {
        { date = "2026-11-09", category = "Quests", text = "There are now more Defias for the quest.", kind = "note", reason = "quest", name = "The Defias Brotherhood" },
        { date = "2026-11-08", kind = "data", reason = "item", name = "Staff of Jordan", id = 15275, fields = { { "item_level", "61", "60" }, { "stats", "", "" } } },
    },
    class = { { date = "2026-11-06", category = "Classes", text = "Arcane Blast lasts 18 seconds.", kind = "note", reason = "class", name = "Mage" } },
} } }
wow.payload = done
T.Sync.Check()
card = hub.Cards(hub.Model({})).news
assert(card.title == "What’s new" and card.reason == "Changes for you: 2" and card.kicker == "Hotfixes from 11.11")
assert(card.rows[1].text == "Concerns you directly" and card.rows[2].title == "There are now more Defias for the quest."
    and card.rows[2].text == "Quest in your log: The Defias Brotherhood" and card.rows[2].value == "9.11")
assert(card.rows[3].title == "Staff of Jordan" and card.rows[3].text == "Your item: Staff of Jordan\nItem level: 61 → 60\nStats changed",
    "a data change shows what changed with the numbers")
assert(card.rows[4].text == "For your class" and card.rows[5].text == "Your class" and card.rows[6].kind == "sources")
assert(card.rows[7].kind == "actions" and card.rows[7].actions[1].label == "Explain briefly" and card.rows[7].actions[2].label == "Check now")
assert(card.persist and card.signal == "k1")

-- Without the world dataset the card says so; when it waits for the Battle.net sign-in, it says where and why instead.
local rev = done.rev
local function newsWith(fields)
    local p = CopyTable(done)
    rev = rev + 1
    p.rev = rev
    for k, v in pairs(fields) do p.insights[1].data[k] = v end
    wow.payload = p
    T.Sync.Check()
    return hub.Cards(hub.Model({})).news
end
local function says(c, start)
    for _, r in ipairs(c.rows) do
        if r.kind == "text" and type(r.text) == "string" and r.text:find(start, 1, true) == 1 then return true end
    end
end
assert(says(newsWith({ no_world = true }), "The world data isn’t downloaded yet"), "a missing world dataset is named")
local waiting = newsWith({ no_world = true, world_sign_in = true })
assert(says(waiting, "The world data comes after a one-time Battle.net sign-in") and not says(waiting, "The world data isn’t downloaded yet"),
    "a dataset that waits for the sign-in says where to sign in, once")
-- Blue posts and news: a matched post shows the line that concerns the character under the post it is from; the
-- newest posts come as copyable links, matched ones first; the explanation is offered for notes and data changes only.
local withPosts = newsWith({
    mine = { { date = "2026-11-19", kind = "post", source = "blue", text = "Class Tuning Coming Soon", line = "Vampiric Embrace heals more.",
        reason = "spell", name = "Vampiric Embrace", url = "https://us.forums.blizzard.com/en/wow/t/800002/1" } },
    class = { { date = "2026-11-20", kind = "post", source = "news", text = "WoW: Forever Weekly", reason = "class", name = "Priest",
        url = "https://news.blizzard.com/en-us/article/90000001" } },
    posts = {
        { date = "2026-11-21", kind = "post", source = "blue", text = "Dungeon Experience Adjusted", url = "https://us.forums.blizzard.com/en/wow/t/800009/1" },
        { date = "2026-11-19", kind = "post", source = "blue", text = "Class Tuning Coming Soon", url = "https://us.forums.blizzard.com/en/wow/t/800002/1" },
    },
})
local r = withPosts.rows
assert(r[2].title == "Vampiric Embrace heals more." and r[2].text == "Blue post: Class Tuning Coming Soon\nYour spell: Vampiric Embrace" and r[2].value == "19.11",
    "a matched post shows its line, the post and why")
assert(r[4].title == "WoW: Forever Weekly" and r[4].text == "Blizzard news\nYour class", "a post matched by its title shows the title")
assert(r[5].text == "Blue posts and news" and r[6].kind == "sources" and #r[6].list == 3
    and r[6].list[1].title == "19.11 · Class Tuning Coming Soon" and r[6].list[2].url == "https://news.blizzard.com/en-us/article/90000001"
    and r[6].list[3].title == "21.11 · Dungeon Experience Adjusted", "links to the posts, the matched ones first, each once")
local last = r[#r]
assert(last.kind == "actions" and #last.actions == 1 and last.actions[1].label == "Check now", "posts alone are not sent for the short explanation")
card = newsWith({})
local toasts = #wow.frames
hub.Observe(hub.Cards(hub.Model({})))
assert(hub.UnreadCount() == 1 and #wow.chat == 1 and wow.chat[1]:find("New hotfixes concern your character (2)", 1, true),
    "news that arrived unprompted is unread on first sight and is announced once in chat")
local function visible(text)
    for _, f in ipairs(wow.texts) do if f:IsVisible() and f:GetText() == text then return f end end
end
assert(visible("Changes for you: 2"), "and with a toast")
hub.Observe(hub.Cards(hub.Model({})))
assert(#wow.chat == 1, "only once")

-- Opening the topic reads it. The page shows the list and a copyable source.
T.Window.Show()
local function button(text)
    for _, f in ipairs(wow.frames) do
        if f:IsVisible() and f.label and f.label.GetText and f.label:GetText() == text and f:GetScript("OnClick") then return f end
    end
end
button("Open changes"):Fire("OnClick")
assert(visible("There are now more Defias for the quest.") and visible("Quest in your log: The Defias Brotherhood") and visible("Concerns you directly"))
assert(hub.UnreadCount() == 0, "an opened card is read")
local source = button("news.blizzard.com · WoW: Forever Hotfixes")
assert(source, "the source is listed by host and title")
source:Fire("OnClick")
local copy
for _, f in ipairs(wow.frames) do if f.edit and f.heading and f:IsVisible() then copy = f end end
assert(copy and copy.edit:GetText() == "https://news.blizzard.com/en-us/article/24315281", "its address is selected for copying")

-- The short explanation is the adviser's: asked only by its button, then shown on top.
local before = #sentInsights(wow)
button("Explain briefly"):Fire("OnClick")
local explained = sentInsights(wow)
assert(#explained == before + 1 and explained[#explained].kind == "digest" and not explained[#explained].force and explained[#explained].facts.gear == "15275")
local told = CopyTable(done)
told.rev = done.rev + 1
told.insights[2] = { kind = "digest", key = "me", state = "ready", at = time(), data = { headline = "Two changes for your mage", newest = "2026-11-11",
    items = { { date = "2026-11-06", category = "Classes", text = "Arcane Blast lasts 18 seconds.", why = "You are a mage.", relevance = "direct" } } } }
wow.payload = told
T.Sync.Check()
card = hub.Cards(hub.Model({})).news
assert(card.title == "Two changes for your mage" and card.rows[1].text == "In short" and card.rows[2].text == "You are a mage.")

-- "Check now" asks again past the cache and is watched on screen.
before = #sentInsights(wow)
button("Check now"):Fire("OnClick")
local again = sentInsights(wow)
assert(#again == before + 1 and again[#again].kind == "changes" and again[#again].force == true and T.Store.Tracked(again[#again].seq))
assert(T.Insights.Watching(), "a request the player is looking at keeps the page polling")
assert(hub.Cards(hub.Model({})).news.rows[1].kind == "pending", "the old answer is not shown as the new one")

-- After /reload the same news stays read; a newer list is new again.
local saved = CopyTable(T.settings)
local wow2, T2 = start(saved)
T2.Insights.Request = function() return false end
wow2.payload = done
T2.Sync.Check()
T2.HubOverview.Observe(T2.HubOverview.Cards(T2.HubOverview.Model({})))
assert(T2.HubOverview.UnreadCount() == 0 and #wow2.chat == 0, "read news does not come back after a reload")
local newer = CopyTable(done)
newer.rev = done.rev + 5
newer.insights[1].data.key = "k2"
wow2.payload = newer
T2.Sync.Check()
T2.HubOverview.Observe(T2.HubOverview.Cards(T2.HubOverview.Model({})))
assert(T2.HubOverview.UnreadCount() == 1, "a new list of changes is unread again")

-- A fresh install researches only on click: after login only the free changes check, nothing on opening a section,
-- one request per button.
local wow4, T4 = start({})
assert(T4.settings.research == "click" and T4.settings.researchCap == 60, "research on click and 60 jobs a day are the defaults")
wow4.Event("PLAYER_ENTERING_WORLD", true)
wow4.Run(30)
assert(#sentInsights(wow4) == 1 and sentInsights(wow4)[1].kind == "changes", "with research on click the login asks only the free check")
T4.Window.Show()
local function press(w, text)
    for _, f in ipairs(w.frames) do
        if f:IsVisible() and f.label and f.label.GetText and f.label:GetText() == text and f:GetScript("OnClick") then f:Fire("OnClick") return true end
    end
end
local function tile(w, text)
    for _, f in ipairs(w.texts) do
        if f:IsVisible() and f:GetText() == text and f:GetParent():GetScript("OnClick") then f:GetParent():Fire("OnClick") return true end
    end
end
for _, label in ipairs({ "Where to level", "Bags", "Character check-up", "Gold and training", "What’s new" }) do
    assert(tile(wow4, label), "the overview has the tile " .. label)
    assert(#sentInsights(wow4) == 1, "opening " .. label .. " asks nothing")
    if label ~= "What’s new" then assert(press(wow4, "Back to overview"), "the section leads back to the overview") end
end
assert(not T4.Insights.Request("dungeon", "Ragefire Chasm:Horde", { name = "Ragefire Chasm" }, { auto = true }) and #sentInsights(wow4) == 1,
    "any request marked as unasked is refused")
local answered = CopyTable(done)
answered.rev = done.rev + 10
answered.acks = { [T4.Transport.session] = { sentInsights(wow4)[1].seq } }
wow4.payload = answered
T4.Sync.Check()
T4.Window.Show()
assert(press(wow4, "Explain briefly"), "the news card offers its explanation")
local clicked = sentInsights(wow4)
assert(#clicked == 2 and clicked[2].kind == "digest" and not clicked[2].force, "a press asks once, and the first press may be answered from the cache")
T4.settings.researchCap = 20
assert(T4.Insights.Request("digest", "y", { level = 1 }) and sentInsights(wow4)[3].cap == 20, "a changed cap goes out with the next request")

-- Turning the overview's marks off does not stop automatic research, and a failure offers a retry.
saved = CopyTable(T2.settings)
saved.autoAdvice = false
local wow5 = start(saved)
wow5.Event("PLAYER_ENTERING_WORLD", true)
wow5.Run(30)
assert(#sentInsights(wow5) == 1, "automatic research does not depend on the overview's unread marks")
saved.research = "click"
local wow3, T3, ready3 = start(saved)
wow3.Event("PLAYER_ENTERING_WORLD", true)
wow3.Run(30)
assert(#sentInsights(wow3) == 1 and sentInsights(wow3)[1].kind == "changes", "with research on click the login asks only the free check")
local failed = CopyTable(ready3)
failed.rev = ready3.rev + 1
failed.insights = { { kind = "changes", key = "me", state = "failed", note = "hotfix list unavailable" } }
wow3.payload = failed
T3.Sync.Check()
local broken = T3.HubOverview.Cards(T3.HubOverview.Model({})).news
assert(broken.rows[1].tone == "warn" and broken.rows[1].text == "hotfix list unavailable" and broken.rows[2].kind == "actions")

-- Another character's research is not shown.
failed.rev, failed.character = failed.rev + 1, "Player-1-OTHER"
T3.Sync.Check()
assert(#T3.Store.insights == 0)
function InCombatLockdown() return true end
assert(T3.Insights.Request("digest", "x", { level = 1 }) == true and #sentInsights(wow3) == 1, "requests wait for the end of combat")
function InCombatLockdown() return false end
wow3.Event("PLAYER_REGEN_ENABLED")
assert(#sentInsights(wow3) == 2)

-- Frames lost on the way: the helper's next payload does not confirm the request, so it goes out once more.
local lostSeq = sentInsights(wow3)[2].seq
local frames = #wow3.bits
while T3.Transport.Sending(lostSeq) do wow3.Run(0.04) end
local once = #wow3.bits - frames
wow3.Run(2.1)
assert(T3.Store.Retried(lostSeq) and T3.Store.Delivery(lostSeq) == "sending", "an unconfirmed request is repeated by itself")
local heard = CopyTable(failed)
heard.rev, heard.character = failed.rev + 1, nil
heard.acks = { [T3.Transport.session] = { lostSeq } }
heard.insights = { { kind = "digest", key = "x", state = "ready", at = time(), data = { headline = "ok", items = {} } } }
wow3.payload = heard
wow3.Run(4)
assert(#wow3.bits - frames == 2 * once, "the same frames went out exactly twice")
assert(T3.Store.Delivery(lostSeq) == "delivered" and T3.Insights.State("digest", "x") == "ready", "the repeated request is confirmed and answered")
assert(#sentInsights(wow3) == 2, "the repeat reuses the same envelope: the helper sees one sequence number")
print("Insights: research on click by default, free changes check at login, three-load budget, news card with ID matches, toast and chat notice, explanation on request, copyable source, forced recheck, persisted unread, failure, combat deferral and automatic repeat of a lost request OK")
