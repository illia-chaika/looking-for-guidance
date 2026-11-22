local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")

local passed = 0
local function check(name, condition)
    if not condition then
        error("failed: " .. name, 2)
    end
    passed = passed + 1
end

local function find(test)
    for _, frame in ipairs(wow.frames) do
        if test(frame) then
            return frame
        end
    end
end

local function tool(name)
    return find(function(frame) return frame.glyph and frame.glyph.name == name and frame._scripts.OnClick end)
end

wow.payload = samples.payloads.offline
local T = wow.Load(here .. "/../looking-for-guidance")
-- Journal research has its own test (test/insights.lua); here it would add packets and loads.
T.Insights.Request = function() return false end

-- Run 0.04 s ticks (FRAME_SECONDS in Transport.lua) until the transport sent all frames of the last packet.
local function settle()
    local last = wow.sent[#wow.sent].seq
    while T.Transport.Sending(last) do
        wow.Run(0.04)
    end
end
-- Run until the add-on has decided about a packet: confirmed, or given up after sending it a second time by itself.
local function decided(seq)
    for _ = 1, 400 do
        local delivery = T.Store.Delivery(seq)
        if delivery == "failed" or delivery == "delivered" then return end
        wow.Run(0.04)
    end
end
check("login loads the first slot", #wow.loaded == 1)
check("login says hello", wow.sent[1].type == "hello")
check("the helper is offline", T.Store.codex == "offline")
check("offline is not something to wait on", not T.Store.Awaiting())

local starting = CopyTable(samples.payloads.ready)
starting.rev, starting.starting, starting.threads = starting.rev - 2, true, {}
wow.payload = starting
T.Sync.Now()
check("an app still starting is waited on", T.Store.Awaiting())
local started = CopyTable(starting)
started.rev, started.starting = started.rev + 1, nil
wow.payload = started
local startLoads = #wow.loaded
wow.Run(2)
check("the next check, 2 s later, reads the started app, and with nothing awaited the checks stop", #wow.loaded == startLoads + 1 and not T.Store.Awaiting())

T.Window.Show()
check("the window opens", T.Window.IsShown())

local function fold(placed)
    local window = looking_for_guidance_window
    local top, left, height = window:GetTop(), window:GetLeft(), window:GetHeight()
    tool("minimize"):Fire("OnClick")
    check("minimize keeps the window", T.Window.IsShown())
    check("the minimized bar sits where the title bar was, wherever " .. placed, window:GetTop() == top and window:GetLeft() == left and window:GetHeight() < height)
    tool("restore"):Fire("OnClick")
    check("restore brings the window back to the same place, wherever " .. placed, window:GetTop() == top and window:GetLeft() == left and window:GetHeight() == height)
end
fold("it first opened")

wow.payload = samples.payloads.ready
T.Sync.Now()
check("the helper's threads arrive", #T.Store.Threads() == 6)
local first = T.Store.Threads()[1]
check("threads needing you come first", first.state == "needs_you" or first.state == "error")
check("two threads need you", T.Store.Waiting() == 2)

for _, thread in ipairs(T.Store.Threads()) do
    T.Window.Select(thread.id)
    check("renders " .. thread.title, T.Window.Current() == thread.id)
end

local mana = T.Store.Find("019a7c2e-0002")
T.Window.Select(mana.id)
local seq = T.Actions.Reply(mana, "Thanks. Where do I train it?")
check("a reply is queued", seq ~= nil)
check("a reply is sending", T.Store.Delivery(seq) == "sending")
check("a sent reply is awaited", T.Store.Awaiting())
check("the first question has a game snapshot ready for MCP", wow.sent[#wow.sent].contextText and wow.sent[#wow.sent].contextText:find("Quests", 1, true))
check("the reply shows in the transcript", T.Store.Turns(mana)[#T.Store.Turns(mana)].pending)
settle()
check("the reply's frames go out", T.Store.Delivery(seq) == "sent")

wow.Run(2)
check("a reply the helper did not confirm goes out once more by itself", T.Store.Delivery(seq) == "sending" and T.Store.Retried(seq))
settle()
decided(seq)
check("no ack after the second window means not delivered", T.Store.Delivery(seq) == "failed")
local resent = T.Actions.Resend(mana, seq)
check("delivery retry reuses the original reply identity", resent == seq and T.Store.Delivering(mana))
check("a second click while sending adds no copy", T.Actions.Resend(mana, seq) == nil)
settle()

local acked = CopyTable(samples.payloads.ready)
acked.rev = acked.rev + 1
acked.acks = { [T.Transport.session] = { resent } }
wow.payload = acked
T.Sync.Now()
check("the helper's ack delivers it", T.Store.Delivery(resent) == "delivered")

local approval = T.Store.Find("019a7c2e-0004")
T.Window.Select(approval.id)
T.Actions.Answer(approval, true)
check("allow sends an approve packet", wow.sent[#wow.sent].type == "approve" and wow.sent[#wow.sent].approval == "1-12")
check("the card knows it was answered", T.Store.Answered(approval))

T.Actions.Stop(T.Store.Find("019a7c2e-0003"))
check("stop sends a stop packet", wow.sent[#wow.sent].type == "stop")

tool("plus"):Fire("OnClick")
check("new conversation has no thread", T.Window.Current() == nil)
local task = T.Actions.Ask("Plan my route from Refuge Pointe.")
check("asking starts a local task", task and task.state == "queued")
check("asking sends a create packet", wow.sent[#wow.sent].type == "create" and wow.sent[#wow.sent].model == "gpt-6.1-sol")

local adopted = CopyTable(acked)
adopted.rev = adopted.rev + 1
table.insert(adopted.threads, 1, {
    id = "019a7c2e-0006", title = "Plan my route from Refuge Pointe.", state = "active", project = "wow", cwd = "~/wow",
    model = "gpt-5.5", effort = "medium", updated = wow.now, since = wow.now, unread = false,
    origin = task.origin, turns = { { role = "user", text = "Plan my route from Refuge Pointe.", at = wow.now, context = true } },
})
wow.payload = adopted
T.Sync.Now()
check("the helper's thread replaces the local task", T.Store.Find(task.id) == nil and T.Store.Find("019a7c2e-0006") ~= nil)

local replied = CopyTable(adopted)
replied.rev = replied.rev + 1
replied.acks = { [T.Transport.session] = {} }
for _, packet in ipairs(wow.sent) do
    table.insert(replied.acks[T.Transport.session], packet.seq)
end
replied.threads[1].state = "idle"
for _, thread in ipairs(replied.threads) do
    if thread.state == "active" then
        thread.state = "idle"
    end
end
table.insert(replied.threads[1].turns, { role = "assistant", text = "Head north to **Go'Shek Farm** first.", at = wow.now })
for _, thread in ipairs(replied.threads) do
    if thread.agent == "claude" then
        thread.updated = wow.now
        table.insert(thread.turns, { role = "assistant", text = "Moved it.", at = wow.now })
    end
end
T.Window.Select("019a7c2e-0002")
local chats = #wow.chat
wow.payload = replied
T.Sync.Now()
local said = table.concat(wow.chat, "\n", chats + 1)
check("a finished reply alerts in chat, naming its agent", #wow.chat == chats + 2 and said:find("Codex ответил", 1, true) and said:find("Claude ответил", 1, true))
check("nothing in flight stops the checks", not T.Store.Awaiting())

local function listed(title)
    return find(function(frame) return frame.status and frame.title and frame.title._text == title and frame:IsVisible() end) ~= nil
end
check("the journal initially shows its conversation list", T.settings.window.sidebar and listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnClick")
check("the conversation list collapses", not T.settings.window.sidebar and not listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnClick")
check("the sidebar lists every conversation", T.settings.window.sidebar and listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnClick")
check("the sidebar closes", not T.settings.window.sidebar and not listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnEnter")
check("closed sidebar has a visible split-pane glyph and a specific tooltip", tool("sidebar").glyph:IsVisible()
    and not tool("sidebar").pane:IsShown() and GameTooltip._text == "Показать список чатов")
tool("sidebar"):Fire("OnLeave")
tool("sidebar"):Fire("OnClick")

local function row(title)
    return find(function(frame) return frame.status and frame.title and frame.title._text == title and frame:IsVisible() end)
end
local function text(value)
    for _, region in ipairs(wow.texts) do
        if region._text == value and region:IsVisible() then
            return region
        end
    end
end
check("each row starts with its agent", row("Running out of mana between pulls").status._text == "Codex · 4 мин назад")
check("Claude's rows name Claude", row("Move my mana bar after combat ends").status._text:find("^Claude · ") ~= nil)
check("initial groups use the conversation date", T.settings.list.group == "day")
local byDate = find(function(frame) return frame.text and frame.text._text == "По дате · Недавние" and frame._scripts.OnClick end)
assert(byDate):Fire("OnClick")
local byProject = find(function(frame) return frame.mark and frame.text and frame.text._text == "По папкам" and frame._scripts.OnClick end)
assert(byProject):Fire("OnClick")
byDate:Fire("OnClick")
check("rows group under their project", text("wow") ~= nil and text("ManaBar") ~= nil)
local sorter = find(function(frame) return frame.text and frame.text._text == "Папки · Недавние" and frame._scripts.OnClick end)
check("the sidebar names its grouping and order", sorter ~= nil)
sorter:Fire("OnClick")
local function option(label)
    return find(function(frame) return frame.mark and frame.text and frame.text._text == label and frame._scripts.OnClick end)
end
option("Без группировки"):Fire("OnClick")
check("ungrouped rows name their project", T.settings.list.group == "none" and text("ManaBar") == nil and row("Running out of mana between pulls").status._text == "Codex · 4 мин назад · wow")
option("Требуют внимания"):Fire("OnClick")
check("the choice of grouping and order is saved", T.settings.list.sort == "needs" and sorter.text._text == "Все · Требуют внимания")
option("По папкам"):Fire("OnClick")
option("Недавние"):Fire("OnClick")

T.Window.Select("019a7c2e-0002")
find(function(frame) return frame.summary and frame:IsVisible() end).summary:Fire("OnClick")
check("historical traces display their original tool", text("thott.bot · fetch") ~= nil)
local trace = { role = "activity", text = "Таланты · Заклинания · Экипировка · Интернет: 1", events = {
    { kind = "web", label = "Поиск в интернете", detail = "WoW Forever · Brill", url = "https://wow.example.org/brill", status = "ok" },
    { kind = "mcp", label = "Таланты", detail = "лимит 50 · допустимо от 1 до 30 записей", status = "failed" },
} }
local tested = T.Store.Find("019a7c2e-0002")
local replacement
for i, turn in ipairs(tested.turns) do
    if turn.role == "activity" then replacement = i break end
end
local original = tested.turns[replacement]
tested.turns[replacement] = trace
T.Window.Refresh()
check("a structured trace shows the real web destination and tool failure",
    text("Таланты · Заклинания · Экипировка · Интернет: 1") and text("Таланты · ошибка")
    and text("лимит 50 · допустимо от 1 до 30 записей") and
    find(function(f) return f.detail and f.sources and f.sources[1].host == "wow.example.org" and f:IsVisible() end))
tested.turns[replacement] = original
T.Window.Refresh()

tool("gear"):Fire("OnClick")
local function listening()
    return find(function(frame) return frame._events and frame._events.GLOBAL_MOUSE_DOWN end)
end
local popover = listening()
wow.Event("GLOBAL_MOUSE_DOWN", "LeftButton")
check("a click outside an open popover closes it, and a closed popover stops listening for clicks", popover and not popover:IsShown() and listening() == nil)
fold("the sidebar moved it")

local function key(label)
    return find(function(frame) return frame.label and frame.label._text == label end)
end
T.Window.Select("019a7c2e-0002")
local composer = find(function(frame) return frame.choice and frame.context and frame.edit end)
local context = composer.context
check("all sections are available by default", #T.Context.parts == 12 and context.text._text == T.Context.Summary())
for _, part in ipairs(T.Context.parts) do check("default " .. part.key, T.settings.context[part.key]) end
check("active conversation shows model and effort in footer", composer.choice:IsShown() and composer.choice.text._text == "GPT-6.1-Sol · medium")
composer.choice:Fire("OnClick")
check("the footer meter and model picker belong to the selected conversation",
    composer.meter.text:GetText():find("84%%") and composer.picker:IsVisible() and text("Для следующего ответа"))
composer.choice:Fire("OnClick")
context:Fire("OnClick")
local function settingsLink(label)
    return find(function(frame) return frame.text and frame.text._text == label and frame._scripts.OnClick end)
end
local all, none = settingsLink("Выбрать все"), settingsLink("Снять все")
none:Fire("OnClick")
check("clearing a draft leaves saved categories unchanged", T.settings.context.character and not none:IsEnabled() and all:IsEnabled())
option("Персонаж"):Fire("OnClick")
check("a partial draft enables both bulk controls", none:IsEnabled() and all:IsEnabled())
none:Fire("OnClick")
all:Fire("OnClick")
check("gear, bags, reputation and buffs can be toggled independently", option("Экипировка") and option("Сумки") and option("Репутация") and option("Эффекты"))
key("Посмотреть данные"):Fire("OnClick")
check("preview lays each part out beside its name", text("Quests (1)") ~= nil and text("Foul Magics |c" .. T.Kit.Hex("ink-3") .. "(quest:676)|r level 36 · 2/4 Syndicate Conjuror") ~= nil)
check("the preview names each collected category and shows occupied gear slots", T.Context.Preview() == table.concat({
    "Game: WoW Forever 1.60.1.70170",
    "Character: level 33 Human Mage, Alliance · XP 41200 / 66300, rested 12000 · 34g 56s 78c · No active pet",
    "Location: Refuge Pointe, Arathi Highlands 45.0, 47.0 · resting",
    "Quests (1):",
    "  Foul Magics (quest:676) level 36 · 2/4 Syndicate Conjuror",
    "Equipped: item:15275 (main hand)",
    "Bags: item:8079 ×25",
    "Talents: spell 11069 (Rank 2)",
    "Skills: First Aid 150/225",
    "Spells known: Fireball (Rank 1)",
    "Target: Elandra (npc:2601) level 33, hostile",
    "Reputation: Stormwind (faction:72) 3000",
    "Buffs: Arcane Intellect",
    "Installed add-ons: available on request",
}, "\n"))
local previewClose = find(function(frame) return frame.glyph and frame.glyph.name == "close" and rawget(frame._parent, "Open") and frame:IsVisible() end)
previewClose:Fire("OnClick")
local master = option("Прикладывать снимок персонажа")
master:Fire("OnClick")
check("disabling the draft snapshot disables its category controls", not option("Персонаж"):IsEnabled())
key("Сохранить"):Fire("OnClick")
check("saving the master switch excludes all automatic data", not T.settings.extras and T.Context.Text() == nil)
T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Just this")
check("a message sent with it off asks the app to add nothing", wow.sent[#wow.sent].plain == true and wow.sent[#wow.sent].contextText == nil)
context:Fire("OnClick")
master:Fire("OnClick")
key("Сохранить"):Fire("OnClick")
check("turned back on, saved categories are captured", T.settings.extras and T.Context.Text() ~= nil)
tool("gear"):Fire("OnClick")
local scale = find(function(frame) return frame.slider and frame.readout._text == "100%" end)
local opacity = find(function(frame) return frame.slider and frame.readout._text == "100%" and frame ~= scale end)
local font = find(function(frame) return frame.slider and frame.readout._text:find("пт", 1, true) end)
scale.slider:Fire("OnValueChanged", 111)
font.slider:Fire("OnValueChanged", 19)
opacity.slider:Fire("OnValueChanged", 75)
check("scale and type changes are staged", T.settings.scale == 1 and scale.readout._text == "110%")
check("appearance has an isolated preview and unsaved indicator", text("Предпросмотр") and text("Отнеси приказы в Подгород.") and text("Есть изменения"))
check("escape targets the sheet ahead of chat", tContains(UISpecialFrames, "looking_for_guidance_settings_panel") and not tContains(UISpecialFrames, "looking_for_guidance_window"))
key("Отмена"):Fire("OnClick")
tool("gear"):Fire("OnClick")
check("cancel discards all staged changes", scale.readout._text == "100%" and font.readout._text == "17 пт" and opacity.readout._text == "100%")
scale.slider:Fire("OnValueChanged", 110)
font.slider:Fire("OnValueChanged", 19)
opacity.slider:Fire("OnValueChanged", 75)
local function segment(label)
    return find(function(f) return f.value and f.label and f.label._text == label and f._scripts.OnClick end)
end
segment("Диалог"):Fire("OnClick")
key("Сохранить"):Fire("OnClick")
check("appearance, typography, size and alpha commit together", T.settings.scale == 1.1 and T.settings.fontSize == 19 and T.settings.opacity == .75 and T.settings.appearance == "dialog" and T.Tokens.font["t-body"].size == 19)
tool("gear"):Fire("OnClick")
segment("Трекер"):Fire("OnClick")
key("Сохранить"):Fire("OnClick")
check("tracker theme narrows the chat", T.settings.appearance == "tracker" and looking_for_guidance_window:GetWidth() == 555)
tool("gear"):Fire("OnClick")
segment("Журнал"):Fire("OnClick")
key("Сохранить"):Fire("OnClick")
local narrowComposer = T.Composer.Build(CreateFrame("Frame"), {})
narrowComposer:SetWidth(360)
narrowComposer:Set({ context = "Выбрать данные", choice = "GPT-6.1-Sol · medium", placeholder = "Ask anything" })
check("footer text and controls fit at minimum width", narrowComposer.context:GetWidth() + narrowComposer.choice:GetWidth() + 2 * T.Kit.space["space-3"] + T.Kit.space["space-2"] <= narrowComposer:GetWidth() + 0.01)
tool("plus"):Fire("OnClick")
composer.choice:Fire("OnClick")
local function pickerRow(kind, value)
    return find(function(f) return f.kind == kind and f.value == value and f:IsVisible() end)
end
check("model picker uses the catalog from the helper", pickerRow("model", "gpt-6-astra"))
pickerRow("model", "gpt-6-astra"):Fire("OnClick")
pickerRow("effort", "ultra"):Fire("OnClick")
check("choosing a model and effort immediately sets the next chat", T.Actions.Choice().model.id == "gpt-6-astra" and T.Actions.Choice().effort == "ultra")
pickerRow("more", nil):Fire("OnClick")
pickerRow("model", "claude:opus"):Fire("OnClick")
composer.picker:Hide()
local claude = T.Actions.Ask("Why does my bar move in combat?")
check("asking Claude sends a Claude create packet", wow.sent[#wow.sent].model == "claude:opus" and wow.sent[#wow.sent].effort == "high")
T.Window.Select(claude.id)
check("a new Claude conversation says where it is sending", find(function(frame) return frame.text and frame.text._text == "Передаю вопрос в Claude" and frame:IsVisible() end) ~= nil)

local busy = CopyTable(samples.payloads.ready)
busy.rev = busy.rev + 50
table.insert(busy.threads, { id = "glance-new", agent = "codex", title = "A fresh reply", state = "idle", project = "wow", updated = wow.now, unread = true, turns = {} })
table.insert(busy.threads, { id = "glance-read", agent = "codex", title = "Already read", state = "idle", project = "wow", updated = wow.now, unread = false, turns = {} })
table.insert(busy.threads, { id = "6d2f9b14-0007", agent = "claude", title = claude.title, state = "active", project = claude.project, origin = claude.origin, updated = wow.now, unread = false, turns = {} })
wow.payload = busy
T.Sync.Now()
local glanced = T.Glance.Threads()
local order, rank = true, -1
for index, thread in ipairs(glanced) do
    local this = T.Store.Rank(thread)
    order = order and this >= rank and (this < 2 or T.Store.IsUnread(thread))
    if index > 1 and this == rank then
        order = order and (glanced[index - 1].updated or 0) >= (thread.updated or 0)
    end
    rank = this
end
check("the glance keeps needs you, stopped, working and replied, in that order, newest first", order and glanced[1].id == "019a7c2e-0004" and glanced[2].id == "019a7c2e-0005")
local skipped = true
for _, thread in ipairs(glanced) do
    skipped = skipped and thread.id ~= "glance-read" and thread.state ~= "history"
end
check("the glance leaves out quiet conversations", skipped and T.Store.Find("glance-new") and glanced[#glanced].state == "idle")
tool("minimize"):Fire("OnClick")
local function glance()
    local rows = {}
    for _, frame in ipairs(wow.frames) do
        if frame.word and frame.title and frame:IsVisible() then
            table.insert(rows, frame)
        end
    end
    return rows
end
local shown = glance()
check("minimized shows at most four rows", #shown == 4 and shown[1].word._text == "Ждёт ответа" and shown[2].word._text == "Ошибка" and shown[3].word._text == "В работе")
check("the rest are counted", text("Ещё: " .. (#glanced - 4)) ~= nil)
local bar = find(function(frame) return frame.mark and frame.heading end)
check("minimized is titled with the mark and thott.bot, with only restore and close", text("Помощник") ~= nil and bar.mark:IsShown() and not tool("gear"):IsVisible() and not tool("sidebar"):IsVisible() and tool("restore"):IsVisible() and tool("close"):IsVisible())
check("minimized is narrow", T.settings.window.mini and find(function(frame) return frame.grip end):GetWidth() == 280)
shown[2]:Fire("OnClick")
check("a glance row restores the window to it, titled without the mark", not T.settings.window.mini and T.Window.Current() == "019a7c2e-0005" and not bar.mark:IsShown())

T.Window.Select("019a7c2e-0002")
local heading = find(function(frame) return frame._scripts.OnDoubleClick end)
heading:Fire("OnDoubleClick")
local box = find(function(frame) return frame._kind == "EditBox" and frame._scripts.OnEnterPressed and frame:IsShown() and frame._text == "Running out of mana between pulls" end)
check("double-clicking the name edits it", box ~= nil)
local sentBefore = #wow.sent
box:SetText("x")
box:Fire("OnEscapePressed")
check("escape cancels a rename", #wow.sent == sentBefore and not box:IsShown())
heading:Fire("OnDoubleClick")
box:SetText("   ")
box:Fire("OnEnterPressed")
check("an empty name is ignored", #wow.sent == sentBefore)
heading:Fire("OnDoubleClick")
box:SetText("Mana between pulls")
box:Fire("OnEnterPressed")
local renamed = wow.sent[#wow.sent]
check("enter sends a rename packet", renamed.type == "rename" and renamed.thread == "019a7c2e-0002" and renamed.title == "Mana between pulls")
check("the new name shows at once", text("Mana between pulls") ~= nil and T.Store.Title(T.Store.Find("019a7c2e-0002")) == "Mana between pulls")
local confirmed = CopyTable(busy)
confirmed.rev = confirmed.rev + 1
for _, thread in ipairs(confirmed.threads) do
    if thread.id == "019a7c2e-0002" then
        thread.title = "Mana between pulls"
    end
end
wow.payload = confirmed
T.Sync.Now()
check("the helper's title takes over", T.Store.Title(T.Store.Find("019a7c2e-0002")) == "Mana between pulls")

local function tip(value)
    local region = text(value)
    for _, other in ipairs(wow.texts) do
        if other._text == value and other:IsVisible() and other._parent._parent == UIParent then
            region = other
        end
    end
    return region and region._parent._parent == UIParent and region or nil
end
local long = row("Mana between pulls")
local previousGameTip = GameTooltip._text
long.title.IsTruncated = function() return true end
long:Fire("OnEnter")
check("a cut-off title shows in full on hover, in the addon's own tooltip", tip("Mana between pulls") ~= nil and GameTooltip._text == previousGameTip)
long:Fire("OnLeave")
check("the tooltip goes when the pointer leaves", tip("Mana between pulls") == nil)
local whole = row("Which of my quests are close by?")
whole:Fire("OnEnter")
check("a title that fits has no tooltip", tip("Which of my quests are close by?") == nil)

T.Alerts.Tooltip(CreateFrame("Button"))
check("the chat opens with /ask and /guidance", SLASH_LOOKING_FOR_GUIDANCE1 == "/ask" and SLASH_LOOKING_FOR_GUIDANCE2 == "/guidance" and SLASH_LOOKING_FOR_GUIDANCE3 == nil)
check("the minimap tooltip names the add-on", GameTooltip._text == "Looking for Guidance")
local toc = io.open(here .. "/../looking-for-guidance/looking-for-guidance.toc"):read("*a")
check("the AddOns list names the add-on", toc:find("## Title: Looking for Guidance\n", 1, true) ~= nil)

T.Fire("Problem", "low")
T.Fire("Problem", "DISABLED")
check("notices name the add-on", text("Включи группу Looking for Guidance в списке аддонов WoW.") ~= nil)

context:Fire("OnClick")
none:Fire("OnClick")
key("Сохранить"):Fire("OnClick")
check("None turns every part off", not T.settings.context.character and not T.settings.context.reputation and not none:IsEnabled())
check("with every part off nothing is attached", T.Context.Text() == nil)
check("the composer says so", T.Context.Summary() == "Выбрать данные")

for _, part in ipairs(T.Context.parts) do
    T.settings.context[part.key] = part.key == "buffs"
end
check("buffs go without reputation", T.Context.Preview() == "Game: WoW Forever 1.60.1.70170\nBuffs: Arcane Intellect")
T.settings.context.buffs, T.settings.context.bags = false, true
check("bags go without gear", T.Context.Preview() == "Game: WoW Forever 1.60.1.70170\nBags: item:8079 ×25")
T.settings.context.bags, T.settings.context.character, T.settings.context.target = false, true, true
issecretvalue = function(value) return value == 41200 or value == "Elandra" end
check("a value the game keeps secret is left out, never sent as nil", T.Context.Preview() == table.concat({
    "Game: WoW Forever 1.60.1.70170",
    "Character: level 33 Human Mage, Alliance · 34g 56s 78c · No active pet",
    "Target: (npc:2601) level 33, hostile",
}, "\n"))
issecretvalue = function() return false end
T.settings.context.character, T.settings.context.target = false, false

-- The auction house follows region, ruleset and faction; the Game line names the first two when the client tells.
T.settings.context.buffs = true
local rules, region = Enum.GameRule, GetCurrentRegion
Enum.GameRule = { PvPRuleset = 213, RPRuleset = 214 }
C_GameRules.IsGameRuleActive = function(rule) return rule == 213 end
GetCurrentRegion = function() return 3 end
check("the Game line names region and ruleset", T.Context.Preview() == "Game: WoW Forever 1.60.1.70170 · EU · PvP\nBuffs: Arcane Intellect")
C_GameRules.IsGameRuleActive = function() return true end
local market = T.Context.Market()
check("both rules mean Roleplay PvP", market.region == "EU" and market.ruleset == "RP-PvP" and market.faction == "Alliance")
C_GameRules.IsGameRuleActive = function() error("not in this client") end
check("a client without the rules says nothing about them", T.Context.Market().ruleset == nil)
Enum.GameRule, GetCurrentRegion, C_GameRules.IsGameRuleActive = rules, region, nil
check("without them the line is as before", T.Context.Preview() == "Game: WoW Forever 1.60.1.70170\nBuffs: Arcane Intellect")
T.settings.context.buffs = false

local function exchange(turns, n, at)
    table.insert(turns, { role = "user", text = "question " .. n, at = at })
    table.insert(turns, { role = "activity", text = "Read 1 file", items = {} })
    table.insert(turns, { role = "assistant", text = "answer " .. n, at = at + 5 })
end
local history = {}
for n = 1, 4 do
    exchange(history, n, wow.now - 100 + n * 10)
end
local function windowed(turns)
    local out = {}
    for i = math.max(1, #turns - 11), #turns do
        table.insert(out, turns[i])
    end
    return out
end
local function longThread(rev, state)
    local p = CopyTable(samples.payloads.ready)
    p.rev = p.rev + rev
    table.insert(p.threads, 1, { id = "long", agent = "codex", title = "A long conversation", state = state, project = "wow", updated = p.rev, unread = true, turns = windowed(history) })
    wow.payload = p
    T.Sync.Now()
end
longThread(200, "idle")
longThread(201, "active")
chats = #wow.chat
exchange(history, 5, wow.now)
longThread(202, "idle")
check("a reply alerts once the conversation outgrows the payload's window of turns", #wow.chat == chats + 1 and wow.chat[#wow.chat]:find("Codex ответил", 1, true) ~= nil)

tool("plus"):Fire("OnClick")
local lost = T.Actions.Ask("Plan a dungeon run.")
settle()
local unseen = CopyTable(samples.payloads.ready)
unseen.rev, unseen.acks = unseen.rev + 300, { [T.Transport.session] = {} }
wow.payload = unseen
local lostSeq = tonumber(lost.origin:match(":(%d+)$"))
decided(lostSeq)
check("a new conversation the app never saw is sent once more, then reads as not delivered, no longer working, and stops being waited on", T.Store.Retried(lostSeq) and T.Store.Delivery(lostSeq) == "failed" and not T.Store.Tracked(lostSeq) and T.Store.Rank(lost) == 2)
local again = T.Actions.Resend(lost, lostSeq)
local resentCreate = wow.sent[#wow.sent]
check("delivery retry keeps the original create identity and pending turn", again == lostSeq and resentCreate.type == "create" and resentCreate.text == "Plan a dungeon run." and lost.origin == T.Transport.session .. ":" .. again and #T.Store.Turns(lost) == 1)
settle()
local adoptedAgain = CopyTable(unseen)
adoptedAgain.rev, adoptedAgain.acks = adoptedAgain.rev + 1, { [T.Transport.session] = { again } }
table.insert(adoptedAgain.threads, 1, {
    id = "019a7c2e-0007", agent = "codex", title = "Plan a dungeon run.", state = "active", project = "wow", cwd = "~/wow",
    model = "gpt-5.5", effort = "medium", updated = wow.now, since = wow.now, unread = false,
    origin = lost.origin, turns = { { role = "user", text = "Plan a dungeon run.", at = wow.now, context = true } },
})
wow.payload = adoptedAgain
T.Sync.Now()
check("the app's thread replaces the resent conversation", T.Store.Find(lost.id) == nil and T.Store.Find("019a7c2e-0007") ~= nil)

T.Window.Select("019a7c2e-0002")
local lostReply = T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "And after that?")
settle()
T.Actions.Stop(T.Store.Find("019a7c2e-0003"))
local stopSeq = wow.sent[#wow.sent].seq
settle()
local later = CopyTable(adoptedAgain)
later.rev, later.acks = later.rev + 1, { [T.Transport.session] = { stopSeq } }
wow.payload = later
decided(lostReply)
check("a lost reply stays not delivered when a later packet is acked", T.Store.Delivery(lostReply) == "failed" and T.Store.Delivery(stopSeq) == "delivered")
local late = CopyTable(later)
late.rev, late.acks = late.rev + 1, { [T.Transport.session] = { stopSeq, lostReply } }
wow.payload = late
T.Sync.Now()
check("a late ack still delivers it", T.Store.Delivery(lostReply) == "delivered")

local sentAt
T.On("PacketSent", function() sentAt = GetTime() end)
local loads = #wow.loaded
local still = T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Still there?")
table.insert(late.acks[T.Transport.session], still)
settle()
wow.Run(60)
local gaps, last, cadence = { 2, 3, 5, 8, 13, 13, 13 }, sentAt, #wow.loaded == loads + 7
for index = loads + 1, #wow.loaded do
    cadence = cadence and wow.loaded[index].at - last == gaps[index - loads]
    last = wow.loaded[index].at
end
check("while a conversation is working, checks follow the frames by 2 s, then come 3, 5, 8 and 13 s apart, then every 13 s", cadence)

local quiet = CopyTable(late)
quiet.rev = quiet.rev + 1
for _, thread in ipairs(quiet.threads) do
    if T.Store.Rank(thread) == 1 then
        thread.state = "idle"
    end
end
wow.payload = quiet
local unheard = T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Hello?")
settle()
decided(unheard)
check("a message never acked stops being waited on once it reads as not delivered", T.Store.Retried(unheard) and T.Store.Delivery(unheard) == "failed" and not T.Store.Tracked(unheard) and not T.Store.Awaiting())
local left = T.Transport.SlotsLeft()
loads = #wow.loaded
wow.Run(120)
check("with nothing else awaited the checks stop, spending no more of the session's 128 loads", #wow.loaded == loads and T.Transport.SlotsLeft() == left)

-- Reads the saved frames back the way the helper does when none is missed: 48 data frames, then 6 check frames.
local function received()
    local packets, stream, count, offset = {}, nil, nil, 0
    for _, frame in ipairs(wow.bits) do
        local head = frame:byte(1)
        if head >= 128 then
            stream, offset = "", 0
        elseif head % 64 ~= ((count or 0) + 1) % 64 then
            stream = nil
        end
        count = head % 64
        if stream then
            local length = #stream >= 2 and stream:byte(1) * 256 + stream:byte(2)
            local data = not length or #stream < 2 + length
            if data and offset < 48 then stream = stream .. frame:sub(2) end
            offset = (offset + 1) % 54
            if head % 128 >= 64 then
                length = stream:byte(1) * 256 + stream:byte(2)
                table.insert(packets, { body = stream:sub(3, 2 + length), padding = stream:sub(3 + length) })
                stream = nil
            end
        end
    end
    return packets
end
local function same(a, b)
    for key, value in pairs(a) do
        if b[key] ~= value then
            return false
        end
    end
    for key in pairs(b) do
        if a[key] == nil then
            return false
        end
    end
    return true
end
local packets = received()
local intact = true
local wire, count = {}, {}
for _, got in ipairs(packets) do
    local n = wow.Decode(got.body).seq
    intact = intact and got.padding == string.rep("\0", #got.padding) and (not wire[n] or wire[n] == got.body)
    count[n], wire[n] = (count[n] or 0) + 1, got.body
end
for _, packet in ipairs(wow.sent) do
    intact = intact and wire[packet.seq] and same(packet, wow.Decode(wire[packet.seq]))
    -- A packet travels once, plus once when the add-on repeated it by itself, plus once for the player's own retry.
    local expected = 1 + (T.Store.Retried(packet.seq) and 1 or 0) + ((packet.seq == seq or packet.seq == lostSeq) and 1 or 0)
    intact = intact and count[packet.seq] == expected
end
check("every packet decodes intact and delivery retries have byte-identical envelopes", intact)
do
    local compress = C_EncodingUtil.CompressString
    C_EncodingUtil.CompressString = function() return string.rep("x", 65536) end
    local number, reason = T.Transport.Send({type = "reply", text = "Слишком длинный снимок"})
    C_EncodingUtil.CompressString = compress
    check("oversized compressed packets fail before sending a partial envelope", not number and reason == "oversized")
end

local old = CopyTable(samples.payloads.ready)
old.v, old.rev = 3, old.rev + 400
wow.payload = old
T.Sync.Now()
check("a payload from another protocol version asks to restart WoW, since the app installs its own addon", text("Перезапусти WoW, чтобы загрузить помощника.") ~= nil)

local link = EventRegistry.callbacks.SetItemRef
link("addon:looking-for-guidance:open:019a7c2e-0005")
check("a chat link opens its thread", T.Window.Current() == "019a7c2e-0005")

local signedOut = CopyTable(samples.payloads.ready)
signedOut.rev, signedOut.codex, signedOut.note = signedOut.rev + 99, "signed-out", "Sign in to the Codex CLI on this computer."
wow.payload = signedOut
T.Sync.Now()
check("the helper's reason shows when Codex isn't ready", find(function(frame) return frame.text and frame.text._text == signedOut.note end) ~= nil)

local away = CopyTable(samples.payloads.offline)
away.rev = signedOut.rev + 1
wow.payload = away
T.Sync.Now()
check("with the app away, the window asks for the app and the CLI it needs, and offers Проверить связь",
    text("Ожидаю подключение") and text("Открой Looking for Guidance на Mac. Когда помощник подключится, здесь появится игровой чат.") and key("Проверить связь"):IsVisible())
local restart = CopyTable(samples.payloads.restart)
restart.rev = away.rev + 1
wow.payload = restart
T.Sync.Now()
check("an addon older than the app asks for a WoW restart instead of looking connected",
    text("Перезапусти WoW") and text("Помощник обновляет аддон после выхода из игры.") and text("Ожидаю подключение") == nil
    and not key("Проверить связь"):IsVisible() and not composer:IsVisible())
local back = CopyTable(signedOut)
back.rev, back.codex, back.note = restart.rev + 1, "ready", ""
wow.payload = back
T.Sync.Now()

local linked = T.Format.Blocks("Train [Mage Armor](spell:6117), keep your [Hearthstone](item:6948), not [Frost Armor](spell:6117) or [Thing](item:999).", "ffffffff")[1].text
check("names the game confirms become its own links", linked:find("|Hspell:6117:0|h[Mage Armor]|h", 1, true) and linked:find("|Hitem:6948", 1, true))
check("names the game doesn't confirm stay plain text", linked:find("not Frost Armor or Thing.", 1, true) and wow.requested == 999)
wow.requested = nil
wow.Event("ITEM_DATA_LOAD_RESULT", 999, false)
T.Format.Blocks("Keep [Thing](item:999).", "ffffffff")
check("an id the game says doesn't exist is asked for once", wow.requested == nil)

T.settings.context.quests = true
C_QuestLog.GetSelectedQuest = function() return 676 end
C_QuestLog.GetQuestWatchType = function(id) return id == 676 and 0 or nil end
local snapshot = T.Context.Text()
check("capturing the log marks the selected and tracked quest without selecting any quest", snapshot:find("level 36 · selected · tracked · 2/4", 1, true))
T.settings.context.quests = false
check("turning the quest log off also hides the selected and tracked quest", not (T.Context.Text() or ""):find("quest:676", 1, true))

local function questLink()
    return T.Format.Blocks("[Foul Magics](quest:676) / [Wrong](quest:676) / [Missing](quest:777)", "ffffffff")[1].text
end
local picked, pinned
OpenQuestLogToQuest = function(id) picked = id end
check("a link opens only a matching logged quest", questLink():find("|Hquest:676:36|h[Foul Magics]|h", 1, true) and not questLink():find("|h[Wrong]|h", 1, true)
    and T.Context.OpenQuest(676) and picked == 676 and not T.Context.OpenQuest(777))
check("a game without quest waypoints displays no pin action", not questLink():find("waypoint", 1, true))
UiMapPoint = { CreateFromCoordinates = function(map, x, y) return { map=map, x=x, y=y } end }
C_Map.SetUserWaypoint = function(point) pinned = point end
C_Map.CanSetUserWaypointOnMap = function(map) return map == 1420 end
C_QuestLog.GetNextWaypoint = function() return 1420, 0.4, 0.51 end
check("a verified quest offers an explicit pin action without automatically setting a waypoint", questLink():find("waypoint:676", 1, true) and pinned == nil)
check("placing the pin uses current client-provided coordinates", T.Context.MarkQuest(676) and pinned.x == 0.4 and pinned.y == 0.51)
C_QuestLog.GetNextWaypoint = function() return 1420, 80, 52 end
check("unavailable or invalid map data cannot produce a waypoint", not T.Context.MarkQuest(676) and not T.Context.MarkQuest(777))

local transcript = T.Transcript.Build(CreateFrame("Frame"), {})
local range = 200
function transcript:GetVerticalScrollRange() return range end
local scrolling = { id = "scroll-test", state = "idle", turns = { { role = "assistant", text = "A reply." } } }
scrolling.turns = {
    { role = "user", text = "Куда дальше?", snapshotSummary = "Брилл · заданий: 12" },
    { role = "assistant", text = "Проверь журнал.", source = "Журнал заданий" },
}
transcript:Render(scrolling, wow.now)
check("an exchange renders its snapshot and the sources of its answer", text("Снимок · Брилл · заданий: 12") and text("Журнал заданий"))
check("web search is shown only when the backend explicitly reports it", T.Format.Activity({activity="Searching the web"}) == "Ищу в интернете"
    and T.Format.Activity({activity="Thinking"}) == "Codex готовит ответ"
    and T.Format.Activity({activity="wow_game · get_quests"}) == "Читаю журнал заданий")
wow.Run(0)
range = 260
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("following replies survives a layout range change", transcript.target == range)
transcript:Fire("OnMouseWheel", 1)
local readingAt = transcript.target
range = 310
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("a new reply preserves manual scrolling", transcript.target == readingAt)
transcript:ScrollToEnd()
range = 360
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("returning to the bottom follows replies again", transcript.target == range)
transcript:Fire("OnMouseWheel", 1)
for _ = 1, 30 do
    wow.Run(1 / 60)
end
check("the wheel glides the transcript to where it sends it", transcript:GetVerticalScroll() == transcript.target and transcript.target < range)
range = 0
transcript:ScrollTo(0)
for _ = 1, 30 do
    wow.Run(1 / 60)
end
check("with nothing left to scroll, the transcript stops its per-frame work", transcript:GetScript("OnUpdate") == nil)

local labelled = CopyTable(samples.payloads.ready)
labelled.rev = labelled.rev + 600
for _, model in ipairs(labelled.models) do
    model.label = model.label .. " |n"
end
wow.payload = labelled
T.Sync.Now()
tool("plus"):Fire("OnClick")
check("the composer shows a model's name as the app sent it, as the picker does", composer.choice.text._text:find(" ||n · ", 1, true) ~= nil)
local named = T.Actions.Ask(string.rep("é", 130))
check("a long question names its conversation by the first 120 characters, never half of one", named.title == string.rep("é", 120))

local function toasts(id)
    local count = 0
    for _, frame in ipairs(wow.frames) do
        if frame.stripe and frame.close and frame:IsShown() and frame.id == id then
            count = count + 1
        end
    end
    return count
end
local pulls, quests = T.Store.Find("019a7c2e-0002"), T.Store.Find("019a7c2e-0003")
T.Fire("StoreChanged", { replied = { pulls } })
T.Fire("StoreChanged", { replied = { quests } })
find(function(frame) return frame.stripe and frame.close and frame:IsShown() and frame.id == pulls.id end):Fire("OnClick")
T.Fire("StoreChanged", { replied = { quests } })
check("another reply in a conversation renews its toast instead of stacking a second", toasts(quests.id) == 1)

local url = "https://mobalytics.gg/wow/guide_(forever)?build=7#warlock"
local refs = {}
local formatted = T.Format.Blocks("[гайд Crix](" .. url .. ") и [ещё](https://example.org/guide)", "ffffffff", refs)[1].text
check("HTTPS sources render short labels, including URLs with balanced parentheses", #refs == 2 and refs[1].url == url
    and formatted:find("|Haddon:looking-for-guidance:source:1|h[гайд Crix]", 1, true) and not formatted:find("https://", 1, true))
refs = {}
T.Format.Blocks("[x](file:///tmp/demo) [y](javascript:alert(1)) [z](https://a.org/a|Hfoo) [spoof](https://foo.com@evil.org) [bad|H](https://a.org)", "ffffffff", refs)
check("unsafe schemes, embedded credentials and WoW control codes do not produce source links", #refs == 0)
transcript:Render({ id="source-test", state="idle", turns={ {role="assistant", text="[гайд Crix](" .. url .. ")"} } }, wow.now)
local sourceBlock = find(function(frame) return frame.sources and frame.sources[1] and frame.sources[1].url == url and frame:IsVisible() end)
sourceBlock:Fire("OnHyperlinkClick", "addon:looking-for-guidance:source:1", "", "LeftButton")
local copied = find(function(frame) return frame.edit and frame.edit:GetText() == url and frame:IsShown() end)
check("clicking a source opens the in-game copy field with its original URL", copied and copied.heading._text:find("mobalytics.gg", 1, true))
copied.edit:Fire("OnEscapePressed")
check("Escape closes the source field", not copied:IsShown())

local probe = T.Composer.Build(CreateFrame("Frame"), {
    send = function(value) wow.ownQuestion = value return true end,
})
probe:Set({ context = "Журнал", placeholder = "Спроси про игру…" })
probe:Draft("Мой вопрос")
check("typing a question preserves the draft across a refresh", probe.edit:GetText() == "Мой вопрос")
IsShiftKeyDown = function() return false end
probe.edit:Fire("OnEnterPressed")
check("Enter sends the player's question and clears a successful draft", wow.ownQuestion == "Мой вопрос" and probe.edit:GetText() == "")

-- Clicking a compact answer must make the rest of it readable in place.
local trackerProbe, story
local function redraw() trackerProbe:Render(story, wow.now) end
trackerProbe = T.Transcript.Build(CreateFrame("Frame"), { refresh = redraw })
trackerProbe.child:SetWidth(345)
story = { id = "reading", state = "idle", turns = {
    { role = "user", text = "Что дальше?" },
    { role = "assistant", text = "Дойди до города.\nВторая строка подробного ответа." },
} }
T.settings.appearance = "tracker"
redraw()
local function visibleLink(label)
    return find(function(f) return f.text and f.text._text == label and f._scripts.OnClick and f:IsVisible() end)
end
check("tracker shows the first paragraph and an explicit expand control", text("Дойди до города.") and not text("Вторая строка подробного ответа.") and visibleLink("Полный ответ"))
visibleLink("Полный ответ"):Fire("OnClick")
check("expanding the tracker shows the whole answer", text("Вторая строка подробного ответа.") and visibleLink("Свернуть ответ"))
T.settings.appearance = "journal"
story.turns[2].text = "Открой [Foul Magics](quest:676)."
redraw()
local tile = find(function(f) return f.cta and f.id == 676 and f:IsVisible() end)
picked = nil
tile:Fire("OnClick")
check("a quest card opens the matching live quest in the journal", picked == 676 and tile.name._text == "Foul Magics")

T.Window.Select("019a7c2e-0002")
visibleLink("История"):Fire("OnClick")
local jump = find(function(f) return f.turnIndex and f.label and f:IsVisible() end)
check("history indexes questions within the selected chat", jump and jump.label._text ~= "" and text("В этом разговоре"))
jump:Fire("OnClick")
check("choosing a question returns to the same conversation", not jump:IsVisible() and T.Window.Current() == "019a7c2e-0002")

print(string.format("ok %d checks", passed))
