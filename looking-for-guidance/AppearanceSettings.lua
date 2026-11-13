local _, T = ...
local Kit = T.Kit
local themes = { { "journal", "Журнал" }, { "dialog", "Диалог" }, { "tracker", "Трекер" } }

local function rgb(hex)
    return tonumber(hex:sub(1, 2), 16) / 255, tonumber(hex:sub(3, 4), 16) / 255, tonumber(hex:sub(5, 6), 16) / 255
end

local function piece(host, hex, top, left, width, height)
    local t = host:CreateTexture(nil, "ARTWORK")
    t:SetColorTexture(rgb(hex))
    t:SetPoint("TOPLEFT", left, -top)
    t:SetSize(width, height)
    return t
end

local function swatch(page, theme, label, choose)
    local palette = T.Readers[theme]
    local button = Kit.Panel(CreateFrame("Button", nil, page), "surface-2")
    button.value = theme
    button:SetHeight(73)
    button.label = Kit.Text(button, "t-ui", "ink")
    T.BindLabel(button.label, label)
    button.label:SetPoint("BOTTOM", 0, 5)
    local art = CreateFrame("Frame", nil, button)
    art:SetPoint("TOPLEFT", 5, -5)
    art:SetPoint("TOPRIGHT", -5, -5)
    art:SetHeight(43)
    local paper = art:CreateTexture(nil, "BACKGROUND")
    paper:SetAllPoints()
    paper:SetColorTexture(rgb(palette.page))
    local head = piece(art, theme == "tracker" and palette.rule or "473826", 0, 0, 4, theme == "tracker" and 2 or 7)
    head:SetPoint("TOPRIGHT")
    local tracker = theme == "tracker"
    if tracker then
        local icon = Kit.Text(art, "t-meta", "gold")
        icon:SetText("?")
        icon:SetPoint("TOPLEFT", 4, -7)
    else
        local quote = piece(art, palette.quote, 11, 0, 23, 5)
        quote:ClearAllPoints()
        quote:SetPoint("TOPRIGHT", -5, -11)
    end
    local x, y = tracker and 17 or 6, tracker and 11 or 22
    for i, length in ipairs({ 0.64, 0.82, 0.48 }) do
        local line = piece(art, i == 1 and palette.strong or palette.detail, y + 5 * (i - 1), x, 8, i == 1 and 3 or 2)
        art:HookScript("OnSizeChanged", function(_, width) line:SetWidth(math.max(1, (width - x - 5) * length)) end)
    end
    local function paint(hover)
        local ink = button.selected and "gold" or hover and "line-strong" or "edge"
        for _, edge in ipairs(button.edges) do Kit.Paint(edge, ink) end
        Kit.Paint(button.label, button.selected and "accent" or "ink")
    end
    function button:Select(selected) self.selected = selected; paint(self:IsMouseOver()) end
    button:SetScript("OnEnter", function() paint(true) end)
    button:SetScript("OnLeave", function() paint(false) end)
    button:SetScript("OnClick", function() choose(theme) end)
    return button
end

