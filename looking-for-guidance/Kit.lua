local _, T = ...

local Kit = {}
T.Kit = Kit

local tokens = T.Tokens
local fonts, hairlines = {}, {}
local reading = setmetatable({}, { __mode = "k" })

Kit.space = tokens.space
Kit.size = tokens.size
Kit.motion = tokens.motion

function Kit.Reader()
    return T.settings and T.Readers[T.settings.appearance] or T.Readers.journal
end

function Kit.Color(name)
    local hex = Kit.Reader()[name]
    if hex then
        return tonumber(hex:sub(1, 2), 16) / 255, tonumber(hex:sub(3, 4), 16) / 255, tonumber(hex:sub(5), 16) / 255, 1
    end
    local c = tokens.color[name]
    return c[1], c[2], c[3], c[4]
end

function Kit.Hex(name)
    local r, g, b = Kit.Color(name)
    return string.format("ff%02x%02x%02x", math.floor(r * 255 + 0.5), math.floor(g * 255 + 0.5), math.floor(b * 255 + 0.5))
end

function Kit.Paint(region, name)
    reading[region] = Kit.Reader()[name] and name or nil
    if region:IsObjectType("FontString") then
        region:SetTextColor(Kit.Color(name))
    else
        region:SetColorTexture(Kit.Color(name))
    end
end

function Kit.Font(style)
    if not fonts[style] then
        local spec = tokens.font[style]
        local font = CreateFont("looking_for_guidance_" .. style:gsub("%-", "_"))
        font:SetFont(T.FontFile(spec.file), spec.size, "")
        font:SetSpacing(spec.line - spec.size)
        font:SetJustifyH("LEFT")
        font:SetJustifyV("TOP")
        fonts[style] = font
    end
    return fonts[style]
end

function Kit.ApplyReading()
    local spec = tokens.font["t-body"]
    spec.size = math.min(23, math.max(15, tonumber(T.settings.fontSize) or 17))
    spec.line = math.floor(spec.size * (tonumber(T.settings.lineHeight) or 1.45) + .5)
    for style, font in pairs(fonts) do
        local entry = tokens.font[style]
        font:SetFont(T.FontFile(entry.file), entry.size, "")
        font:SetSpacing(entry.line - entry.size)
    end
    for region, name in pairs(reading) do Kit.Paint(region, name) end
end

-- Use the game's nine-slice dialog edge. The flat gold edges already painted by
-- the caller also work on clients that do not provide BackdropTemplateMixin.
function Kit.GoldFrame(parent, edge)
    if not BackdropTemplateMixin then return end
    local border = CreateFrame("Frame", nil, parent, "BackdropTemplate")
    border:SetPoint("TOPLEFT", -4, 4)
    border:SetPoint("BOTTOMRIGHT", 4, -4)
    border:SetFrameLevel(parent:GetFrameLevel() + 24)
    border:EnableMouse(false)
    border:SetBackdrop({ edgeFile = "Interface\\DialogFrame\\UI-DialogBox-Gold-Border", edgeSize = edge or 16 })
    border:SetBackdropBorderColor(0.89, 0.78, 0.57)
    return border
end

local function nativeButton(button)
    for state, file in pairs({ Normal = "Up", Pushed = "Down", Disabled = "Disabled", Highlight = "Highlight" }) do
        local texture = button:CreateTexture(nil, state == "Highlight" and "HIGHLIGHT" or "BORDER")
        texture:SetTexture("Interface\\Buttons\\UI-Panel-Button-" .. file)
        texture:SetTexCoord(0, 0.625, 0, 0.6875)
        texture:SetAllPoints()
        if state == "Highlight" then texture:SetBlendMode("ADD") end
        button["Set" .. state .. "Texture"](button, texture)
    end
end

function Kit.Text(parent, style, ink, layer)
    local text = parent:CreateFontString(nil, layer or "ARTWORK")
    text.companionStyle = style
    text:SetFontObject(Kit.Font(style))
    Kit.Paint(text, ink or "ink")
    local spec = tokens.font[style]
    text.leading = (spec.line - spec.size) / 2
    return text
end

function Kit.TextHeight(text)
    return text:GetStringHeight() + text.leading * 2
end

function Kit.Place(text, parent, x, y)
    text:SetPoint("TOPLEFT", parent, "TOPLEFT", x, -(y + text.leading))
end

