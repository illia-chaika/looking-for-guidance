local _, T = ...

local Kit, Format = T.Kit, T.Format
local Popovers = {}
T.Popovers = Popovers

local function clamp(value, low, high, fallback)
    return math.min(high, math.max(low, tonumber(value) or fallback))
end

function Popovers.Settle()
    local s = T.settings
    s.scale = clamp(s.scale, 0.75, 1.50, 1)
    s.opacity = clamp(s.opacity, 0.5, 1, 1)
    s.fontSize = math.floor(clamp(s.fontSize, 15, 23, 17))
    s.locale = T.LocaleChoice(s.locale)
    s.lineHeight = clamp(s.lineHeight, 1.20, 1.80, 1.45)
    if s.autoAdvice == nil then s.autoAdvice = true end
    if s.research ~= "auto" then s.research = "click" end
    local caps = T.Insights
    s.researchCap = math.floor(clamp(s.researchCap, caps.CAP_MIN, caps.CAP_MAX, caps.CAP_DEFAULT) / 10 + .5) * 10
    if not T.Readers[s.appearance] then s.appearance = "journal" end
end

local tabsSpec = {
    { "display", "Оформление" }, { "language", "Язык" }, { "context", "Данные персонажа" }, { "adviser", "Советник" },
    { "notifications", "Уведомления" },
}
local keys = { "appearance", "scale", "opacity", "fontSize", "lineHeight", "locale", "research", "researchCap", "autoAdvice", "alertChat", "alertSound", "minimap", "context", "extras" }

-- Esc dismisses the top sheet. WoW's CloseSpecialWindows normally closes every
-- registered frame, including the chat below; temporarily substitute our sheet.
local function escapeLayer(under, over)
    for i, name in ipairs(UISpecialFrames) do
        if name == under then UISpecialFrames[i] = over; return end
    end
end

