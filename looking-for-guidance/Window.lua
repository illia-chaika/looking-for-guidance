local _, T = ...

local Kit, Format, Store, Actions, Popovers = T.Kit, T.Format, T.Store, T.Actions, T.Popovers
local Skin, L = T.JournalSkin, T.L
local Window = {}
T.Window = Window

local SIZE, MIN, MAX = { 734, 642 }, { 350, 360 }, { 1024, 900 }
local MINI = { 280, 220, 420 }
local BAR, TABS = 40, 30
local PAD = Kit.space["space-4"]
local SIDE = 200
local PROBLEMS = {
    uninstalled = { "Перезапусти WoW, чтобы загрузить помощника." },
    exhausted = { "Каналы связи исчерпаны. Обнови интерфейс.", true },
    low = { "Осталось мало каналов связи. Скоро понадобится обновить интерфейс.", true },
    unreadable = { "Перезапусти WoW, чтобы загрузить помощника." },
    unencodable = { "Не удалось подготовить вопрос. Измени текст и отправь ещё раз." },
    oversized = { "Вопрос с данными слишком велик. Сократи текст или отключи лишние разделы." },
    staleDelivery = { "Срок подтверждения истёк. Проверь историю перед повторной отправкой вопроса." },
    unready = { "Помощник подключается к Codex. Вопрос останется в поле ввода." },
    journalBusy = { "Дождись окончания текущего ответа и открой совет ещё раз." },
    DISABLED = { "Включи группу Looking for Guidance в списке аддонов WoW.", true },
    noQuest = { "Не удалось открыть задание. Проверь журнал клавишей L." },
    noWaypoint = { "Игра больше не показывает точку для этого задания." },
}
local AWAY = {
    offline = { "Ожидаю подключение", "Открой Looking for Guidance на Mac. Когда помощник подключится, здесь появится игровой чат.", true },
    restart = { "Перезапусти WoW", "Помощник обновляет аддон после выхода из игры.", false, true },
}

local frame, bar, side, well, composer, transcript, list, first, notice, glance, rename
local settingsMenu, preview, nav, history, paper
local tabs, pages = nil, {}
local page = "overview"
local footer, identityLabel, levelLabel
local prevTheme
local selected, screen, folding = nil, "thread", false

local function layout()
    return T.settings.window
end

local function current()
    return screen ~= "new" and Store.Find(selected) or nil
end

local function recentConversation()
    local guid = T.CharacterIdentity()
    local saved = guid and (layout().characterThreads or {})[guid] or layout().lastThread
    local thread = Store.Find(saved)
    if thread then return thread end
    -- The sidebar sorts errors and approvals first. They are not necessarily
    -- the conversation the player most recently used.
    for _, candidate in ipairs(Store.Threads()) do
        if not thread or (candidate.updated or 0) > (thread.updated or 0) then thread = candidate end
    end
    return thread
end

local function rememberChat(id)
    if not id or id:find("^local:") then return end
    layout().lastThread = id
    local guid = T.CharacterIdentity()
    if guid then
        layout().characterThreads = layout().characterThreads or {}
        layout().characterThreads[guid] = id
    end
end

local function online()
    return Store.received and not AWAY[Store.codex]
end

local function title()
    if not online() or layout().mini then
        return L("Помощник")
    elseif screen == "new" then
        return L("Новый чат")
    end
    local thread = current()
    return thread and Store.Title(thread) or L("Новый чат")
end

local function modelAndEffort(thread)
    local label = thread.model
    for _, model in ipairs(Store.models) do
        if model.id == thread.model then
            label = model.label
        end
    end
    local parts = {}
    for _, part in ipairs({ label or "", thread.effort or "" }) do
        if part ~= "" then
            table.insert(parts, Format.Escape(part))
        end
    end
    return table.concat(parts, " · ")
end

local function applyScale()
    frame:SetScale(T.settings.scale)
    Kit.Snap()
end

local function applyOpacity()
    for _, region in ipairs({ bar, composer, side, glance }) do
        region.fill:SetAlpha(T.settings.opacity)
    end
    bar.grain:SetAlpha(T.settings.opacity)
    side.grain:SetAlpha(0.75 * T.settings.opacity)
    -- Preserve a solid-enough reading surface even over bright game scenery.
    local opacity = 0.75 + T.settings.opacity * 0.25
    well.fill:SetAlpha(opacity)
    paper:SetAlpha(0.86 * opacity)
    paper:SetShown(T.settings.appearance == "journal")
end

local function save(resized)
    local point, _, relative, x, y = frame:GetPoint(1)
    local saved = layout()
    saved.point, saved.relative, saved.x, saved.y = point, relative, x, y
    if saved.mini then
        saved.miniWidth = frame:GetWidth()
    elseif resized then
        saved.width, saved.height = frame:GetWidth(), frame:GetHeight()
    end
end

local function fullWidth()
    local width = layout().width or (T.settings.appearance == "tracker" and 355 or page == "chat" and SIZE[1] or 566)
    return math.max(MIN[1], math.min(MAX[1], width))