function Kit.Fill(frame, name, layer, sublevel)
    local texture = frame:CreateTexture(nil, layer or "BACKGROUND", nil, sublevel)
    texture:SetAllPoints()
    Kit.Paint(texture, name)
    return texture
end

local function pixel(region)
    return PixelUtil.GetPixelToUIUnitFactor() / region:GetEffectiveScale()
end

local function snap(line)
    local p = pixel(line)
    local side, frame, inset = line.side, line.frame, line.offset + line.pixels * p
    line:ClearAllPoints()
    if side == "TOP" or side == "BOTTOM" then
        local y = side == "TOP" and -inset or inset
        line:SetPoint(side .. "LEFT", frame, side .. "LEFT", line.indent, y)
        line:SetPoint(side .. "RIGHT", frame, side .. "RIGHT", -line.indent, y)
        line:SetHeight(p)
    else
        local x = side == "LEFT" and inset or -inset
        line:SetPoint("TOP" .. side, frame, "TOP" .. side, x, -line.indent)
        line:SetPoint("BOTTOM" .. side, frame, "BOTTOM" .. side, x, line.indent)
        line:SetWidth(p)
    end
end

function Kit.Hairline(frame, name, side, options)
    options = options or {}
    local line = frame:CreateTexture(nil, options.layer or "BORDER")
    Kit.Paint(line, name)
    line:SetSnapToPixelGrid(true)
    line.side, line.frame = side, frame
    line.pixels, line.offset, line.indent = options.pixels or 0, options.offset or 0, options.indent or 0
    snap(line)
    table.insert(hairlines, line)
    return line
end

function Kit.Snap()
    for _, line in ipairs(hairlines) do
        snap(line)
    end
end

function Kit.Edge(frame)
    local edges = {}
    for _, side in ipairs({ "TOP", "BOTTOM", "LEFT", "RIGHT" }) do
        table.insert(edges, Kit.Hairline(frame, "edge", side))
    end
    return edges
end

function Kit.Panel(frame, surface)
    frame.fill = Kit.Fill(frame, surface or "surface-1")
    frame.edges = Kit.Edge(frame)
    frame.bevel = Kit.Hairline(frame, "bevel-light", "TOP", { pixels = 1, layer = "ARTWORK" })
    return frame
end

function Kit.Recess(frame)
    frame.fill = Kit.Fill(frame, "well")
    frame.edges = Kit.Edge(frame)
    frame.bevel = Kit.Hairline(frame, "bevel-shade", "TOP", { pixels = 1, layer = "ARTWORK" })
    return frame
end

function Kit.Stripe(frame, name)
    local stripe = frame:CreateTexture(nil, "ARTWORK", nil, 1)
    stripe:SetPoint("TOPLEFT", 1, -1)
    stripe:SetPoint("BOTTOMLEFT", 1, 1)
    stripe:SetWidth(2)
    Kit.Paint(stripe, name)
    return stripe
end

-- Glyphs are white textures in Media/Glyphs, so the ink tints them.
function Kit.Glyph(parent, name, ink)
    local glyph = CreateFrame("Frame", nil, parent)
    glyph:SetSize(12, 12)
    glyph.texture = glyph:CreateTexture(nil, "ARTWORK")
    glyph.texture:SetAllPoints()
    function glyph:SetGlyph(next)
        self.name = next
        self.texture:SetTexture(T.media .. "Glyphs\\" .. next)
    end
    function glyph:SetInk(next)
        self.ink = next
        self.texture:SetVertexColor(Kit.Color(next))
    end
    glyph:SetGlyph(name)
    glyph:SetInk(ink or "ink-3")
    return glyph
end

local toolLabels = {
    plus = "Новый чат",
    gear = "Настройки", minimize = "Свернуть", restore = "Развернуть", close = "Закрыть",
}