function Popovers.Settings(owner, parent, handlers)
    local veil = CreateFrame("Frame", nil, UIParent)
    veil:SetAllPoints(UIParent)
    veil:SetFrameStrata("FULLSCREEN_DIALOG")
    veil:EnableMouse(true)
    local shade = veil:CreateTexture(nil, "BACKGROUND")
    shade:SetAllPoints()
    shade:SetColorTexture(0.04, 0.03, 0.02, 0.42)
    veil:Hide()

    -- Keep both mouse targets in one frame tree. Reparenting an ordinary
    -- popover under UIParent can leave it below the fullscreen mouse shield.
    local panel = Kit.Panel(CreateFrame("Frame", "looking_for_guidance_settings_panel", veil), "surface-1")
    panel:SetFrameLevel(veil:GetFrameLevel() + 1)
    panel:SetPoint("CENTER", veil, "CENTER")
    panel:EnableMouse(true)
    panel:SetClampedToScreen(true)
    Kit.GoldFrame(panel, 14)
    panel:Hide()
    T.JournalSkin.Surface(panel, "Leather")
    local header = CreateFrame("Frame", nil, panel)
    header:SetPoint("TOPLEFT", 1, -1)
    header:SetPoint("TOPRIGHT", -1, -1)
    header:SetHeight(49)
    T.JournalSkin.Surface(header, "Title")
    local title = Kit.Text(header, "t-display", "gold")
    title:SetPoint("CENTER")
    T.BindLabel(title, "Настройки")
    local close = Kit.Tool(panel, "close", function() panel:Hide() end)
    close:SetPoint("TOPRIGHT", -12, -12)

    local tabBar = Kit.Panel(CreateFrame("Frame", nil, panel), "surface-2")
    tabBar:SetPoint("TOPLEFT", 1, -50)
    tabBar:SetPoint("TOPRIGHT", -1, -50)
    tabBar:SetHeight(35)
    local scroll = Kit.Scroll(panel)
    scroll:SetPoint("TOPLEFT", 15, -99)
    scroll:SetPoint("BOTTOMRIGHT", -15, 55)
    local content = scroll.child
    local pages, tabs = {}, {}
    local current, draft, width
    for i, option in ipairs(tabsSpec) do
        local key, label = option[1], option[2]
        local page = CreateFrame("Frame", nil, content)
        page:SetPoint("TOPLEFT")
        page:Hide()
        pages[key] = page
        local tab = T.JournalSkin.Tab(tabBar, label, function() panel:Tab(key) end)
        tab:SetHeight(29)
        tabs[i] = tab
    end

    local apply = Kit.Key(panel, "Сохранить", true)
    apply:SetPoint("BOTTOMRIGHT", -14, 13)
    local cancel = Kit.Key(panel, "Отмена")
    cancel:SetPoint("RIGHT", apply, "LEFT", -8, 0)
    cancel:SetScript("OnClick", function() panel:Hide() end)
    local status = Kit.Text(panel, "t-meta", "ink-3")
    status:SetPoint("BOTTOMLEFT", 14, 17)
    status:SetPoint("RIGHT", cancel, "LEFT", -6, 0)
    status:SetJustifyH("LEFT")
    Kit.Hairline(panel, "edge", "BOTTOM", {offset = 48})

    local function changed()
        if not draft or not current then return end
        local dirty = false
        for _, key in ipairs(keys) do
            if key == "context" then
                for _, part in ipairs(T.Context.parts) do
                    if draft.context[part.key] ~= T.settings.context[part.key] then dirty = true end
                end
            elseif draft[key] ~= T.settings[key] then dirty = true end
        end
        status:SetText(dirty and T.L("Есть изменения") or "")
        apply:SetEnabled(dirty)
        local page = pages[current]
        page:SetWidth(width)
        page:Refresh(draft, width)
        content:SetHeight(page:GetHeight())
    end
    T.BuildAppearanceSettings(pages.display, function(key, value)
        if draft and draft[key] ~= value then draft[key] = value; changed() end
    end)
    local function getDraft() return draft end
    T.BuildLanguageSettings(pages.language, function(code)
        if draft and draft.locale ~= code then draft.locale = code; changed() end
    end)
    T.BuildDataSettings(pages.context, getDraft, changed, function(d) handlers.preview(d, panel) end)
    T.BuildAdviserSettings(pages.adviser, getDraft, changed)
    T.BuildNoticeSettings(pages.notifications, getDraft, changed)
    local function resize()
        local w, h = UIParent:GetSize()
        local box = math.max(1, math.min(790, w - 18))
        width = math.max(1, box - 34)
        panel:SetSize(box, math.max(1, math.min(502, h - 26)))
        content:SetWidth(width)
        local tabWidth = (box - 10) / #tabsSpec
        for i, tab in ipairs(tabs) do
            tab:SetWidth(tabWidth)
            tab:SetPoint("TOPLEFT", 4 + (i - 1) * tabWidth, -3)
        end
        changed()
    end
    apply:SetScript("OnClick", function()
        if not draft or not apply:IsEnabled() then return end
        local localeChanged = T.settings.locale ~= draft.locale
        for _, key in ipairs(keys) do T.settings[key] = draft[key] end
        T.Fire("SettingsSaved")
        panel:Hide()
        if localeChanged then T.RefreshLocale() end
        handlers.apply()
    end)
    function panel:Tab(key)
        if not pages[key] or not draft then return end
        current = key
        for i, option in ipairs(tabsSpec) do
            local isCurrent = option[1] == key
            pages[option[1]]:SetShown(isCurrent)
            tabs[i]:SetActive(isCurrent)
        end
        changed()
        scroll:ScrollTo(0, true)
    end
    function panel:Toggle(key)
        key = key or "display"
        if not pages[key] then return end
        if self:IsShown() and current == key then self:Hide(); return end
        if not self:IsShown() then
            draft = {}
            for _, k in ipairs(keys) do
                local value = T.settings[k]
                draft[k] = type(value) == "table" and CopyTable(value) or value
            end
            resize()
            veil:Show()
            self:Show()
        end
        self:Tab(key)
    end
    panel:HookScript("OnShow", function()
        owner:SetHeld(true)
        escapeLayer("looking_for_guidance_window", "looking_for_guidance_settings_panel")
    end)
    panel:HookScript("OnHide", function()
        if panel.dataPreview then panel.dataPreview:Hide() end
        escapeLayer("looking_for_guidance_data_preview", "looking_for_guidance_window")
        escapeLayer("looking_for_guidance_settings_panel", "looking_for_guidance_window")
        veil:Hide()
        owner:SetHeld(false)
        current, draft = nil, nil
    end)
    veil:SetScript("OnMouseDown", function()
        if panel.dataPreview and panel.dataPreview:IsVisible() then panel.dataPreview:Hide()
        else panel:Hide() end
    end)
    veil:SetScript("OnEvent", resize)
    veil:SetScript("OnShow", function(self)
        self:RegisterEvent("DISPLAY_SIZE_CHANGED")
        self:RegisterEvent("UI_SCALE_CHANGED")
    end)
    veil:SetScript("OnHide", function(self) self:UnregisterAllEvents() end)
    return panel
end