end

local function dock()
    local open = page == "chat" and layout().sidebar
    local overlay = open and frame:GetWidth() < SIDE + 330
    if open and not overlay then
        well:SetPoint("TOPLEFT", side, "TOPRIGHT")
        composer:SetPoint("BOTTOMLEFT", side, "BOTTOMRIGHT")
    else
        well:SetPoint("TOPLEFT", tabs, "BOTTOMLEFT")
        composer:SetPoint("BOTTOMLEFT", frame, "BOTTOMLEFT", 1, 1)
    end
    side:SetFrameLevel(frame:GetFrameLevel() + (overlay and 12 or 2))
    side:SetShown(open and not layout().mini)
    frame:SetResizeBounds(MIN[1], MIN[2], MAX[1], MAX[2])
    bar.side:SetHeld(open)
    bar.side.pane:SetShown(open)
    side.overlay = overlay
end

local function applyMini()
    local mini = layout().mini
    local chatting = page == "chat"
    well:SetShown(chatting and not mini)
    side:SetShown(chatting and layout().sidebar and not mini)
    composer:SetShown(chatting and not mini and Store.codex ~= "restart")
    glance:SetShown(mini)
    tabs:SetShown(not mini)
    for key, body in pairs(pages) do body:SetShown(not mini and page == key) end
    for _, tool in ipairs({ bar.gear, bar.divider }) do tool:SetShown(not mini) end
    bar.book:SetShown(not mini)
    if mini then frame:SetResizeBounds(MINI[2], BAR + 2, MINI[3], MAX[2])
    else dock() end
    frame.grip:SetAlpha(mini and 0.6 or 1)
    bar.mini.glyph:SetGlyph(mini and "restore" or "minimize")
end

local function size()
    if layout().mini then
        frame:SetSize(layout().miniWidth or MINI[1], BAR + 2)
    else
        frame:SetSize(fullWidth(), layout().height or SIZE[2])
    end
end

local function pin(corner)
    local x, top = corner == "TOPLEFT" and frame:GetLeft() or frame:GetRight(), frame:GetTop()
    frame:ClearAllPoints()
    frame:SetPoint(corner, UIParent, "BOTTOMLEFT", x, top)
end

local function setMini(mini)
    pin("TOPLEFT")
    save()
    layout().mini = mini
    if mini and settingsMenu then settingsMenu:Hide() composer.picker:Hide() history:Hide() end
    local fromWidth, fromHeight = frame:GetSize()
    local toWidth, toHeight
    if mini then
        toWidth, toHeight = layout().miniWidth or MINI[1], BAR + 2 + glance:Render()
        glance:Hide()
        for _, region in ipairs({ well, side, composer }) do
            Kit.Leave(region)
        end
    else
        toWidth, toHeight = fullWidth(), layout().height or SIZE[2]
        Kit.Leave(glance)
    end
    folding = true
    Kit.Tween(frame, Kit.motion[mini and "motion-2" or "motion-3"], function(progress)
        frame:SetSize(fromWidth + (toWidth - fromWidth) * progress, fromHeight + (toHeight - fromHeight) * progress)
    end, function()
        folding = false
        applyMini()
        size()
        Window.Refresh()
        if mini then
            glance:Reveal()
        else
            for _, region in ipairs({ side, well, composer }) do
                if region:IsShown() then
                    Kit.Rise(region)
                end
            end
        end
    end)
end

local function restore(id)
    setMini(false)
    Window.Select(id)
end

local function startRename()
    local thread = current()
    if layout().mini or not online() or not thread or thread.id:find("^local:") then
        return
    end
    rename.id = thread.id
    rename:SetText(Store.Title(thread))
    rename:Show()
    nav.title:Hide()
    Window.Refresh()
    rename:SetFocus()
    rename:HighlightText()
end

local function finishRename(commit)
    if not rename:IsShown() then
        return
    end
    rename:Hide()
    nav.title:Show()
    rename:ClearFocus()
    local thread = Store.Find(rename.id)
    local text = strtrim(rename:GetText())
    if commit and thread and text ~= "" and text ~= Store.Title(thread) then
        Actions.Rename(thread, text)
    end
    Window.Refresh()
end