function Kit.Tool(parent, name, onClick)
    local tool = CreateFrame("Button", nil, parent)
    tool:SetSize(tokens.size.control, tokens.size.control)
    nativeButton(tool)
    tool.hover = Kit.Fill(tool, "surface-3")
    tool.hover:Hide()
    tool.glyph = Kit.Glyph(tool, name, "gold")
    tool.glyph:SetPoint("CENTER")
    local function lit(on)
        tool.hover:SetShown(on or tool.held)
        tool.glyph:SetInk((on or tool.held) and "ink" or "gold")
    end
    function tool:SetHeld(held)
        self.held = held
        lit(self:IsMouseOver())
    end
    tool:SetScript("OnEnter", function(self)
        lit(true)
        local label = self.glyph.name == "sidebar" and (self.held and "Скрыть список чатов" or "Показать список чатов") or toolLabels[self.glyph.name]
        if label then
            GameTooltip:SetOwner(self, "ANCHOR_TOP")
            GameTooltip:SetText(T.L(label))
            GameTooltip:Show()
        end
    end)
    tool:SetScript("OnLeave", function()
        lit(false)
        GameTooltip:Hide()
    end)
    tool:HookScript("OnHide", function(self)
        if GameTooltip:IsOwned(self) then GameTooltip:Hide() end
    end)
    tool:SetScript("OnClick", onClick)
    return tool
end

function Kit.Link(parent, style, ink, hover, glyph)
    local link = CreateFrame("Button", nil, parent)
    link:SetHeight(tokens.space["space-4"])
    link.text = Kit.Text(link, style, ink)
    link.text:SetPoint("LEFT")
    link.text:SetWordWrap(false)
    if glyph then
        link.glyph = Kit.Glyph(link, glyph, ink)
        link.glyph:SetPoint("RIGHT")
        link.text:SetPoint("RIGHT", link.glyph, "LEFT", -tokens.space["space-1"], 0)
    else
        link.text:SetPoint("RIGHT")
    end
    local function lit(on)
        local shade = (on or link.held) and hover or ink
        Kit.Paint(link.text, shade)
        if link.glyph then
            link.glyph:SetInk(shade)
        end
    end
    function link:SetHeld(held)
        self.held = held
        lit(self:IsMouseOver())
    end
    function link:SetLabel(label)
        T.BindLabel(self.text, label, function(text)
            self:SetWidth(text:GetUnboundedStringWidth() + (self.glyph and tokens.space["space-1"] + self.glyph:GetWidth() or 0))
        end)
    end
    link:SetScript("OnEnter", function() lit(true) end)
    link:SetScript("OnLeave", function() lit(false) end)
    return link
end

function Kit.Key(parent, label, gold)
    local key = CreateFrame("Button", nil, parent)
    key:SetHeight(tokens.size.control)
    Kit.Panel(key, gold and "gold" or "surface-3")
    nativeButton(key)
    key.label = Kit.Text(key, gold and "t-ui-strong" or "t-ui", gold and "bg" or "ink", "OVERLAY")
    key.label:SetPoint("CENTER")
    local function paint(hover, down)
        local enabled = key:IsEnabled()
        Kit.Paint(key.fill, not enabled and "surface-2" or gold and "gold" or hover and "surface-4" or "surface-3")
        Kit.Paint(key.bevel, down and "bevel-shade" or "bevel-light")
        key.bevel:SetShown(enabled)
        Kit.Paint(key.label, not enabled and "ink-3" or gold and "gold" or "ink")
        if key.glyph then
            key.glyph:SetInk(not enabled and "ink-3" or gold and "gold" or "ink")
        end
        key.label:SetPoint("CENTER", 0, down and -1 or 0)
    end
    key:SetScript("OnEnter", function() paint(true) end)
    key:SetScript("OnLeave", function() paint(false) end)
    key:SetScript("OnMouseDown", function(self) paint(self:IsMouseOver(), self:IsEnabled()) end)
    key:SetScript("OnMouseUp", function(self) paint(self:IsMouseOver()) end)
    key:SetScript("OnEnable", function() paint(false) end)
    key:SetScript("OnDisable", function() paint(false) end)
    function key:SetLabel(text)
        T.BindLabel(self.label, text, function(label)
            self:SetWidth(math.max(64, math.ceil(label:GetUnboundedStringWidth()) + 2 * tokens.space["space-3"]))
        end)
    end
    function key:SetGlyph(name)
        self.glyph = self.glyph or Kit.Glyph(self, name)
        self.glyph:SetGlyph(name)
        self.glyph:SetPoint("CENTER")
        self:SetWidth(tokens.size.control)
        paint(false)
    end
    if label then
        key:SetLabel(label)
    end
    paint(false)
    return key
end