local function sheet(text)
    local lines, held = {}, nil
    for line in text:gmatch("[^\n]+") do
        local key, value = line:match("^(%S[^:]*):%s?(.*)$")
        if key and value == "" then
            held = key
        else
            table.insert(lines, { key = key or held, first = key ~= nil or held ~= nil, value = key and value or strtrim(line) })
            held = nil
        end
    end
    return lines
end

function Popovers.Preview(parent, anchor)
    local preview = Kit.Panel(CreateFrame("Frame", nil, parent), "surface-1")
    preview:SetPoint("TOPLEFT", anchor, "TOPLEFT", Kit.space["space-3"], -Kit.space["space-3"])
    preview:SetPoint("BOTTOMRIGHT", parent, "BOTTOMRIGHT", -Kit.space["space-3"] - 1, Kit.space["space-3"] + 1)
    preview:SetFrameStrata("FULLSCREEN_DIALOG")
    preview:SetFrameLevel(parent:GetFrameLevel() + 40)
    preview:EnableMouse(true)
    local heading = Kit.Text(preview, "t-ui-strong", "ink")
    heading:SetPoint("TOPLEFT", Kit.space["space-3"], -Kit.space["space-3"])
    T.BindLabel(heading, "Посмотреть данные")
    local close = Kit.Tool(preview, "close", function() Kit.Leave(preview) end)
    close:SetPoint("TOPRIGHT", -Kit.space["space-1"], -Kit.space["space-1"])
    local scroll = Kit.Scroll(preview)
    scroll:SetPoint("TOPLEFT", Kit.space["space-3"], -(Kit.space["space-3"] + Kit.size.control))
    scroll:SetPoint("BOTTOMRIGHT", -Kit.space["space-3"], Kit.space["space-3"])
    local rows = {}
    local id = "|c" .. Kit.Hex("ink-3") .. "%1|r"
    function preview:Open(draft, sheetOwner)
        if sheetOwner then
            sheetOwner.dataPreview = self
            self:SetParent(sheetOwner)
            self:SetFrameLevel(sheetOwner:GetFrameLevel() + 40)
            self:ClearAllPoints()
            self:SetPoint("TOPLEFT", sheetOwner, "TOPLEFT", 5, -5)
            self:SetPoint("BOTTOMRIGHT", sheetOwner, "BOTTOMRIGHT", -5, 5)
        end
        self:Show()
        local lines = sheet((T.Context.Preview or T.Context.Text)(draft) or "")
        local column = 0
        for index, line in ipairs(lines) do
            local row = rows[index]
            if not row then
                row = CreateFrame("Frame", nil, scroll.child)
                row.rule = Kit.Hairline(row, "line", "TOP")
                row.key = Kit.Text(row, "t-meta", "ink-3")
                row.value = Kit.Text(row, "t-meta", "ink")
                rows[index] = row
            end
            row.key:SetText(line.first and Format.Escape(line.key or "") or "")
            column = math.max(column, row.key:GetUnboundedStringWidth())
        end
        local width, y = scroll:GetWidth(), 0
        column = math.min(column, width * 0.34)
        scroll.child:SetWidth(width)
        for index, line in ipairs(lines) do
            local row = rows[index]
            local start = line.first and index > 1
            local top = start and Kit.space["space-1"] + Kit.space["space-1"] / 2 or Kit.space["space-1"] / 2
            y = y + (start and Kit.space["space-1"] or 0)
            row.rule:SetShown(start)
            row.value:SetWidth(width - column - Kit.space["space-3"])
            row.value:SetText((Format.Escape(line.value):gsub("(%(?%a+:%d+%)?)", id)))
            Kit.Place(row.key, row, 0, top)
            Kit.Place(row.value, row, column + Kit.space["space-3"], top)
            local height = top + Kit.TextHeight(row.value) + Kit.space["space-1"] / 2
            row:ClearAllPoints()
            row:SetPoint("TOPLEFT", 0, -y)
            row:SetPoint("RIGHT")
            row:SetHeight(height)
            row:Show()
            y = y + height
        end
        for index = #lines + 1, #rows do
            rows[index]:Hide()
        end
        scroll.child:SetHeight(y)
        scroll:ScrollTo(0, true)
        Kit.Rise(self)
        for index = 1, #lines do
            Kit.Rise(rows[index], Kit.Stagger(index))
        end
    end
    _G.looking_for_guidance_data_preview = preview
    preview:HookScript("OnShow", function() escapeLayer("looking_for_guidance_settings_panel", "looking_for_guidance_data_preview") end)
    preview:HookScript("OnHide", function()
        escapeLayer("looking_for_guidance_data_preview", "looking_for_guidance_settings_panel")
    end)
    preview:Hide()
    return preview
end
