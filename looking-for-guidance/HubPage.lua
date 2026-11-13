local _, T = ...
local Kit = T.Kit
local Page = {}
T.HubPage = Page

local function label(parent, style, ink)
    local text = Kit.Text(parent, style, ink)
    text:SetJustifyH("LEFT")
    text:SetJustifyV("TOP")
    text:SetWordWrap(true)
    return text
end

-- Game titles, notes and quest text are literal data, not WoW markup.
local function put(text, value, width)
    text:SetWidth(math.max(1, width))
    T.SetContent(text, T.Format.Escape(value or ""))
    return math.max(T.Tokens.font[text.companionStyle].size, Kit.TextHeight(text))
end

local function anchor(row, parent, x, y)
    row:ClearAllPoints()
    row:SetPoint("TOPLEFT", parent, "TOPLEFT", x, -y)
end

local function highlight(button)
    local glow = button:CreateTexture(nil, "HIGHLIGHT")
    glow:SetAllPoints()
    glow:SetColorTexture(.72, .53, .24, .17)
end

local function makeIcon(parent)
    local icon = parent:CreateTexture(nil, "ARTWORK")
    return icon
end

local function makeControl(parent, primary)
    if primary then
        -- The localized caption comes from the current card each render.
        -- A binding to an empty key could reset its size during a locale change.
        local button = Kit.Key(parent, nil, true)
        button.label:SetWordWrap(true)
        return button
    end
    local button = CreateFrame("Button", nil, parent)
    button.label = label(button, "t-ui", "detail")
    button.label:SetPoint("LEFT", 4, 0)
    local underline = button:CreateTexture(nil, "ARTWORK")
    Kit.Paint(underline, "rule")
    underline:SetHeight(1)
    underline:SetAlpha(.65)
    underline:SetPoint("TOPLEFT", button.label, "BOTTOMLEFT", 0, 0)
    underline:SetPoint("TOPRIGHT", button.label, "BOTTOMRIGHT", 0, 0)
    button:SetScript("OnEnter", function() Kit.Paint(button.label, "link") end)
    button:SetScript("OnLeave", function() Kit.Paint(button.label, "detail") end)
    return button
end

local function control(button, spec, maxWidth)
    local padding = spec.primary and 20 or 8
    local text = button.label
    text:SetWidth(0)
    T.SetContent(text, T.Format.Escape(spec.label))
    local width = math.min(maxWidth, math.max(32, math.ceil(text:GetUnboundedStringWidth()) + padding))
    local height = put(text, spec.label, width - padding)
    text:SetJustifyV("MIDDLE")
    button:SetSize(width, math.max(23, height + (spec.primary and 9 or 5)))
    button:SetEnabled(spec.enabled ~= false)
    button:SetScript("OnClick", spec.click)
    if not spec.primary then Kit.Paint(text, "detail") end
    return width, button:GetHeight()
end

-- Each toolbar owns its children so the lead card and detail page have
-- independent input regions. Localized actions wrap onto a second line.
local function placeActions(owner, actions, x, y, width)
    owner.journalButtons = owner.journalButtons or {primary={}, links={}}
    local used = {primary=0, links=0}
    local offset, top, rowHeight = 0, 0, 0
    for _, spec in ipairs(actions) do
        local kind = spec.primary and "primary" or "links"
        used[kind] = used[kind] + 1
        local pool, index = owner.journalButtons[kind], used[kind]
        local button = pool[index]
        if not button then button = makeControl(owner, spec.primary); pool[index] = button end
        button:Show()
        local w, h = control(button, spec, width)
        if offset > 0 and offset + w > width then top, offset, rowHeight = top + rowHeight + 3, 0, 0 end
        local pos = spec.trailing and math.max(offset, width - w) or offset
        anchor(button, owner, x + pos, y + top)
        rowHeight = math.max(rowHeight, h)
        offset = pos + w + 9
    end
    for kind, buttons in pairs(owner.journalButtons) do
        for i = used[kind] + 1, #buttons do buttons[i]:Hide() end
    end
    return top + rowHeight
end