function Kit.Segmented(parent, options, onChange)
    local control = Kit.Recess(CreateFrame("Frame", nil, parent))
    control:SetHeight(tokens.size.control)
    control.thumb = Kit.Panel(CreateFrame("Frame", nil, control), "surface-3")
    control.options = {}
    local function paint(button, hover)
        Kit.Paint(button.label, not button:IsEnabled() and "line-strong" or (hover or button.value == control.value) and "ink" or "ink-3")
    end
    for index, option in ipairs(options) do
        local button = CreateFrame("Button", nil, control)
        button.value = option.value
        button.label = Kit.Text(button, "t-meta", "ink-3", "OVERLAY")
        button.label:SetPoint("CENTER")
        T.BindLabel(button.label, option.label)
        button:SetScript("OnEnter", function(self) paint(self, true) end)
        button:SetScript("OnLeave", function(self) paint(self) end)
        button:SetScript("OnClick", function(self)
            if self.value ~= control.value then
                control:SetValue(self.value)
                onChange(self.value)
            end
        end)
        control.options[index] = button
    end
    function control:SetValue(value)
        self.value = value
        local width = (self:GetWidth() - 2) / #self.options
        for index, button in ipairs(self.options) do
            button:ClearAllPoints()
            button:SetPoint("TOPLEFT", 1 + (index - 1) * width, -1)
            button:SetSize(width, tokens.size.control - 2)
            paint(button)
            if button.value == value then
                self.thumb:ClearAllPoints()
                self.thumb:SetAllPoints(button)
            end
        end
    end
    function control:SetOptionEnabled(value, enabled)
        for _, button in ipairs(self.options) do
            if button.value == value then
                button:SetEnabled(enabled)
                paint(button)
            end
        end
    end
    control:SetScript("OnSizeChanged", function(self) self:SetValue(self.value) end)
    return control
end

function Kit.Slider(parent, low, high, step, onChange, live, suffix)
    local control = CreateFrame("Frame", nil, parent)
    control:SetHeight(tokens.size.control)
    control.readout = Kit.Text(control, "t-num", "ink-2")
    control.readout:SetPoint("RIGHT")
    control.readout:SetWidth(36)
    control.readout:SetJustifyH("RIGHT")
    local slider = CreateFrame("Slider", nil, control)
    slider:SetPoint("LEFT")
    slider:SetPoint("RIGHT", control.readout, "LEFT", -tokens.space["space-2"], 0)
    slider:SetHeight(tokens.space["space-4"])
    slider:SetOrientation("HORIZONTAL")
    slider:SetMinMaxValues(low, high)
    slider:SetValueStep(step)
    slider:SetObeyStepOnDrag(true)
    slider:EnableMouse(true)
    local track = slider:CreateTexture(nil, "BACKGROUND")
    track:SetPoint("LEFT")
    track:SetPoint("RIGHT")
    track:SetHeight(tokens.space["space-1"])
    Kit.Paint(track, "well")
    local fill = slider:CreateTexture(nil, "ARTWORK")
    fill:SetPoint("LEFT")
    fill:SetHeight(tokens.space["space-1"])
    Kit.Paint(fill, "line-strong")
    local thumb = slider:CreateTexture(nil, "ARTWORK")
    thumb:SetSize(tokens.space["space-2"], tokens.space["space-4"])
    thumb:SetColorTexture(0, 0, 0, 0)
    slider:SetThumbTexture(thumb)
    control.thumb = Kit.Panel(CreateFrame("Frame", nil, slider), "surface-4")
    control.thumb:SetAllPoints(thumb)
    control.slider = slider
    local function show(value)
        control.value = value
        control.readout:SetText(type(suffix) == "function" and suffix(value) or tostring(value) .. (suffix or "%"))
        local half = tokens.space["space-2"] / 2
        fill:SetWidth(half + (value - low) / (high - low) * (slider:GetWidth() - 2 * half))
    end
    slider:SetScript("OnValueChanged", function(_, value)
        value = math.floor(value / step + 0.5) * step
        if value ~= control.value then
            show(value)
            if live or not control.dragging then
                control.applied = value
                onChange(value)
            end
        end
    end)
    slider:SetScript("OnMouseDown", function() control.dragging = true end)
    slider:SetScript("OnMouseUp", function()
        control.dragging = false
        if control.value ~= control.applied then
            control.applied = control.value
            onChange(control.value)
        end
    end)
    slider:SetScript("OnSizeChanged", function() show(control.value or low) end)
    function control:SetValue(value)
        control.applied = value
        show(value)
        slider:SetValue(value)
    end
    return control
end

local tip