local function preview(page)
    local scene = Kit.Panel(CreateFrame("Frame", nil, page), "well")
    local land = scene:CreateTexture(nil, "BACKGROUND", nil, 1)
    land:SetTexture(T.media .. "BrillPreview")
    land:SetPoint("TOPLEFT", 1, -1)
    land:SetPoint("BOTTOMRIGHT", -1, 1)
    local answer = CreateFrame("Frame", nil, scene)
    local edge = Kit.Edge(answer)
    local body = answer:CreateTexture(nil, "BACKGROUND")
    body:SetAllPoints()
    local bar = Kit.Panel(CreateFrame("Frame", nil, answer), "surface-2")
    bar:SetPoint("TOPLEFT")
    bar:SetPoint("TOPRIGHT")
    bar:SetHeight(27)
    local crest = bar:CreateTexture(nil, "ARTWORK")
    crest:SetTexture(T.media .. "Compass")
    crest:SetSize(20, 20)
    crest:SetPoint("LEFT", 5, 0)
    local name = Kit.Text(bar, "t-title", "gold")
    name:SetPoint("LEFT", crest, "RIGHT", 5, 0)
    local question = Kit.Text(answer, "t-ui", "ink")
    question:SetJustifyH("RIGHT")
    question:SetWordWrap(true)
    local bubble = answer:CreateTexture(nil, "BORDER")
    bubble:SetPoint("TOPLEFT", question, "TOPLEFT", -6, 2)
    bubble:SetPoint("BOTTOMRIGHT", question, "BOTTOMRIGHT", 6, -2)
    local title = Kit.Text(answer, "t-title", "ink")
    title:SetWordWrap(true)
    local prose = Kit.Text(answer, "t-body", "ink")
    prose:SetWordWrap(true)
    local mark = Kit.Text(answer, "t-title", "gold")
    mark:SetText("?")

    function scene:Refresh(draft, available)
        local tracking = draft.appearance == "tracker"
        local colors = T.Readers[draft.appearance]
        local zoom = draft.scale
        local width = math.max(80, math.min(available - 14, available * 0.82 * zoom) / zoom)
        answer:SetScale(zoom)
        answer:SetWidth(width)
        answer:SetPoint("CENTER")
        local textWidth = math.max(40, width - (tracking and 40 or 24))
        local spacing = math.max(0, math.floor(draft.fontSize * (draft.lineHeight - 1) + .5))
        T.SetPreviewText(name, draft.locale, "Советник")
        T.SetPreviewText(question, draft.locale, "Куда идти дальше?")
        T.SetPreviewText(title, draft.locale, "Отнеси приказы в Подгород.", draft.fontSize, 2)
        T.SetPreviewText(prose, draft.locale, "Там можно сдать {1}два задания{2} за одну поездку. В журнале указаны оба получателя.",
            draft.fontSize, spacing, "|cff" .. colors.quest, "|r")
        title:SetWidth(textWidth)
        prose:SetWidth(textWidth)
        question:SetWidth(math.max(40, width - 32))
        local top = tracking and 37 or math.max(65, 47 + question:GetStringHeight())
        local x = tracking and 27 or 12
        question:SetPoint("TOPRIGHT", -16, -38)
        question:SetShown(not tracking)
        bubble:SetShown(not tracking)
        bubble:SetColorTexture(rgb(colors.quote))
        mark:SetPoint("TOPLEFT", 10, -top)
        mark:SetShown(tracking)
        title:SetPoint("TOPLEFT", x, -top)
        title:SetTextColor(rgb(colors.strong))
        prose:SetTextColor(rgb(colors.prose))
        prose:SetPoint("TOPLEFT", title, "BOTTOMLEFT", 0, -6)
        body:SetColorTexture(rgb(colors.page))
        -- Use the same reading-surface floor as the actual chat.
        body:SetAlpha(0.75 + draft.opacity * 0.25)
        bar.fill:SetAlpha(draft.opacity)
        for _, part in ipairs(edge) do Kit.Paint(part, "line-strong") end
        local height = top + title:GetStringHeight() + 6 + prose:GetStringHeight() + 16
        answer:SetHeight(height)
        local sceneHeight = math.max(218, math.ceil(height * zoom) + 26)
        self:SetHeight(sceneHeight)
        return sceneHeight
    end
    return scene
end

function T.BuildAppearanceSettings(page, change)
    local heading = Kit.Text(page, "t-title", "gold")
    T.BindLabel(heading, "Вид переписки")
    heading:SetPoint("TOPLEFT", 2, 0)
    local options, sliders = {}, {}
    for i, theme in ipairs(themes) do
        options[i] = swatch(page, theme[1], theme[2], function(value) change("appearance", value) end)
    end
    local rule = page:CreateTexture(nil, "BORDER")
    Kit.Paint(rule, "edge")
    rule:SetHeight(1)
    local controls = {
        { "Размер текста", "fontSize", 15, 23, 1, 1, function(v) return T.L("{1} пт",v) end },
        { "Межстрочный интервал", "lineHeight", 120, 180, 5, 100, function(v) return string.format("%.2f",v/100) end },
        { "Масштаб окна", "scale", 75, 150, 5, 100, "%" },
        { "Непрозрачность", "opacity", 50, 100, 5, 100, "%" },
    }
    for i, spec in ipairs(controls) do
        local label = Kit.Text(page, "t-ui", "ink")
        T.BindLabel(label, spec[1])
        local bar = Kit.Slider(page, spec[3], spec[4], spec[5], function(value)
            change(spec[2], value / spec[6])
        end, true, spec[7])
        sliders[i] = { label = label, bar = bar, spec = spec }
    end
    local sampleName = Kit.Text(page, "t-title", "gold")
    T.BindLabel(sampleName, "Предпросмотр")
    local sample = preview(page)
    function page:Refresh(draft, width)
        local wide = width >= 535
        local left = wide and math.floor(width * .43) or width
        local right = wide and width - left - 20 or width
        local cell = (left - 14) / 3
        for i, button in ipairs(options) do
            button:SetPoint("TOPLEFT", 2 + (i - 1) * (cell + 5), -26)
            button:SetWidth(cell)
            button:Select(draft.appearance == button.value)
        end
        rule:ClearAllPoints()
        rule:SetPoint("TOPLEFT", 2, -112)
        rule:SetWidth(left - 4)
        for i, row in ipairs(sliders) do
            local y = 124 + (i - 1) * 56
            row.label:SetPoint("TOPLEFT", 3, -y)
            row.bar:SetPoint("TOPLEFT", 3, -y - 18)
            row.bar:SetWidth(left - 5)
            row.bar:SetValue(math.floor(draft[row.spec[2]] * row.spec[6] + .5))
        end
        local x, y = wide and left + 20 or 1, wide and 0 or 354
        sampleName:SetPoint("TOPLEFT", x, -y)
        sample:SetPoint("TOPLEFT", x, -y - 26)
        sample:SetWidth(right - 2)
        local sceneHeight = sample:Refresh(draft, right - 2)
        self:SetHeight(math.max(350, y + 31 + sceneHeight))
    end
end