function Window.Refresh()
    if not frame or not frame:IsShown() then
        return
    end
    local now = time()
    local thread = current()
    if screen == "thread" and not thread then
        thread = recentConversation()
        selected = thread and thread.id
    end
    local live = online()
    local mini = layout().mini
    if rename:IsShown() and (mini or rename.id ~= Window.Current()) then
        finishRename(true)
    end
    local chatting = page == "chat"
    local expanded = not mini and not folding
    tabs:SetShown(expanded)
    if footer then
        footer:SetShown(expanded and not chatting)
        if expanded and not chatting then
            local label = {}
            if T.settings.extras and T.settings.context.character then label[1] = UnitName("player") end
            if T.settings.extras and T.settings.context.location then table.insert(label, GetZoneText()) end
            T.SetContent(identityLabel, Format.Escape(table.concat(label, " · ")))
            local class = T.settings.extras and T.settings.context.character and UnitClass("player")
            T.SetContent(levelLabel, class and (Format.Escape(class) .. " · " .. L("Уровень {1}", UnitLevel("player"))) or "")
        end
    end
    for key, tab in pairs(tabs.buttons) do tab:SetActive(key == page) end
    for key, body in pairs(pages) do
        local active = expanded and key == page
        if body:IsShown() ~= active then body:SetShown(active)
        elseif active then body:Refresh() end
    end
    well:SetShown(expanded and chatting)
    side:SetShown(expanded and chatting and layout().sidebar)
    bar.title:SetFontObject(Kit.Font(mini and "t-label" or "t-title"))
    bar.title:SetText(L(mini and "Помощник" or "Полевой журнал"))
    T.SetContent(nav.title, Format.Escape(title()))
    nav:SetShown(expanded and chatting and live)
    bar.mark:SetShown(mini)
    bar.spinner:SetShown(expanded and chatting and thread ~= nil and Store.Rank(thread) == 1)
    local waiting = Store.Waiting(chatting and thread and thread.id)
    bar.badge:SetShown(expanded and live and waiting > 0 and not side:IsShown())
    bar.badge.text:SetText(waiting)
    bar.badge:SetWidth(math.max(Kit.space["space-4"], bar.badge.text:GetUnboundedStringWidth() + 2 * Kit.space["space-1"]))
    bar.new:SetShown(expanded and chatting and live and not layout().sidebar)
    if mini and not folding then
        frame:SetHeight(BAR + 2 + glance:Render())
    end

    if live and Store.codex ~= "ready" and Store.note ~= "" then
        notice:Set(Format.Escape(Store.note), false, Store.codex == "error" and "status" or "setup")
    elseif notice.kind == "setup" or notice.kind == "status" then
        notice:Hide()
    end
    first:SetShown(not live or not thread or screen == "new")
    if live and (not thread or screen == "new") then
        first.heading:SetText(L("Что обсудим?"))
        first.body:SetText("")
        first.heading:SetPoint("BOTTOM", first.body, "TOP", 0, 12)
        first.retry:Hide()
        first.warning:Hide()
    elseif not live then
        local away = AWAY[Store.codex] or AWAY.offline
        first.heading:SetText(L(away[1]))
        first.body:SetText(L(away[2]))
        first.retry:SetShown(away[3])
        first.warning:SetShown(away[4])
        first.heading:SetPoint("BOTTOM", first.body, "TOP", away[4] and (first.warning:GetWidth() + Kit.space["space-2"]) / 2 or 0, Kit.space["space-3"])
    end
    transcript:SetShown(chatting and expanded and live and screen == "thread" and thread ~= nil)
    composer:SetShown(chatting and expanded and Store.codex ~= "restart")

    if side:IsShown() then
        list:Render(Window.Current(), now)
    end
    if transcript:IsShown() then
        transcript:Render(thread, now)
        Actions.Read(thread)
    end
    if composer:IsShown() then
        local pick = Actions.Choice(thread)
        local enabled = false
        for _, part in ipairs(T.Context.parts) do
            enabled = enabled or T.settings.context[part.key]
        end
        local localThread = thread and thread.id:match("^local:")
        local busy = thread and (Store.Rank(thread) == 1 or Store.Delivering(thread))
        local placeholder = not live and "Черновик до подключения…" or busy and "Подготовь следующий вопрос…"
            or localThread and "Повтори отправку первого вопроса выше"
            or thread and thread.state == "error" and "Уточни вопрос или повтори" or "Спроси про игру…"
        composer:Set({
            offline = not live,
            placeholder = L(placeholder),
            disabled = not live or localThread or busy or not thread and (not pick.model or not pick.project),
            context = T.Context.Summary(),
            contextOn = enabled and T.settings.extras,
            thread = thread,
            choice = thread and not pick.overridden and modelAndEffort(thread)
                or pick.model and Format.Escape((pick.model.label or pick.model.id) .. (pick.effort and " · " .. pick.effort or ""))
                or L("Модель…"),
        })
    end
    if history:IsShown() then history:Fill(thread) end
end

function Window.SetSidebar(open)
    layout().sidebar = open
    dock()
    applyMini()
    save()
    Window.Refresh()
    if side:IsShown() then
        list:Reveal()
    end
end

local function newChat()
    composer.picker:Hide()
    history:Hide()
    page, screen, selected = "chat", "new", nil
    layout().page = page
    if not layout().width and not layout().mini then size() end
    composer:SwitchDraft("new")
    if side.overlay then layout().sidebar = false end
    applyMini()
    Window.Refresh()
    composer:Focus()
end

function Window.OpenPage(key)
    if key ~= "chat" and not pages[key] then return end
    if rename:IsShown() then finishRename(true) end
    if key ~= "chat" then composer.edit:ClearFocus() end
    composer.picker:Hide()
    history:Hide()
    page = key
    layout().page = key
    if not layout().width and not layout().mini then size() end
    applyMini()
    Window.Refresh()