-- The design system's tooltip (.tb-tip): surface-2, a line edge, space-2 by space-3 padding, at most tip-measure wide.
local function showTip(owner, value)
    if not tip then
        tip = CreateFrame("Frame", nil, UIParent)
        tip:SetFrameStrata("TOOLTIP")
        tip:SetClampedToScreen(true)
        Kit.Fill(tip, "surface-2")
        for _, side in ipairs({ "TOP", "BOTTOM", "LEFT", "RIGHT" }) do
            Kit.Hairline(tip, "line", side)
        end
        tip.text = Kit.Text(tip, "t-ui", "ink")
        tip.text:SetJustifyH("LEFT")
        tip.text:SetWordWrap(true)
        Kit.Place(tip.text, tip, tokens.space["space-3"], tokens.space["space-2"])
    end
    tip.text:SetWidth(0)
    tip.text:SetText(value)
    tip.text:SetWidth(math.min(tip.text:GetUnboundedStringWidth(), tokens.size["tip-measure"]))
    tip:SetSize(tip.text:GetWidth() + tokens.space["space-3"] * 2, Kit.TextHeight(tip.text) + tokens.space["space-2"] * 2)
    tip.owner = owner
    -- Like WoW's ANCHOR_CURSOR: the bottom-left corner follows the pointer.
    local function follow(self)
        local x, y = GetCursorPosition()
        local scale = self:GetEffectiveScale()
        self:ClearAllPoints()
        self:SetPoint("BOTTOMLEFT", UIParent, "BOTTOMLEFT", x / scale, y / scale)
    end
    follow(tip)
    tip:SetScript("OnUpdate", follow)
    tip:Show()
end

local function hideTip(owner)
    if tip and tip.owner == owner then
        tip:SetScript("OnUpdate", nil)
        tip:Hide()
    end
end

function Kit.Tip(owner, text)
    owner:HookScript("OnEnter", function(self)
        if text:IsTruncated() then
            showTip(self, text:GetText())
        end
    end)
    owner:HookScript("OnLeave", hideTip)
    owner:HookScript("OnHide", hideTip)
end

function Kit.Label(parent, text)
    local label = Kit.Text(parent, "t-meta", "ink-3")
    label:SetHeight(tokens.size.control)
    label:SetJustifyV("MIDDLE")
    T.BindLabel(label, text)
    return label
end

function Kit.Option(parent, onClick)
    local option = CreateFrame("Button", nil, parent)
    option:SetHeight(tokens.size.control)
    option.mark = option:CreateTexture(nil, "ARTWORK")
    option.mark:SetSize(tokens.space["space-1"], tokens.space["space-1"])
    option.mark:SetPoint("LEFT")
    option.text = Kit.Text(option, "t-ui", "ink-3")
    option.text:SetPoint("LEFT", option.mark, "RIGHT", tokens.space["space-2"], 0)
    local function paint(self, hover)
        local enabled = self:IsEnabled()
        Kit.Paint(self.mark, not enabled and "line" or self.on and "accent" or "line-strong")
        Kit.Paint(self.text, not enabled and "line-strong" or (self.on or hover) and "ink" or "ink-3")
    end
    option:SetScript("OnEnter", function(self) paint(self, true) end)
    option:SetScript("OnLeave", function(self) paint(self) end)
    option:SetScript("OnClick", onClick)
    function option:Set(label, on, enabled)
        self.on = on
        self.text:SetText(label)
        self:SetEnabled(enabled ~= false)
        paint(self, self:IsMouseOver())
    end
    return option
end

function Kit.Mark(parent, size, ink)
    local mark = parent:CreateTexture(nil, "ARTWORK")
    mark:SetTexture(T.media .. "Mark")
    mark:SetSize(tokens.size[size], tokens.size[size])
    mark:SetVertexColor(Kit.Color(ink or "ink"))
    return mark
end

function Kit.Spinner(parent)
    local spinner = CreateFrame("Frame", nil, parent)
    spinner:SetSize(10, 2)
    spinner.dots = {}
    for index = 1, 3 do
        local dot = spinner:CreateTexture(nil, "ARTWORK")
        dot:SetSize(2, 2)
        dot:SetPoint("LEFT", (index - 1) * 4, 0)
        Kit.Paint(dot, "ink-2")
        spinner.dots[index] = dot
    end
    -- Same as the website's spinner: each dot is full for the first half of a 0.6 s cycle and at 0.25 for the rest.
    -- Each dot is 0.2 s behind the dot before it.
    spinner:SetScript("OnUpdate", function(self)
        local now = GetTime()
        for index, dot in ipairs(self.dots) do
            dot:SetAlpha((now - (index - 1) * 0.2) % 0.6 < 0.3 and 1 or 0.25)
        end
    end)
    return spinner
