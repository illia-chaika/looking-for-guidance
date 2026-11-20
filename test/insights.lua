-- Journal research: nothing unasked by default; with automatic research one request after login, a small load
-- budget, the news card and its badge.
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
assert(#asked == 1 and asked[1].kind == "digest" and asked[1].key == "me", "one digest request follows the login")
assert(asked[1].facts.class == "MAGE" and asked[1].facts.level == 33 and asked[1].facts.quests[1].id == 676, "it carries the character's class, level and quests")
assert(asked[1].facts.name == nil and asked[1].locale == "enUS" and asked[1].gameLocale == "ruRU")
assert(asked[1].cap == 60, "the daily cap from the settings travels with the request")
assert(asked[1].market.faction == "Alliance" and asked[1].market.region == nil, "the market goes along, with what the client knows of it")
assert(T.Store.Tracked(asked[1].seq), "a background request is confirmed like a chat message, so a lost one can be repeated")

local pending = CopyTable(ready)
pending.rev = ready.rev + 1
pending.acks = { [T.Transport.session] = { asked[1].seq } }
pending.insights = { { kind = "digest", key = "me", state = "pending", since = time() } }
wow.payload = pending
wow.Run(400)
assert(#wow.loaded - loads == 3, "a background request spends three loads at most, however long the helper takes: " .. (#wow.loaded - loads))
assert(not T.Store.Retried(asked[1].seq), "a confirmed request is not sent twice")
assert(#sentInsights(wow) == 1, "and is not sent again")

local card = hub.Cards(hub.Model({})).news
assert(card.rows[1].kind == "pending" and not card.signal, "the card shows that the advisor is working")

local done = CopyTable(pending)
done.rev = pending.rev + 1
done.insights = { { kind = "digest", key = "me", state = "ready", at = time(), data = {
    headline = "Two changes for your mage", newest = "2026-11-11", url = "https://news.blizzard.com/en-us/article/24315281",
    items = {
        { date = "2026-11-09", category = "Dungeons", text = "Dungeon kill experience is up by 50%.", why = "You are level 33.", relevance = "related", quote = "…", quest_id = 0 },
        { date = "2026-11-06", category = "Classes", text = "Arcane Blast lasts 18 seconds.", why = "You are a mage.", relevance = "direct", quote = "…", quest_id = 0 },
    } } } }
wow.payload = done
T.Sync.Check()
card = hub.Cards(hub.Model({})).news
assert(card.title == "Two changes for your mage" and card.reason == "Changes for you: 2" and card.kicker == "Hotfixes of 11.11")
assert(card.rows[1].text == "Concerns you directly" and card.rows[2].title == "Arcane Blast lasts 18 seconds." and card.rows[2].value == "6.11")
assert(card.rows[3].text == "Good to know" and card.rows[5].kind == "sources" and card.rows[5].list[1].url:find("blizzard", 1, true))
assert(card.persist and card.signal == "2026-11-11:2:2026-11-09")
hub.Observe(hub.Cards(hub.Model({})))
assert(hub.UnreadCount() == 1 and #wow.chat == 1 and wow.chat[1]:find("New hotfixes concern your character (2)", 1, true),
    "news that arrived unprompted is unread on first sight and is announced once in chat")

-- Opening the topic reads it. The page shows the list and a copyable source.
T.Window.Show()
local function button(text)
    for _, f in ipairs(wow.frames) do
        if f:IsVisible() and f.label and f.label.GetText and f.label:GetText() == text and f:GetScript("OnClick") then return f end
    end
end
local function visible(text)
    for _, f in ipairs(wow.texts) do if f:IsVisible() and f:GetText() == text then return f end end
end
assert(visible("Two changes for your mage"), "the news leads the overview")
button("Open changes"):Fire("OnClick")
assert(visible("Arcane Blast lasts 18 seconds.") and visible("You are a mage.") and visible("Concerns you directly"))
assert(hub.UnreadCount() == 0, "an opened card is read")
local source = button("news.blizzard.com · WoW: Forever Hotfixes")
assert(source, "the source is listed by host and title")
source:Fire("OnClick")
local copy
for _, f in ipairs(wow.frames) do if f.edit and f.heading and f:IsVisible() then copy = f end end
assert(copy and copy.edit:GetText() == "https://news.blizzard.com/en-us/article/24315281", "its address is selected for copying")

-- "Check now" asks again past the cache and is watched on screen.
local before = #sentInsights(wow)
button("Check now"):Fire("OnClick")
local again = sentInsights(wow)
assert(#again == before + 1 and again[#again].force == true and T.Store.Tracked(again[#again].seq))
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
newer.insights[1].data.newest = "2026-11-12"
wow2.payload = newer
T2.Sync.Check()
T2.HubOverview.Observe(T2.HubOverview.Cards(T2.HubOverview.Model({})))
assert(T2.HubOverview.UnreadCount() == 1, "a newer hotfix list is unread again")

-- A fresh install researches only on click: nothing after login, nothing on opening a section, one request per button.
local wow4, T4 = start({})
assert(T4.settings.research == "click" and T4.settings.researchCap == 60, "research on click and 60 jobs a day are the defaults")
wow4.Event("PLAYER_ENTERING_WORLD", true)
wow4.Run(30)
assert(#sentInsights(wow4) == 0, "with research on click the login asks nothing")
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
    assert(#sentInsights(wow4) == 0, "opening " .. label .. " asks nothing")
    if label ~= "What’s new" then assert(press(wow4, "Back to overview"), "the section leads back to the overview") end
end
assert(not T4.Insights.Request("dungeon", "Ragefire Chasm:Horde", { name = "Ragefire Chasm" }, { auto = true }) and #sentInsights(wow4) == 0,
    "any request marked as unasked is refused")
assert(press(wow4, "Check now"), "the news card offers its button")
local clicked = sentInsights(wow4)
assert(#clicked == 1 and clicked[1].kind == "digest" and not clicked[1].force, "a press asks once, and the first press may be answered from the cache")
T4.settings.researchCap = 20
assert(T4.Insights.Request("digest", "y", { level = 1 }) and sentInsights(wow4)[2].cap == 20, "a changed cap goes out with the next request")

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
assert(#sentInsights(wow3) == 0, "with research on click the login asks nothing")
local failed = CopyTable(ready3)
failed.rev = ready3.rev + 1
failed.insights = { { kind = "digest", key = "me", state = "failed", note = "hotfix list unavailable" } }
wow3.payload = failed
T3.Sync.Check()
local broken = T3.HubOverview.Cards(T3.HubOverview.Model({})).news
assert(broken.rows[1].tone == "warn" and broken.rows[1].text == "hotfix list unavailable" and broken.rows[2].kind == "actions")

-- Another character's research is not shown.
failed.rev, failed.character = failed.rev + 1, "Player-1-OTHER"
T3.Sync.Check()
assert(#T3.Store.insights == 0)
function InCombatLockdown() return true end
assert(T3.Insights.Request("digest", "x", { level = 1 }) == true and #sentInsights(wow3) == 0, "requests wait for the end of combat")
function InCombatLockdown() return false end
wow3.Event("PLAYER_REGEN_ENABLED")
assert(#sentInsights(wow3) == 1)

-- Frames lost on the way: the helper's next payload does not confirm the request, so it goes out once more.
local lostSeq = sentInsights(wow3)[1].seq
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
assert(#sentInsights(wow3) == 1, "the repeat reuses the same envelope: the helper sees one sequence number")
print("Insights: research on click by default, login digest, three-load budget, news card, copyable source, forced recheck, persisted unread, failure, combat deferral and automatic repeat of a lost request OK")
