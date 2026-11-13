local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
T.Window.Show()
local function control(test)
    for _, object in ipairs(wow.frames) do
        if object:IsVisible() and test(object) then return object end
    end
    error("missing visible control")
end
local function tab(text)
    return control(function(b) return b.SetActive and b.label and b.label:GetText() == text and b:GetScript("OnClick") end)
end
assert(T.Window.Current() == nil, "first opening is the overview")
tab("Чат"):Fire("OnClick")
local composer = control(function(f) return f.edit and f.choice and f.meter end)
local chats = T.Store.Threads()
assert(#chats >= 2)
local idle
for _, thread in ipairs(chats) do if thread.agent == "codex" and T.Store.Rank(thread) == 2 then idle=thread;break end end
assert(idle)
local other = chats[1] ~= idle and chats[1] or chats[2]
T.Window.Select(idle.id)
local item = "|cffffffff|Hitem:123::::::::|h[Example]|h|r"
composer.edit:SetText("Compare " .. item)
T.Window.Select(other.id)
assert(composer.edit:GetText() == "")
composer.edit:SetText("Другой вопрос")
T.Window.Select(idle.id)
assert(composer.edit:GetText() == "Compare " .. item)
local before = #wow.sent
tab("Обзор"):Fire("OnClick")
assert(not composer:IsVisible() and T.Window.Current() == nil)
tab("Память"):Fire("OnClick")
assert(not composer:IsVisible())
tab("Чат"):Fire("OnClick")
assert(composer:IsVisible() and composer.edit:GetText() == "Compare " .. item)
for i = before + 1, #wow.sent do
    assert(wow.sent[i].type ~= "create" and wow.sent[i].type ~= "reply", "opening a page sent a question")
end
T.Window.OpenPage("overview")
T.Window.Discuss("Local facts recorded in the journal.", {item}, "route", "Два задания по пути")
assert(T.Window.Current() == idle.id)
assert(composer.edit:GetText() == "Compare " .. item)
local request = wow.sent[#wow.sent]
assert(request.type=="reply" and request.thread==idle.id and request.journal.topic=="route")
assert(request.journal.facts:find("Local facts",1,true) and request.contextText:find("item:123::::::::",1,true))
local card = T.Store.LastQuestionTurn(idle)
assert(card.journal and card.journal.title == "Два задания по пути")
T.Window.Select(other.id)
assert(composer.edit:GetText() == "Другой вопрос")
print("Journal: first opening, explicit analysis in same chat, structured facts, untouched drafts and separate exact links OK")