end

-- The game cannot open a browser, so an address is shown selected for Cmd+C.
function Kit.CopyBox(parent)
    local pad, gap = tokens.space["space-4"], tokens.space["space-2"]
    local box = Kit.Panel(CreateFrame("Frame", nil, parent), "surface-1")
    box:SetPoint("BOTTOMLEFT", pad, pad)
    box:SetPoint("BOTTOMRIGHT", -pad, pad)
    box:SetHeight(2 * tokens.size.control + 3 * gap)
    box:SetFrameLevel(parent:GetFrameLevel() + 30)
    box:EnableMouse(true)

    local close = Kit.Tool(box, "close", function() box:Hide() end)
    close:SetPoint("TOPRIGHT", -gap, -gap)
    box.heading = Kit.Text(box, "t-ui-strong", "ink")
    box.heading:SetPoint("LEFT", box, "LEFT", pad, 0)
    box.heading:SetPoint("TOP", close, "TOP", 0, -2)
    box.heading:SetPoint("RIGHT", close, "LEFT", -gap, 0)
    box.heading:SetWordWrap(false)

    local field = Kit.Recess(CreateFrame("Frame", nil, box))
    field:SetPoint("BOTTOMLEFT", gap, gap)
    field:SetPoint("BOTTOMRIGHT", -gap, gap)
    field:SetHeight(tokens.size.control)
    local edit = CreateFrame("EditBox", nil, field)
    edit:SetAllPoints()
    edit:SetTextInsets(gap, gap, 0, 0)
    edit:SetAutoFocus(false)
    edit:SetFontObject(Kit.Font("t-ui"))
    edit:SetTextColor(Kit.Color("ink"))
    edit:SetScript("OnEscapePressed", function() box:Hide() end)
    box:HookScript("OnHide", function() edit:ClearFocus() end)
    parent:HookScript("OnHide", function() box:Hide() end)
    box.edit = edit
    function box:Open(host, url)
        self:Show()
        self.heading:SetText(T.L("Скопировать адрес") .. " · " .. host .. " · Cmd+C")
        edit:SetText(url)
        edit:SetFocus()
        edit:HighlightText()
    end
    box:Hide()
    return box
end

function Kit.Scroll(parent)
    local scroll = CreateFrame("ScrollFrame", nil, parent)
    scroll.child = CreateFrame("Frame", nil, scroll)
    scroll.child:SetSize(1, 1)
    scroll:SetScrollChild(scroll.child)
    scroll.rail = CreateFrame("Frame", nil, scroll)
    scroll.rail:SetAllPoints()
    scroll.rail:SetFrameLevel(scroll.child:GetFrameLevel() + 4)
    scroll.thumb = scroll.rail:CreateTexture(nil, "OVERLAY")
    scroll.thumb:SetWidth(tokens.space["space-1"])
    Kit.Paint(scroll.thumb, "line-strong")
    scroll.target = 0
    local function place(self)
        local range, height = self:GetVerticalScrollRange(), self:GetHeight()
        self.thumb:SetShown(range > 0 and self:IsMouseOver())
        if range > 0 then
            local size = math.max(tokens.space["space-6"], height * height / (height + range))
            self.thumb:SetHeight(size)
            self.thumb:SetPoint("TOPRIGHT", -2, -(height - size) * self:GetVerticalScroll() / range)
        end
    end
    -- When settled over a list that can scroll, poll the pointer to show the thumb. The rows take the mouse, so the
    -- scroll frame gets no OnEnter.
    local function update(self, elapsed)
        local current = self:GetVerticalScroll()
        if math.abs(self.target - current) > 0.5 then
            self:SetVerticalScroll(current + (self.target - current) * math.min(1, elapsed * 16))
            place(self)
        elseif current ~= self.target then
            self:SetVerticalScroll(self.target)
            place(self)
        elseif self:GetVerticalScrollRange() > 0 then
            self.thumb:SetShown(self:IsMouseOver())
        else
            self:SetScript("OnUpdate", nil)
        end
    end
    local function wake(self)
        self.target = math.min(self.target, self:GetVerticalScrollRange())
        place(self)
        self:SetScript("OnUpdate", update)
    end
    local movement = 0
    function scroll:ScrollTo(y, instant)
        movement = movement + 1
        self.target = math.max(0, math.min(y, self:GetVerticalScrollRange()))
        self.atEnd = self.target >= self:GetVerticalScrollRange() - 1
        if instant then
            self:SetVerticalScroll(self.target)
        end
        wake(self)
    end
    function scroll:ScrollToEnd()
        self.atEnd = true
        movement = movement + 1
        local requested = movement
        C_Timer.After(0, function()
            if requested == movement then self:ScrollTo(self:GetVerticalScrollRange(), true) end
        end)
    end
    scroll:EnableMouseWheel(true)
    scroll:SetScript("OnMouseWheel", function(self, delta) self:ScrollTo(self.target - delta * 3 * tokens.space["space-4"]) end)
    scroll:SetScript("OnSizeChanged", function(self, width)
        self.child:SetWidth(width)
        wake(self)
    end)
    scroll:SetScript("OnScrollRangeChanged", wake)
    return scroll