end

function Window.Discuss(text, refs, topic, cardTitle)
    if type(text) ~= "string" or text == "" then return end
    if not online() or Store.codex ~= "ready" then
        T.Fire("Problem", "unready")
        return
    end
    local thread = current()
    if thread and (thread.id:match("^local:") or Store.Delivering(thread) or Store.Rank(thread) == 1 or thread.approval) then
        T.Fire("Problem", "journalBusy")
        return
    end
    local attached = {}
    for _, link in ipairs(refs or {}) do
        if type(link) == "string" then
            local _, references = T.GameLinks.Prepare(link)
            for _, ref in ipairs(references) do attached[#attached+1] = ref end
        end
    end
    local title = cardTitle or L("Полевой журнал")
    local card = {topic = topic or "route", title = title, facts = text, locale = T.GetUILocale()}
    if thread then
        if Actions.Reply(thread, title, attached, card) then Window.Select(thread.id) end
    else
        local task = Actions.Ask(title, attached, card)
        if task then
            composer:AdoptDraft("new", task.id)
            Window.Select(task.id)
        end
    end
end

function Window.Select(id)
    if composer then composer:SwitchDraft(id or "new") end
    page = "chat"
    layout().page = page
    if not layout().width and not layout().mini then size() end
    if side and side.overlay then layout().sidebar = false end
    if tabs then applyMini() end
    if composer then composer.picker:Hide() end
    if history then history:Hide() end
    selected, screen = id, "thread"
    rememberChat(id)
    Window.Refresh()
end

function Window.Show(id)
    if id then
        Window.Select(id)
    end
    if not frame:IsShown() then
        frame:Show()
        Kit.Rise(frame)
    end
    T.Sync.Fresh()
    Window.Refresh()
end

function Window.Toggle()
    if frame:IsShown() then
        frame:Hide()
    else
        Window.Show()
    end
end

function Window.OpenAdvice()
    if layout().mini then setMini(false) end
    Window.Show()
    Window.OpenPage("overview")
    pages.overview:OpenAdvice()
end

function Window.IsShown()
    return frame ~= nil and frame:IsShown()
end

function Window.Current()
    return page == "chat" and screen == "thread" and selected or nil
end

local function buildBar()
    bar = CreateFrame("Frame", nil, frame)
    bar:SetPoint("TOPLEFT", 1, -1)
    bar:SetPoint("TOPRIGHT", -1, -1)
    bar:SetHeight(BAR)
    bar.fill = Kit.Fill(bar, "surface-1")
    bar.grain = Skin.Surface(bar, "Title")
    bar.book = Skin.Icon(bar, "Book", 42)
    bar.book:SetPoint("LEFT", 1, 0)
    Kit.Hairline(bar, "bevel-light", "TOP")
    Kit.Hairline(bar, "edge", "BOTTOM")
    bar:EnableMouse(true)
    bar:RegisterForDrag("LeftButton")
    bar:SetScript("OnDragStart", function() frame:StartMoving() end)
    bar:SetScript("OnDragStop", function()
        frame:StopMovingOrSizing()
        save()
    end)

    bar.close = Kit.Tool(bar, "close", function() frame:Hide() end)
    bar.close:SetPoint("RIGHT", -Kit.space["space-1"], 0)
    bar.mini = Kit.Tool(bar, "minimize", function()
        setMini(not layout().mini)
        Window.Refresh()
    end)
    bar.mini:SetPoint("RIGHT", bar.close, "LEFT")
    bar.divider = CreateFrame("Frame", nil, bar)
    bar.divider:SetSize(2 * Kit.space["space-1"] + 1, Kit.space["space-3"])
    bar.divider:SetPoint("RIGHT", bar.mini, "LEFT")
    Kit.Hairline(bar.divider, "line", "LEFT", { offset = Kit.space["space-1"] })
    bar.gear = Kit.Tool(bar, "gear", function()
        composer.picker:Hide()
        history:Hide()
        settingsMenu:Toggle("display")
    end)
    bar.gear:SetPoint("RIGHT", bar.divider, "LEFT")
    bar.heading = CreateFrame("Button", nil, bar)
    bar.heading:RegisterForDrag("LeftButton")
    bar.heading:SetScript("OnDragStart", bar:GetScript("OnDragStart"))
    bar.heading:SetScript("OnDragStop", bar:GetScript("OnDragStop"))
    bar.spinner = Kit.Spinner(bar.heading)
    bar.spinner:SetPoint("LEFT")
    bar.mark = Kit.Mark(bar.heading, "mark-foot")
    bar.mark:SetPoint("LEFT")
    bar.title = Kit.Text(bar.heading, "t-title", "gold")
    bar.heading:SetPoint("TOPLEFT", bar, "TOPLEFT", 46, 0)
    bar.heading:SetPoint("BOTTOMRIGHT", bar.gear, "BOTTOMLEFT", -8, 0)
    bar.title:SetPoint("CENTER")
    bar.title:SetJustifyH("CENTER")
    bar.title:SetWordWrap(false)

    bar.badge = CreateFrame("Frame", nil, bar)
    bar.badge:SetHeight(Kit.space["space-4"])
    bar.badge:SetPoint("LEFT", bar.heading, "RIGHT", Kit.space["space-2"], 0)
    Kit.Fill(bar.badge, "agent-needs")
    Kit.Edge(bar.badge)
    bar.badge.text = Kit.Text(bar.badge, "t-num", "bg")
    bar.badge.text:SetPoint("CENTER")
    Kit.Tip(bar.heading, bar.title)

end

local function buildNavigation()
    tabs = CreateFrame("Frame", nil, frame)
    tabs:SetPoint("TOPLEFT", bar, "BOTTOMLEFT")
    tabs:SetPoint("TOPRIGHT", bar, "BOTTOMRIGHT")
    tabs:SetHeight(TABS)
    tabs.fill = Kit.Fill(tabs, "well")
    tabs.buttons = {}
    local previous
    for _, entry in ipairs({{"overview", "Обзор"}, {"chat", "Чат"}, {"memory", "Память"}}) do
        local key = entry[1]
        local tab = Skin.Tab(tabs, entry[2], function() Window.OpenPage(key) end)
        tab:SetHeight(TABS - 4)
        if previous then tab:SetPoint("LEFT", previous, "RIGHT", 2, 0)
        else tab:SetPoint("LEFT", tabs, "LEFT", 3, 0) end
        tabs.buttons[key] = tab
        previous = tab
    end
    local function fit(_, width)
        for _, tab in pairs(tabs.buttons) do tab:SetWidth(math.max(1, (width - 10)/3)) end
    end
    tabs:SetScript("OnSizeChanged", fit)
    fit(tabs, tabs:GetWidth())
    local dot = tabs.buttons.overview:CreateTexture(nil, "OVERLAY")
    dot:SetSize(6, 6)
    dot:SetPoint("RIGHT", -8, 0)
    Kit.Paint(dot, "gold")
    dot:Hide()
    T.On("JournalAdviceChanged", function(count) dot:SetShown(count > 0) end)
end

local function buildRename(heading)
    heading:SetScript("OnDoubleClick", startRename)
    rename = CreateFrame("EditBox", nil, heading)
    rename:SetHeight(Kit.size.control)
    rename:SetPoint("LEFT", heading, "LEFT", -Kit.space["space-1"], 0)
    rename:SetPoint("RIGHT", heading, "RIGHT", Kit.space["space-1"], 0)
    rename:SetAutoFocus(false)
    rename:SetFontObject(Kit.Font("t-title"))
    rename:SetTextColor(Kit.Color("ink"))
    rename:SetTextInsets(Kit.space["space-1"], Kit.space["space-1"], 0, 0)
    Kit.Fill(rename, "well")
    for _, edge in ipairs(Kit.Edge(rename)) do
        Kit.Paint(edge, "accent")
    end
    rename:SetScript("OnTextChanged", function(self) T.ApplyEditFont(self, T.Tokens.font["t-title"]) end)
    rename:SetScript("OnEnterPressed", function() finishRename(true) end)
    rename:SetScript("OnEscapePressed", function() finishRename(false) end)
    rename:SetScript("OnEditFocusLost", function() finishRename(true) end)
    rename:Hide()
end

local function buildFirst()
    first = CreateFrame("Frame", nil, well)
    first:SetAllPoints()
    first.body = Kit.Text(first, "t-body", "detail")
    first.body:SetWidth(320)
    first.body:SetJustifyH("CENTER")
    first.body:SetPoint("CENTER", 0, Kit.space["space-2"])
    first.heading = Kit.Text(first, "t-display", "strong")
    first.heading:SetPoint("BOTTOM", first.body, "TOP", 0, Kit.space["space-3"])
    first.warning = Kit.Glyph(first, "warning", "agent-needs")
    first.warning:SetPoint("RIGHT", first.heading, "LEFT", -Kit.space["space-2"], 0)
    first.retry = Kit.Key(first, "Проверить связь")
    first.retry:SetPoint("TOP", first.body, "BOTTOM", 0, -Kit.space["space-3"])
    first.retry:SetScript("OnClick", T.Sync.Check)
end

local function buildNotice()
    notice = CreateFrame("Frame", nil, frame)
    notice:SetPoint("TOPLEFT", tabs, "BOTTOMLEFT")
    notice:SetPoint("TOPRIGHT", tabs, "BOTTOMRIGHT")
    notice:SetHeight(Kit.size["control-lg"] + Kit.space["space-2"])
    notice:SetFrameLevel(well:GetFrameLevel() + 10)
    Kit.Fill(notice, "surface-2")
    Kit.Hairline(notice, "line", "BOTTOM")
    notice.stripe = Kit.Stripe(notice, "agent-needs")
    notice.stripe:SetPoint("TOPLEFT")
    notice.stripe:SetPoint("BOTTOMLEFT")
    notice.warning = Kit.Glyph(notice, "warning", "agent-needs")
    notice.warning:SetPoint("LEFT", PAD, 0)
    notice.text = Kit.Text(notice, "t-ui", "ink")
    notice.close = Kit.Tool(notice, "close", function() notice:Hide() end)
    notice.close:SetPoint("RIGHT", -Kit.space["space-1"], 0)
    notice.action = Kit.Key(notice, "Обновить")
    notice.action:SetPoint("RIGHT", notice.close, "LEFT", -Kit.space["space-1"], 0)
    notice.action:SetScript("OnClick", C_UI.Reload)
    notice.text:SetPoint("RIGHT", notice.action, "LEFT", -Kit.space["space-2"], 0)
    notice.text:SetWordWrap(false)
    function notice:Set(text, reload, kind)
        local asks = kind ~= "status"
        self.kind = kind
        self.stripe:SetShown(asks)
        self.warning:SetShown(asks)
        if asks then
            self.text:SetPoint("LEFT", self.warning, "RIGHT", Kit.space["space-2"], 0)
        else
            self.text:SetPoint("LEFT", PAD, 0)
        end
        self.text:SetText(text)
        self.action:SetShown(reload)
        self:Show()
    end
    notice:SetScript("OnHide", function(self) self.kind = nil end)
    notice:Hide()
end

local function buildGrip()
    frame.grip = CreateFrame("Button", nil, frame)
    frame.grip:SetSize(Kit.space["space-4"], Kit.space["space-4"])
    frame.grip:SetPoint("BOTTOMRIGHT", -1, 1)
    frame.grip:SetFrameLevel(frame:GetFrameLevel() + 30)
    local glyph = Kit.Glyph(frame.grip, "grip")
    glyph:SetPoint("CENTER")
    frame.grip:SetScript("OnEnter", function() glyph:SetInk("ink") end)
    frame.grip:SetScript("OnLeave", function() glyph:SetInk("ink-3") end)
    frame.grip:SetScript("OnMouseDown", function() frame:StartSizing(layout().mini and "RIGHT" or "BOTTOMRIGHT") end)
    frame.grip:SetScript("OnMouseUp", function()
        frame:StopMovingOrSizing()
        save(true)
    end)
end

function Window.Init()
    frame = CreateFrame("Frame", "looking_for_guidance_window", UIParent)
    frame:SetFrameStrata("HIGH")
    frame:SetToplevel(true)
    frame:SetClampedToScreen(true)
    frame:SetMovable(true)
    frame:SetResizable(true)
    frame:SetResizeBounds(MIN[1], MIN[2], MAX[1], MAX[2])
    frame:EnableMouse(true)
    Kit.Edge(frame)
    Kit.GoldFrame(frame)
    table.insert(UISpecialFrames, "looking_for_guidance_window")

    Popovers.Settle()
    Kit.ApplyReading()
    prevTheme = T.settings.appearance
    local saved = layout()
    page = (saved.page == "chat" or saved.page == "memory") and saved.page or "overview"
    size()
    if saved.point then
        frame:SetPoint(saved.point, UIParent, saved.relative, saved.x, saved.y)
    else
        frame:SetPoint("TOPLEFT", UIParent, "TOPLEFT", 28, -36)
    end
    applyScale()

    buildBar()
    buildNavigation()
    composer = T.Composer.Build(frame, {
        send = function(text, refs)
            local thread = current()
            if not thread then
                local task = Actions.Ask(text, refs)
                if task then
                    Window.Select(task.id)
                end
                return task ~= nil
            end
            return thread and Actions.Reply(thread, text, refs) ~= nil
        end,
        context = function()
            composer.picker:Hide()
            history:Hide()
            settingsMenu:Toggle("context")
        end,
        choice = function() history:Hide() settingsMenu:Hide() end,
    })
    side = CreateFrame("Frame", nil, frame)
    side:SetPoint("TOPLEFT", tabs, "BOTTOMLEFT")
    side:SetPoint("BOTTOMLEFT", 1, 1)
    side:SetWidth(SIDE)
    side.fill = Kit.Fill(side, "well")
    side.grain = Skin.Surface(side, "Leather")
    side:EnableMouse(true)
    Kit.Hairline(side, "bevel-shade", "TOP")
    Kit.Hairline(side, "edge", "RIGHT")
    list = T.List.Build(side, Window.Select, newChat, function() Window.SetSidebar(false) end)
    glance = T.Glance.Build(frame, restore)
    glance:SetPoint("TOPLEFT", bar, "BOTTOMLEFT")
    glance:SetPoint("TOPRIGHT", bar, "BOTTOMRIGHT")
    list:SetPoint("TOPLEFT")
    list:SetPoint("BOTTOMRIGHT", -1, 0)

    composer:SetPoint("BOTTOMRIGHT", -1, 1)

    well = CreateFrame("Frame", nil, frame)
    well:SetPoint("TOPRIGHT", tabs, "BOTTOMRIGHT")
    well:SetPoint("BOTTOM", composer, "TOP")
    composer:HookScript("OnHide", function() well:SetPoint("BOTTOM", frame, "BOTTOM", 0, 1) end)
    composer:HookScript("OnShow", function() well:SetPoint("BOTTOM", composer, "TOP") end)
    well.fill = Kit.Fill(well, "page")
    paper = Skin.Surface(well, "Paper")
    Kit.Hairline(well, "bevel-shade", "TOP")
    applyOpacity()

    transcript = T.Transcript.Build(well, {
        refresh = Window.Refresh,
        resend = function(seq) Actions.Resend(current(), seq) end,
        retry = function(thread, question)
            if not question or Store.Delivering(thread) then return end
            local turn = Store.LastQuestionTurn(thread)
            if not turn then return end
            local refs = T.GameLinks.RestoreReferences(turn.references)
            if thread.origin and thread.id == thread.origin then
                local task = Actions.Ask(question, refs, turn.journal, turn.snapshotId)
                if task then Window.Select(task.id) end
            else
                Actions.Reply(thread, question, refs, turn.journal, turn.snapshotId)
            end
        end,
        answer = function(allow) Actions.Answer(current(), allow) end,
        stop = function() Actions.Stop(current()) end,
    })
    nav = CreateFrame("Frame", nil, well)
    nav:SetPoint("TOPLEFT")
    nav:SetPoint("TOPRIGHT")
    nav:SetHeight(32)
    Kit.Fill(nav, "surface-2")
    Skin.Surface(nav, "Chapter")
    bar.side = Kit.Tool(nav, "sidebar", function() Window.SetSidebar(not layout().sidebar) end)
    bar.side:SetPoint("LEFT", 4, 0)
    bar.side.pane = bar.side.glyph:CreateTexture(nil, "OVERLAY")
    bar.side.pane:SetSize(2, 7)
    bar.side.pane:SetPoint("CENTER", bar.side.glyph, "CENTER", -3, 0)
    Kit.Paint(bar.side.pane, "gold")
    bar.new = Kit.Tool(nav, "plus", newChat)
    bar.new:SetPoint("RIGHT", -4, 0)
    Kit.Hairline(nav, "line-strong", "BOTTOM")
    local browse = Kit.Link(nav, "t-ui", "accent", "ink")
    browse:SetLabel("История")
    browse:SetPoint("RIGHT", bar.new, "LEFT", -8, 0)
    nav.heading = CreateFrame("Button", nil, nav)
    nav.heading:SetPoint("TOPLEFT", bar.side, "TOPRIGHT", 5, 0)
    nav.heading:SetPoint("BOTTOMRIGHT", browse, "BOTTOMLEFT", -8, 0)
    nav.title = Kit.Text(nav.heading, "t-ui-strong", "gold")
    nav.title:SetPoint("LEFT")
    nav.title:SetPoint("RIGHT")
    nav.title:SetJustifyH("LEFT")
    nav.title:SetWordWrap(false)
    buildRename(nav.heading)
    transcript:SetPoint("TOPLEFT", nav, "BOTTOMLEFT")
    transcript:SetPoint("BOTTOMRIGHT")
    history = Kit.Panel(CreateFrame("Frame", nil, well), "surface-1")
    history:SetPoint("TOPLEFT", nav, "BOTTOMLEFT")
    history:SetPoint("BOTTOMRIGHT")
    history:SetFrameLevel(well:GetFrameLevel() + 10)
    history:EnableMouse(true)
    local heading = Kit.Text(history, "t-title", "gold")
    heading:SetPoint("TOPLEFT", PAD, -10)
    T.BindLabel(heading, "В этом разговоре")
    local dismiss = Kit.Tool(history, "close", function() history:Hide() end)
    dismiss:SetPoint("TOPRIGHT", -6, -6)
    local entries = Kit.Scroll(history)
    entries:SetPoint("TOPLEFT", PAD, -41)
    entries:SetPoint("BOTTOMRIGHT", -PAD, 12)
    local rows = {}
    local empty = Kit.Text(entries.child, "t-ui", "ink-2")
    empty:SetPoint("TOPLEFT")
    T.BindLabel(empty, "Здесь появятся твои вопросы.")
    function history:Fill(thread)
        local turns = thread and Store.Turns(thread) or {}
        local n, height = 0, 0
        for idx = #turns, 1, -1 do
            if turns[idx].role == "user" then
                n = n + 1
                local row = rows[n]
                if not row then
                    row = CreateFrame("Button", nil, entries.child)
                    row.label = Kit.Text(row, "t-ui", "ink")
                    Kit.Place(row.label, row, 4, 3)
                    Kit.Hairline(row, "line", "BOTTOM")
                    row:SetScript("OnClick", function(button)
                        history:Hide()
                        transcript:JumpTo(button.turnIndex)
                    end)
                    rows[n] = row
                end
                row.turnIndex = idx
                row.label:SetWidth(math.max(20, entries:GetWidth() - 14))
                T.SetContent(row.label, Format.Escape((turns[idx].text or ""):gsub("%]%([%a]+:%d+%)", "]")))
                row:ClearAllPoints()
                row:SetPoint("TOPLEFT", 0, -height)
                row:SetPoint("RIGHT")
                row:SetHeight(10 + Kit.TextHeight(row.label))
                row:Show()
                height = height + row:GetHeight()
            end
        end
        for i = n + 1, #rows do rows[i]:Hide() end
        empty:SetShown(n == 0)
        entries.child:SetHeight(math.max(height, 26))
    end
    history:Hide()
    browse:SetScript("OnClick", function()
        composer.picker:Hide()
        if history:IsShown() then history:Hide() else
            settingsMenu:Hide()
            history:Fill(current())
            history:Show()
            entries:ScrollTo(0, true)
        end
    end)
    buildFirst()
    buildNotice()
    preview = Popovers.Preview(frame, well)
    settingsMenu = Popovers.Settings(bar.gear, frame, {
        apply = function()
            Popovers.Settle()
            Kit.ApplyReading()
            applyOpacity()
            applyScale()
            if prevTheme ~= T.settings.appearance then
                local compact = T.settings.appearance == "tracker"
                if compact or prevTheme == "tracker" then
                    layout().width = compact and (355 + (layout().sidebar and SIDE or 0)) or SIZE[1]
                    layout().height = compact and 374 or SIZE[2]
                    size()
                end
                prevTheme = T.settings.appearance
            end
            T.Alerts.Refresh()
            Window.Refresh()
        end,
        preview = function(draft, sheetOwner) preview:Open(draft, sheetOwner) end,
    })
    local function openMemory() Window.OpenPage("memory") end
    footer = CreateFrame("Frame", nil, frame)
    footer:SetPoint("BOTTOMLEFT", 2, 1)
    footer:SetPoint("BOTTOMRIGHT", -2, 1)
    footer:SetHeight(25)
    Skin.Surface(footer, "Leather")
    Kit.Hairline(footer, "bevel-light", "TOP")
    local portrait = footer:CreateTexture(nil, "ARTWORK")
    portrait:SetSize(19, 19)
    portrait:SetPoint("LEFT", 8, 0)
    if SetPortraitTexture then SetPortraitTexture(portrait, "player") end
    identityLabel = Kit.Text(footer, "t-meta", "ink-2")
    identityLabel:SetPoint("LEFT", portrait, "RIGHT", 5, 0)
    levelLabel = Kit.Text(footer, "t-meta", "ink-2")
    levelLabel:SetPoint("RIGHT", -13, 0)
    pages.overview = T.HubOverview.Create(frame, { discuss = Window.Discuss, memory = openMemory,
        settings = function() settingsMenu:Toggle("notifications") end })
    pages.memory = T.HubMemory.Create(frame)
    for _, body in pairs(pages) do
        body:SetPoint("TOPLEFT", tabs, "BOTTOMLEFT")
        body:SetPoint("BOTTOMRIGHT", footer, "TOPRIGHT", -1, 0)
        body:Hide()
    end
    buildGrip()

    frame:SetScript("OnSizeChanged", function()
        if frame:IsShown() and not folding then
            if not layout().mini then dock() end
            Window.Refresh()
        end
    end)
    frame:HookScript("OnShow", T.Alerts.Refresh)
    frame:HookScript("OnHide", function()
        composer.picker:Hide()
        settingsMenu:Hide()
        history:Hide()
        preview:Hide()
        T.Alerts.Refresh()
    end)
    frame:Hide()
    dock()
    applyMini()

    T.On("StoreChanged", Window.Refresh)
    T.On("LOCALE_CHANGED", function()
        Window.Refresh()
        if frame:IsShown() then applyOpacity() end
    end)
    local loads = CreateFrame("Frame")
    loads:RegisterEvent("ITEM_DATA_LOAD_RESULT")
    loads:RegisterEvent("SPELL_DATA_LOAD_RESULT")
    loads:SetScript("OnEvent", function(_, event, id, success)
        if Format.Loaded((event == "ITEM_DATA_LOAD_RESULT" and "item:" or "spell:") .. id, success) then
            Window.Refresh()
        end
    end)
    T.On("ThreadReplaced", function(old, new)
        composer:AdoptDraft(old, new)
        if selected == old then
            selected = new
            rememberChat(new)
        end
    end)
    T.On("Problem", function(reason)
        local problem = PROBLEMS[reason]
        local message = problem and L(problem[1]) or L("Связь остановлена: {1}", tostring(reason))
        notice:Set(message, not problem or problem[2] == true, "problem")
    end)
    C_Timer.NewTicker(1, function()
        if not frame:IsShown() then
            return
        end
        local now = time()
        if transcript:IsShown() then
            transcript:Tick(now)
        end
        if side:IsShown() then
            list:Render(Window.Current(), now)
        end
    end)
end