function Page.Create(parent)
    local frame = CreateFrame("Frame", nil, parent)
    frame:Hide()
    local scroll = Kit.Scroll(frame)
    scroll:SetAllPoints(frame)
    scroll.rail:EnableMouse(false)
    Kit.Fill(frame, "page")
    local paper = frame:CreateTexture(nil, "BACKGROUND", nil, 1)
    paper:SetAllPoints()
    paper:SetTexture(T.media .. "Journal\\Paper", "REPEAT", "REPEAT")
    paper:SetHorizTile(true)
    paper:SetVertTile(true)
    paper:SetAlpha(.86)

    local pools, used = {}, {}
    local y, width, left = 0, 80, 15
    local function nextRow(kind, make)
        local index = (used[kind] or 0) + 1
        used[kind] = index
        pools[kind] = pools[kind] or {}
        local row = pools[kind][index]
        if not row then row = make(); pools[kind][index] = row end
        row:ClearAllPoints()
        row:Show()
        return row
    end
    local function take(kind, build)
        return nextRow(kind, function()
            local row = CreateFrame("Frame", nil, scroll.child)
            build(row)
            return row
        end)
    end
    local function place(row, height, gap)
        anchor(row, scroll.child, left, y)
        row:SetSize(width, height)
        y = y + height + (gap or 5)
    end

    function frame:Begin()
        wipe(used)
        left = self:GetWidth() < 430 and 14 or 20
        y = left - 2
        width = math.max(80, self:GetWidth() - 2 * left)
        scroll.child:SetWidth(width + 2 * left)
        paper:SetShown(T.settings.appearance == "journal" or not T.settings.appearance)
    end
    function frame:Text(text, style, ink)
        style, ink = style or "t-body", ink or "prose"
        local row = nextRow(style .. ":" .. ink, function() return label(scroll.child, style, ink) end)
        anchor(row, scroll.child, left, y)
        y = y + put(row, text, width) + 4
        return row
    end
    function frame:Actions(actions)
        local row = take("actions", function() end)
        place(row, placeActions(row, actions, 0, 0, width), 7)
    end
    -- Legacy memory tab uses single actions.
    function frame:Action(text, click, enabled)
        self:Actions({{label=text, click=click, primary=true, enabled=enabled}})
    end
    function frame:Heading(eyebrow, title, action)
        local row = take("heading", function(r)
            r.kicker = label(r, "t-meta", "detail")
            r.title = label(r, "t-display", "strong")
            r.option = makeControl(r, false)
        end)
        local actionWidth, actionHeight = 0, 0
        row.option:SetShown(action ~= nil)
        if action then
            actionWidth, actionHeight = control(row.option, action, math.floor(width * .46))
            anchor(row.option, row, width - actionWidth, 3)
        end
        local measure = width - actionWidth - 9
        anchor(row.kicker, row, 0, 0)
        local head = put(row.kicker, eyebrow, measure)
        anchor(row.title, row, 0, head + 2)
        place(row, math.max(head + 2 + put(row.title, title, measure), actionHeight + 3), 10)
    end

    local function makeTitle(row)
        row.icon = makeIcon(row)
        row.kicker = label(row, "t-meta", "detail")
        row.title = label(row, "t-display", "strong")
    end
    local function paintTitle(row, x, top, w, icon, kicker, title)
        row.icon:SetTexture(icon)
        row.icon:SetSize(33, 33)
        anchor(row.icon, row, x, top + 2)
        local textWidth = w - 43
        anchor(row.kicker, row, x + 43, top)
        local height = put(row.kicker, kicker, textWidth) + 2
        anchor(row.title, row, x + 43, top + height)
        return math.max(37, height + put(row.title, title, textWidth))
    end
    function frame:Lead(card, icon, actions, changed)
        local row = take("lead", function(r)
            makeTitle(r)
            r.tint = r:CreateTexture(nil, "BACKGROUND")
            r.tint:SetAllPoints()
            for _, side in ipairs({"LEFT","RIGHT","TOP","BOTTOM"}) do
                Kit.Hairline(r, "rule", side)
                Kit.Hairline(r, "rule", side, {offset=3,indent=3}):SetAlpha(.34)
            end
            r.reason = label(r, "t-body", "prose")
            r.change = label(r, "t-ui", "detail")
            r.mark = Kit.Stripe(r, "rule")
        end)
        local pad, inside = 12, width - 24
        row.tint:SetColorTexture(1, .90, .67, T.settings.appearance == "journal" and .27 or .07)
        local cursor = pad + paintTitle(row, pad, pad, inside, icon, card.kicker, card.title) + 6
        anchor(row.reason, row, pad, cursor)
        cursor = cursor + put(row.reason, card.reason, inside) + 7
        row.change:SetShown(not not changed)
        row.mark:SetShown(not not changed)
        if changed then
            anchor(row.change, row, pad + 9, cursor + 2)
            local h = put(row.change, changed, inside - 9) + 4
            row.mark:ClearAllPoints()
            row.mark:SetPoint("TOPLEFT", row, "TOPLEFT", pad, -cursor)
            row.mark:SetSize(2, h)
            cursor = cursor + h + 5
        end
        cursor = cursor + placeActions(row, actions, pad, cursor, inside)
        place(row, cursor + pad, 12)
    end
    function frame:DetailTitle(icon, kicker, title)
        local row = take("detailTitle", makeTitle)
        place(row, paintTitle(row, 0, 0, width, icon, kicker, title), 7)
    end
    function frame:Section(text)
        local row = take("section", function(r)
            r.title = label(r, "t-title", "strong")
            r.line = r:CreateTexture(nil, "BORDER")
            Kit.Paint(r.line, "rule")
            r.line:SetHeight(1)
            r.line:SetAlpha(.58)
        end)
        anchor(row.title, row, 0, 0)
        local height = put(row.title, text, width)
        local extent = math.min(width, row.title:GetUnboundedStringWidth() + 10)
        anchor(row.line, row, extent, math.floor(height / 2))
        row.line:SetWidth(math.max(1, width - extent))
        row.line:SetShown(extent < width)
        place(row, height, 5)
    end
    function frame:Grid(tiles)
        local columns = width >= 266 and 2 or 1
        local gutter = 14
        local cell = (width - (columns-1)*gutter)/columns
        local rowHeight = 0
        for i, tile in ipairs(tiles) do
            local row = nextRow("tile", function()
                local button = CreateFrame("Button", nil, scroll.child)
                button.icon = makeIcon(button)
                button.icon:SetSize(25, 25)
                button.icon:SetPoint("TOPLEFT", 2, -6)
                button.title = label(button, "t-ui", "strong")
                button.summary = label(button, "t-meta", "detail")
                button.arrow = label(button, "t-title", "detail")
                button.arrow:SetText("›")
                button.arrow:SetPoint("RIGHT", -2, 0)
                button.dot = button:CreateTexture(nil, "OVERLAY")
                button.dot:SetSize(6, 6)
                button.dot:SetPoint("TOPLEFT", 24, -3)
                Kit.Paint(button.dot, "agent-needs")
                Kit.Hairline(button, "rule", "BOTTOM"):SetAlpha(.4)
                highlight(button)
                return button
            end)
            local col = (i - 1) % columns
            anchor(row, scroll.child, left + col*(cell+gutter), y)
            local measure = cell - 46
            row.icon:SetTexture(tile.icon)
            row.dot:SetShown(tile.unread == true)
            anchor(row.title, row, 34, 4)
            local h = put(row.title, tile.label, measure)
            anchor(row.summary, row, 34, 5+h)
            h = math.max(36, h + put(row.summary, tile.summary, measure) + 10)
            row:SetSize(cell, h)
            row:SetScript("OnClick", tile.click)
            rowHeight = math.max(rowHeight, h)
            if col == columns-1 or i == #tiles then
                local count = used.tile
                for j = count-col, count do pools.tile[j]:SetHeight(rowHeight) end
                y = y + rowHeight + 2
                rowHeight = 0
            end
        end
    end
    function frame:Step(index, step)
        local row = take("step", function(r)
            r.mark = label(r, "t-ui-strong", "detail")
            r.kicker = label(r, "t-meta", "detail")
            r.title = label(r, "t-title", "strong")
            r.body = label(r, "t-ui", "prose")
            Kit.Hairline(r, "rule", "BOTTOM"):SetAlpha(.5)
        end)
        anchor(row.mark, row, 2, 4)
        put(row.mark, tostring(index), 20)
        local w, cursor = width - 27, 2
        for _, part in ipairs({{row.kicker, step.label}, {row.title, step.title}, {row.body, step.text}}) do
            part[1]:SetShown(type(part[2]) == "string" and part[2] ~= "")
            if part[1]:IsShown() then
                anchor(part[1], row, 27, cursor)
                cursor = cursor + put(part[1], part[2], w) + 2
            end
        end
        place(row, cursor + 5, 5)
    end
    -- good / warn / bad colour the mark; anything else is a plain note.
    local TONES = { good = "state-open", warn = "agent-needs", bad = "agent-error" }
    function frame:Verdict(tone, title, text)
        local row = take("verdict", function(r)
            r.tint = r:CreateTexture(nil, "BACKGROUND")
            r.tint:SetAllPoints()
            r.mark = r:CreateTexture(nil, "ARTWORK")
            r.mark:SetPoint("TOPLEFT")
            r.mark:SetPoint("BOTTOMLEFT")
            r.mark:SetWidth(3)
            r.title = label(r, "t-title", "strong")
            r.body = label(r, "t-ui", "prose")
        end)
        row.tint:SetColorTexture(1, .90, .67, T.settings.appearance == "journal" and .22 or .06)
        Kit.Paint(row.mark, TONES[tone] or "rule")
        row.tone = tone
        anchor(row.title, row, 12, 7)
        local cursor = 7 + put(row.title, title, width - 22)
        row.body:SetShown(type(text) == "string" and text ~= "")
        if row.body:IsShown() then
            anchor(row.body, row, 12, cursor + 3)
            cursor = cursor + 3 + put(row.body, text, width - 22)
        end
        place(row, cursor + 8, 8)
        return row
    end
    -- One row for checklists, the gold ledger and bag items. state: done, todo, warn, skip or nil.
    local MARKS = { done = { "check", "state-open" }, warn = { "warning", "agent-needs" } }
    function frame:Check(item)
        local row = take("check", function(r)
            r.glyph = Kit.Glyph(r, "check", "detail")
            r.dot = label(r, "t-ui-strong", "detail")
            r.title = label(r, "t-ui", "strong")
            r.value = label(r, "t-ui-strong", "strong")
            r.value:SetJustifyH("RIGHT")
            r.body = label(r, "t-meta", "detail")
            Kit.Hairline(r, "rule", "BOTTOM"):SetAlpha(.35)
            r:SetScript("OnEnter", function(self)
                if not self.link then return end
                GameTooltip:SetOwner(self, "ANCHOR_RIGHT")
                if pcall(GameTooltip.SetHyperlink, GameTooltip, self.link) then GameTooltip:Show() else GameTooltip:Hide() end
            end)
            r:SetScript("OnLeave", function() GameTooltip:Hide() end)
        end)
        local mark = MARKS[item.state]
        row.state, row.link = item.state, item.link
        row:EnableMouse(item.link ~= nil)
        row.glyph:SetShown(mark ~= nil)
        row.dot:SetShown(mark == nil)
        if mark then
            row.glyph:SetGlyph(mark[1])
            row.glyph:SetInk(mark[2])
            row.glyph:ClearAllPoints()
            row.glyph:SetPoint("TOPLEFT", 3, -6)
        else
            anchor(row.dot, row, 6, 3)
            put(row.dot, item.state == "skip" and "–" or "•", 14)
        end
        Kit.Paint(row.title, item.state == "skip" and "detail" or "strong")
        local valueWidth = 0
        row.value:SetShown(type(item.value) == "string" and item.value ~= "")
        if row.value:IsShown() then
            row.value:SetWidth(0)
            T.SetContent(row.value, T.Format.Escape(item.value))
            valueWidth = math.min(math.floor(width * .42), math.ceil(row.value:GetUnboundedStringWidth()) + 2)
            anchor(row.value, row, width - valueWidth, 4)
            put(row.value, item.value, valueWidth)
        end
        local measure = width - 24 - (valueWidth > 0 and valueWidth + 8 or 0)
        anchor(row.title, row, 24, 4)
        local cursor = 4 + put(row.title, item.title, measure)
        row.body:SetShown(type(item.text) == "string" and item.text ~= "")
        if row.body:IsShown() then
            anchor(row.body, row, 24, cursor + 1)
            cursor = cursor + 1 + put(row.body, item.text, width - 24)
        end
        place(row, cursor + 6, 2)
        return row
    end
    function frame:Pending(text, since)
        local row = take("pending", function(r)
            r.spinner = Kit.Spinner(r)
            r.spinner:SetPoint("TOPLEFT", 3, -9)
            r.text = label(r, "t-ui", "detail")
        end)
        anchor(row.text, row, 24, 2)
        local elapsed = since and math.max(0, time() - since) or 0
        local caption = elapsed >= 5 and (text .. " · " .. T.Format.Clock(elapsed)) or text
        place(row, put(row.text, caption, width - 24) + 6, 6)
        return row
    end
    -- Sources are addresses to copy: the game cannot open a browser.
    local copyBox
    function frame:Sources(list, checked)
        local shown = 0
        for _, source in ipairs(list or {}) do
            local host = T.Format.SourceHost(source.url)
            if host and shown < 5 then
                shown = shown + 1
                local row = nextRow("source", function()
                    local button = makeControl(scroll.child, false)
                    button.label:SetWordWrap(false)
                    return button
                end)
                local title = type(source.title) == "string" and source.title ~= "" and (host .. " · " .. source.title) or host
                anchor(row, scroll.child, left, y)
                local _, height = control(row, {label = title, click = function()
                    copyBox = copyBox or Kit.CopyBox(frame)
                    copyBox:Open(host, source.url)
                end}, width)
                row.url = source.url
                y = y + height + 2
            end
        end
        if checked and checked > 0 then
            self:Text(T.L("Проверено: {1}", T.Format.Ago(time() - checked)), "t-meta", "detail")
        end
        return shown
    end
    function frame:Reserve(height)
        local at = y
        y = y + height
        return scroll.child, width, at
    end
    function frame:Finish()
        for kind, pool in pairs(pools) do
            for i = (used[kind] or 0) + 1, #pool do pool[i]:Hide() end
        end
        scroll.child:SetHeight(y + 10)
    end
    function frame:Top() scroll:ScrollTo(0, true) end
    frame.scroll = scroll
    return frame
end