end

function Kit.Popover(owner)
    local popover = Kit.Panel(CreateFrame("Frame", nil, owner), "surface-1")
    Kit.GoldFrame(popover, 14)
    popover:SetClampedToScreen(true)
    popover:SetFrameStrata("DIALOG")
    popover:EnableMouse(true)
    popover:Hide()
    popover:SetScript("OnShow", function(self) self:RegisterEvent("GLOBAL_MOUSE_DOWN") end)
    popover:SetScript("OnHide", function(self) self:UnregisterEvent("GLOBAL_MOUSE_DOWN") end)
    popover:SetScript("OnEvent", function(self)
        if not self:IsMouseOver() and not owner:IsMouseOver() then
            self:Hide()
        end
    end)
    return popover
end

local function rise(region)
    local group = region:CreateAnimationGroup()
    group.hold = group:CreateAnimation("Alpha")
    group.hold:SetFromAlpha(0)
    group.hold:SetToAlpha(0)
    group.hold:SetOrder(1)
    local drop = group:CreateAnimation("Translation")
    drop:SetOffset(0, -tokens.space["space-2"])
    drop:SetDuration(0)
    drop:SetOrder(1)
    local lift = group:CreateAnimation("Translation")
    lift:SetOffset(0, tokens.space["space-2"])
    lift:SetDuration(tokens.motion["motion-3"])
    lift:SetSmoothing("OUT")
    lift:SetOrder(2)
    local fade = group:CreateAnimation("Alpha")
    fade:SetFromAlpha(0)
    fade:SetToAlpha(1)
    fade:SetDuration(tokens.motion["motion-3"])
    fade:SetOrder(2)
    return group
end

local function leave(region)
    local group = region:CreateAnimationGroup()
    local lift = group:CreateAnimation("Translation")
    lift:SetOffset(0, tokens.space["space-1"])
    lift:SetDuration(tokens.motion["motion-1"])
    local fade = group:CreateAnimation("Alpha")
    fade:SetFromAlpha(1)
    fade:SetToAlpha(0)
    fade:SetDuration(tokens.motion["motion-1"])
    group:SetScript("OnFinished", function() region:Hide() end)
    return group
end

function Kit.Stagger(index)
    return math.min((index - 1) * tokens.motion["motion-stagger"], tokens.motion["motion-4"] - tokens.motion["motion-3"])
end

function Kit.Tween(region, duration, step, done)
    if not region.tween then
        region.tween = region:CreateAnimationGroup()
        region.tween.motion = region.tween:CreateAnimation("Animation")
        region.tween.motion:SetSmoothing("OUT")
    end
    local group = region.tween
    group:Stop()
    group.motion:SetDuration(duration)
    group.motion:SetScript("OnUpdate", function(self) step(self:GetSmoothProgress()) end)
    group:SetScript("OnFinished", function()
        step(1)
        done()
    end)
    group:Play()
end

function Kit.Rise(region, delay)
    region.rise = region.rise or rise(region)
    region.rise.hold:SetDuration(delay or 0)
    if region.leave then
        region.leave:Stop()
    end
    region:Show()
    region.rise:Restart()
end

function Kit.Leave(region)
    if region:IsShown() then
        region.leave = region.leave or leave(region)
        region.leave:Play()
    end
end
