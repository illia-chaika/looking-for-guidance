local _, T = ...

local Kit, L = T.Kit, T.L
local Composer = {}
T.Composer = Composer

local LINES = 6

local function formatTokens(n)
    local s = tostring(math.floor(n)):reverse():gsub("(%d%d%d)", "%1 ")
    return (s:reverse():gsub("^ ", ""))
end

local function addMeter(frame, choice)
    local meter = Kit.Link(frame, "t-meta", "ink-2", "gold")
    meter:SetPoint("RIGHT", choice, "LEFT", -8, 0)
    local ring = CreateFrame("Frame", nil, meter)
    ring:SetSize(13, 13)
    ring:SetPoint("LEFT")
    local marks = {}
    for i = 1, 12 do
        local angle = (i - 1) * math.pi / 6
        local mark = ring:CreateTexture(nil, "ARTWORK")
        mark:SetSize(2.2, 2.2)
        mark:SetPoint("CENTER", 5 * math.sin(angle), 5 * math.cos(angle))
        marks[i] = mark
    end
    meter.text:SetPoint("LEFT", ring, "RIGHT", 4, 0)
    local tip = Kit.Popover(meter)
    tip:SetPoint("BOTTOMRIGHT", meter, "TOPRIGHT", 0, 6)
    tip:SetWidth(244)
    local title = Kit.Text(tip, "t-ui-strong", "gold")
    title:SetPoint("TOPLEFT", 11, -10)
    T.BindLabel(title, "Контекст диалога")
    local labels, numbers = {}, {}
    for i, label in ipairs({ "Использовано", "Осталось", "Окно модели" }) do
        labels[i] = Kit.Text(tip, "t-ui", "ink-2")
        labels[i]:SetPoint("TOPLEFT", 11, -35 - (i - 1) * 19)
        T.BindLabel(labels[i], label)
        numbers[i] = Kit.Text(tip, "t-ui", "ink")
        numbers[i]:SetPoint("TOPRIGHT", -11, -35 - (i - 1) * 19)
    end
    local note = Kit.Text(tip, "t-meta", "ink-3")
    note:SetPoint("TOPLEFT", 11, -98)
    note:SetWidth(222)
    note:SetJustifyH("LEFT")
    note:SetWordWrap(true)
    local adviser = Kit.Text(tip, "t-ui", "ink-2")
    local limitLabels, limitValues = {}, {}
    for i = 1, 3 do
        limitLabels[i] = Kit.Text(tip, "t-ui", "ink-2")
        limitValues[i] = Kit.Text(tip, "t-ui", "ink")
    end
    -- The adviser this chat talks to and what is left of its allowance, where its provider reports that.
    -- A window past its reset time is out of date and is left out.
    local function allowance(thread)
        local agent = thread and thread.agent or T.Store.agent
        local rows = {}
        if agent and agent == T.Store.agent then
            for _, limit in ipairs(T.Store.limits or {}) do
                local mins, left, resets = tonumber(limit.mins), tonumber(limit.left), tonumber(limit.resets)
                if mins and left and #rows < 3 and not (resets and resets < time()) then
                    rows[#rows + 1] = { mins < 1440 and L("Лимит на {1} ч", math.floor(mins / 60 + .5))
                        or L("Лимит на {1} дн", math.floor(mins / 1440 + .5)), L("осталось {1}%", left) }
                end
            end
        end
        return agent and (agent == "claude" and "Claude Code" or "Codex"), rows
    end
    function meter:SetUsage(thread)
        local pct = thread and tonumber(thread.contextLeft)
        local used = thread and tonumber(thread.contextUsed)
        local window = thread and tonumber(thread.contextWindow)
        local locale = T.GetUILocale()
        local name, rows = allowance(thread)
        local shown = name or ""
        for _, row in ipairs(rows) do shown = shown .. "\n" .. row[1] .. row[2] end
        if self.hasDrawn and self.pct == pct and self.used == used and self.window == window and self.locale == locale
            and self.shown == shown then return end
        self.locale, self.shown = locale, shown
        self.hasDrawn, self.pct, self.used, self.window = true, pct, used, window
        local measured = used and used >= 0 and window and window > 0
        for i = 1, #marks do
            Kit.Paint(marks[i], self.pct and i <= math.floor(self.pct * 12 / 100 + 0.5) and "accent" or "line")
        end
        for i = 1, #labels do labels[i]:SetShown(not not measured) numbers[i]:SetShown(not not measured) end
        note:ClearAllPoints()
        note:SetPoint("TOPLEFT", 11, measured and -100 or -33)
        if measured then
            numbers[1]:SetText(formatTokens(used))
            numbers[2]:SetText(formatTokens(math.max(0, window - used)))
            numbers[3]:SetText(formatTokens(window))
            note:SetText(L("Токены. Процент учитывает служебный резерв Codex."))
        elseif self.pct ~= nil then
            note:SetText(L("Свободно {1}%. Точное число токенов недоступно.", self.pct))
        else
            note:SetText(L("Замер появится, когда советник сообщит расход токенов."))
        end
        local y = (measured and 100 or 33) + Kit.TextHeight(note) + 8
        adviser:SetShown(name ~= nil)
        if name then
            adviser:ClearAllPoints()
            adviser:SetPoint("TOPLEFT", 11, -y)
            adviser:SetText(L("Советник: {1}", name))
            y = y + 19
        end
        for i = 1, #limitLabels do
            local row = rows[i]
            limitLabels[i]:SetShown(row ~= nil)
            limitValues[i]:SetShown(row ~= nil)
            if row then
                limitLabels[i]:ClearAllPoints()
                limitLabels[i]:SetPoint("TOPLEFT", 11, -y)
                limitLabels[i]:SetText(row[1])
                limitValues[i]:ClearAllPoints()
                limitValues[i]:SetPoint("TOPRIGHT", -11, -y)
                limitValues[i]:SetText(row[2])
                y = y + 19
            end
        end
        tip:SetHeight(y + 2)
    end
    meter:HookScript("OnEnter", function() tip:Show() end)
    meter:HookScript("OnLeave", function() if not tip.pinned then tip:Hide() end end)
    meter:SetScript("OnClick", function()
        if tip.pinned then tip:Hide() else tip:Show() tip.pinned = true end
    end)
    tip:HookScript("OnHide", function(self) self.pinned = false end)
    meter:HookScript("OnHide", function() tip:Hide() end)
    meter.tip, meter.numbers, meter.note = tip, numbers, note
    meter.adviser, meter.limitLabels, meter.limitValues = adviser, limitLabels, limitValues
    return meter
end

function Composer.Build(parent, handlers)
    local frame = CreateFrame("Frame", nil, parent)
    frame.fill = Kit.Fill(frame, "surface-1")
    Kit.Hairline(frame, "edge", "TOP")
    Kit.Hairline(frame, "bevel-light", "TOP", { pixels = 1 })
    local pad = Kit.space["space-3"]
    local body = T.Tokens.font["t-body"]
    local inset = Kit.space["space-2"]

    local field = Kit.Recess(CreateFrame("Frame", nil, frame))
    field:SetPoint("TOPLEFT", pad, -(1 + pad))
    field:SetPoint("TOPRIGHT", -pad, -(1 + pad))

    local send = Kit.Key(field, nil, true)
    send:SetGlyph("send")
    send:SetPoint("BOTTOMRIGHT", -(Kit.space["space-1"] + 1), Kit.space["space-1"] + 1)
    send:Disable()

    local scroll = CreateFrame("ScrollFrame", nil, field)
    scroll:SetPoint("TOPLEFT", pad + 1, -inset)
    scroll:SetPoint("BOTTOMRIGHT", field, "BOTTOMRIGHT", -(send:GetWidth() + 2 * inset), inset)
    local edit = CreateFrame("EditBox", nil, scroll)
    edit:SetMultiLine(true)
    edit:SetAutoFocus(false)
    edit:SetFontObject(Kit.Font("t-body"))
    edit:SetTextColor(Kit.Color("ink"))
    edit:SetJustifyH("LEFT")
    edit:SetJustifyV("TOP")
    edit:SetTextInsets(1, 1, 1, 1)
    edit:SetSize(100, body.line + 4)
    scroll:SetScrollChild(edit)
    local function focusDraft() edit:SetFocus() end
    field:EnableMouse(true)
    field:SetScript("OnMouseDown", focusDraft)
    scroll:EnableMouse(true)
    scroll:SetScript("OnMouseDown", focusDraft)

    local hintLayer = CreateFrame("Frame", nil, field)
    hintLayer:SetAllPoints(scroll)
    hintLayer:SetFrameLevel(scroll:GetFrameLevel() + 2)
    hintLayer:EnableMouse(false)
    local placeholder = Kit.Text(hintLayer, "t-body", "ink-3")
    placeholder:SetPoint("TOPLEFT", scroll, "TOPLEFT", 1, -1)
    placeholder:SetJustifyH("LEFT")
    placeholder:SetJustifyV("TOP")
    placeholder:SetWordWrap(false)
    local measure = Kit.Text(field, "t-body")
    measure:SetWordWrap(true)
    measure:Hide()

    local linkHint = Kit.Text(frame, "t-meta", "ink-2")
    linkHint:SetPoint("TOPLEFT", field, "BOTTOMLEFT", 1, -3)
    T.BindLabel(linkHint, "Shift + щелчок вставляет игровую ссылку")
    linkHint:SetWordWrap(false)
    linkHint:SetPoint("RIGHT", field, "RIGHT", -1, 0)
    linkHint:Hide()

    local context = Kit.Link(frame, "t-meta", "ink-3", "ink-2")
    context:SetPoint("BOTTOMLEFT", pad, Kit.space["space-2"])
    context.mark = context:CreateTexture(nil, "ARTWORK")
    context.mark:SetSize(Kit.space["space-1"], Kit.space["space-1"])
    context.mark:SetPoint("LEFT")
    context.text:SetPoint("LEFT", context.mark, "RIGHT", Kit.space["space-2"], 0)
    context:SetScript("OnClick", handlers.context)

    local choice = Kit.Link(frame, "t-meta", "ink-3", "ink-2", "down")
    choice:SetPoint("BOTTOMRIGHT", -pad, Kit.space["space-2"])
    local meter = addMeter(frame, choice)
    local picker = T.ModelPicker.Build(choice)
    choice:SetScript("OnClick", function()
        if handlers.choice then handlers.choice() end
        meter.tip:Hide()
        picker:Toggle()
    end)
    choice:HookScript("OnEnter", function(self)
        if picker:IsShown() then return end
        local pick = T.Actions.Choice(frame.thread)
        GameTooltip:SetOwner(self, "ANCHOR_TOP")
        GameTooltip:ClearLines()
        if frame.thread then
            GameTooltip:AddLine(L("Этот чат"))
            GameTooltip:AddLine(choice.text:GetText(), 1, 0.9, 0.72)
        end
        GameTooltip:AddLine(L(frame.thread and "Для следующего ответа" or "Для нового чата"))
        local model = pick.model
        GameTooltip:AddLine(model and T.Format.Escape((model.label or model.id) .. (pick.effort and " · " .. pick.effort or "")) or L("Список пока недоступен"), 1, 0.9, 0.72)
        GameTooltip:Show()
    end)
    choice:HookScript("OnLeave", function(self) if GameTooltip:IsOwned(self) then GameTooltip:Hide() end end)
    picker:HookScript("OnShow", function() if GameTooltip:IsOwned(choice) then GameTooltip:Hide() end end)
    local function fit()
        local available = math.max(1, frame:GetWidth() - 2 * pad)
        local full = available >= 385
        local pct = meter.pct
        meter.text:SetText(pct and (full and L("Свободно {1}%", pct) or pct .. "%") or (full and L("Контекст —") or "—"))
        local usageWidth = math.min(available * 0.4, meter.text:GetUnboundedStringWidth() + 19)
        meter:SetWidth(math.max(1, usageWidth))
        local model = choice:IsShown() and math.min(choice.text:GetUnboundedStringWidth() + 16, available * 0.57) or 0
        choice:SetWidth(math.max(1, model))
        local left = available - model - usageWidth - 16
        context:SetShown(left >= 24)
        context:SetWidth(math.max(1, math.min(left, context.text:GetUnboundedStringWidth() + 12)))
    end
    Kit.Tip(context, context.text)
    frame:SetScript("OnSizeChanged", fit)

    local refit, cursorTop, cursorHeight = true, 0, body.line
    local function resize()
        local width = scroll:GetWidth()
        if width and width > 2 then
            if edit:GetWidth() ~= width then edit:SetWidth(width) end
            measure:SetWidth(width - 2)
            placeholder:SetWidth(width - 2)
        end
        local value = edit:GetText() or ""
        -- Match the editable content, including native hyperlinks and composition.
        local font = T.FontFile(body.file, value)
        if measure.fontFile ~= font or measure.fontSize ~= body.size or measure.line ~= body.line then
            measure:SetFont(font, body.size, "")
            measure:SetSpacing(body.line - body.size)
            measure.fontFile, measure.fontSize, measure.line = font, body.size, body.line
        end
        measure:SetText(value .. (value:sub(-1) == "\n" and " " or ""))
        local _, breaks = value:gsub("\n", "")
        local lines = math.max(breaks + 1, edit.GetNumLines and edit:GetNumLines() or 0)
        local height = math.max(body.line * lines, math.ceil(measure:GetStringHeight())) + 4
        if edit:GetHeight() ~= height then edit:SetHeight(height) end
        field:SetHeight(math.min(LINES * body.line + 4, height) + 2 * inset)
        frame:SetHeight(1 + pad + field:GetHeight() + 2 * Kit.space["space-2"] + Kit.space["space-4"] + (linkHint:IsShown() and 18 or 0))
    end
    scroll:SetScript("OnSizeChanged", function() refit = true end)
    scroll:HookScript("OnShow", function() refit = true end)
    local drafts, currentKey, character = {}, nil, nil
    local function bindCharacter()
        local guid = T.CharacterIdentity and T.CharacterIdentity()
        if not guid or guid == character then return false end
        local books = T.settings.characterDrafts
        if type(books) ~= "table" then books = {}; T.settings.characterDrafts = books end
        local saved = books[guid]
        if type(saved) ~= "table" then saved = {}; books[guid] = saved end
        if not character then
            -- A draft may be typed while the player's GUID is still loading.
            for key, value in pairs(drafts) do
                if not saved[key] then saved[key] = value
                elseif saved[key] ~= value and not saved.offline then saved.offline = value end
            end
        end
        drafts, character = saved, guid
        return true
    end
    local function showDraft(key)
        currentKey = key
        local value = type(drafts[key]) == "string" and drafts[key] or ""
        if edit:GetText() ~= value then edit:SetText(value); edit:SetCursorPosition(#value) end
    end
    function frame:SwitchDraft(key)
        local rebound = bindCharacter()
        if key == currentKey and not rebound then return end
        if key ~= "offline" and drafts.offline and not drafts[key] and
            (currentKey == "offline" or not currentKey or key == "new") then
            drafts[key], drafts.offline = drafts.offline, nil
        end
        showDraft(key)
    end
    function frame:AdoptDraft(old, new)
        if old == new then return end
        bindCharacter()
        local value = drafts[old]
        if value and not drafts[new] then drafts[new], drafts[old] = value, nil end
        if currentKey == old or currentKey == new then showDraft(new) end
    end
    frame:RegisterEvent("PLAYER_ENTERING_WORLD")
    frame:RegisterEvent("PLAYER_LOGOUT")
    frame:SetScript("OnEvent", function()
        if currentKey then frame:SwitchDraft(currentKey) else bindCharacter() end
    end)

    local function submit()
        local text, refs = strtrim(edit:GetText()), nil
        if T.GameLinks then text, refs = T.GameLinks.Prepare(text, edit) end
        if not frame.canSend or text == "" then return end
        local bank, sentKey = drafts, currentKey
        local accepted = handlers.send(text, refs)
        if accepted then
            if sentKey then bank[sentKey] = nil end
            if currentKey == sentKey then edit:SetText("") end
            -- WoW clears keyboard focus on the mouse-down over the Send button.
            -- Restore it here, in the actual submit action, so future answer or
            -- connection callbacks cannot take focus from a different edit box.
            if edit:IsVisible() and GetCurrentKeyBoardFocus and not GetCurrentKeyBoardFocus() then
                edit:SetFocus()
            end
        end
    end

    local focused = false
    local function hint()
        local show = focused and edit:GetText() == ""
        if linkHint:IsShown() ~= show then linkHint:SetShown(show) resize() end
    end

    edit:SetScript("OnTextChanged", function(self)
        if T.ApplyEditFont then T.ApplyEditFont(self, body) end
        if currentKey then drafts[currentKey] = self:GetText() ~= "" and self:GetText() or nil end
        refit = true
        hint()
        placeholder:SetShown(self:GetText() == "")
        send:SetEnabled(frame.canSend and strtrim(self:GetText()) ~= "")
    end)
    edit:SetScript("OnCursorChanged", function(_, _, y, _, height)
        cursorTop, cursorHeight = math.abs(y or 0), height or body.line
        refit = true
    end)
    frame:SetScript("OnUpdate", function()
        if refit then
            refit = false
            resize()
            if scroll.UpdateScrollChildRect then scroll:UpdateScrollChildRect() end
            local offset = scroll:GetVerticalScroll()
            local top = math.min(cursorTop, math.max(offset, cursorTop + cursorHeight - scroll:GetHeight()))
            top = math.max(0, math.min(top, scroll:GetVerticalScrollRange()))
            if edit:GetText() == "" then top = 0 end
            if offset ~= top then scroll:SetVerticalScroll(top) end
        end
    end)
    edit:SetScript("OnEnterPressed", function(self)
        if IsShiftKeyDown() then
            self:Insert("\n")
        else
            submit()
        end
    end)
    edit:SetScript("OnEscapePressed", edit.ClearFocus)
    edit:SetScript("OnEditFocusGained", function()
        focused = true
        hint()
        for _, edge in ipairs(field.edges) do
            Kit.Paint(edge, "accent")
        end
    end)
    edit:SetScript("OnEditFocusLost", function()
        focused = false
        hint()
        for _, edge in ipairs(field.edges) do
            Kit.Paint(edge, "edge")
        end
    end)
    send:SetScript("OnClick", submit)

    function frame:Set(state)
        if self.readSize ~= body.size or self.readLine ~= body.line then
            self.readSize, self.readLine, refit = body.size, body.line, true
            T.ApplyEditFont(edit, body)
            edit:SetSpacing(body.line - body.size)
        end
        self.canSend = not state.disabled
        self.thread = state.thread
        picker.thread = state.thread
        meter:SetUsage(state.thread)
        picker:Sync()
        self:SwitchDraft(self.thread and self.thread.id or (state.offline and "offline" or "new"))
        placeholder:SetText(state.placeholder)
        placeholder:SetShown(edit:GetText() == "")
        send:SetEnabled(self.canSend and strtrim(edit:GetText()) ~= "")
        context.text:SetText(state.context)
        Kit.Paint(context.mark, state.contextOn and "accent" or "line-strong")
        choice:SetShown(state.choice ~= nil)
        if state.choice then
            choice.text:SetText(state.choice)
        end
        fit()
    end

    function frame:Draft(text)
        edit:SetText(text)
        edit:SetFocus()
        edit:SetCursorPosition(#text)
    end

    function frame:Focus()
        edit:SetFocus()
    end

    frame.context, frame.choice, frame.edit = context, choice, edit
    frame.meter, frame.picker = meter, picker
    if T.GameLinks then T.GameLinks.Bind(edit) end
    resize()
    return frame
end
