local _, T = ...
local Kit = T.Kit

-- The row is the hit target. The native check art stays square even for wrapped labels.
local function checkRow(parent, label, hint, toggle)
    local row = CreateFrame("CheckButton", nil, parent)
    local art = CreateFrame("Frame", nil, row)
    art:SetSize(24, 24)
    art:SetPoint("TOPLEFT", 0, 1)
    local function icon(file, layer)
        local texture = art:CreateTexture(nil, layer)
        texture:SetTexture("Interface\\Buttons\\UI-CheckBox-" .. file)
        texture:SetAllPoints()
        return texture
    end
    icon("Up", "BACKGROUND")
    row.mark = icon("Check", "ARTWORK")
    row.text = Kit.Text(row, "t-ui", "ink")
    T.BindLabel(row.text, label)
    row.text:SetWordWrap(true)
    row.text:SetPoint("TOPLEFT", 27, -1)
    row.note = Kit.Text(row, "t-meta", "ink-3")
    T.BindLabel(row.note, hint or "")
    row.note:SetWordWrap(true)
    row.note:SetPoint("TOPLEFT", row.text, "BOTTOMLEFT", 0, -2)
    row:SetHighlightTexture("Interface\\QuestFrame\\UI-QuestTitleHighlight", "ADD")
    row:SetScript("OnClick", toggle)
    function row:Place(x, y, width)
        self:SetPoint("TOPLEFT", x, -y)
        self:SetWidth(width)
        self.text:SetWidth(math.max(20, width - 29))
        self.note:SetWidth(math.max(20, width - 29))
        local height = math.max(28, self.text:GetStringHeight() + (hint and self.note:GetStringHeight() + 5 or 0) + 7)
        self:SetHeight(height)
        return height
    end
    function row:Check(on, enabled)
        self:SetChecked(on and true or false)
        self.mark:SetShown(on and true or false)
        self:SetEnabled(enabled ~= false)
        self:SetAlpha(enabled == false and .52 or 1)
    end
    return row
end

local groups = {
    { "Персонаж и вещи", { "character", "Персонаж", "Уровень, класс и характеристики" },
        { "gear", "Экипировка", "Надетые предметы и их свойства" }, { "bags", "Сумки", "Предметы и количество" } },
    { "Способности", { "talents", "Таланты", "Очки и доступные таланты" }, { "spells", "Заклинания", "Книга заклинаний и ранги" },
        { "skills", "Навыки", "Умения и профессии" }, { "buffs", "Эффекты", "Благотворные и вредоносные" } },
    { "Мир и задания", { "quests", "Журнал заданий", "Цели и ход выполнения" }, { "location", "Местность", "Зона и координаты" },
        { "target", "Цель", "Выбранное существо или игрок" }, { "reputation", "Репутация", "Отношения с фракциями" } },
}

function T.BuildDataSettings(page, getDraft, changed, preview)
    local master = checkRow(page, "Прикладывать снимок персонажа", "К каждому вопросу — выбранные данные из игры. Помощник читает нужные разделы.", function()
        local draft = getDraft()
        draft.extras = not draft.extras
        changed()
    end)
    local count = Kit.Text(page, "t-ui-strong", "gold")
    local selectAll = Kit.Link(page, "t-ui", "accent", "ink")
    selectAll:SetLabel("Выбрать все")
    local deselect = Kit.Link(page, "t-ui", "accent", "ink")
    deselect:SetLabel("Снять все")
    local function all(value)
        for _, part in ipairs(T.Context.parts) do getDraft().context[part.key] = value end
        changed()
    end
    selectAll:SetScript("OnClick", function() all(true) end)
    deselect:SetScript("OnClick", function() all(false) end)
    local sample = Kit.Key(page, "Посмотреть данные")
    sample:SetScript("OnClick", function() preview(getDraft()) end)
    local boxes = {}
    for _, group in ipairs(groups) do
        local box = Kit.Panel(CreateFrame("Frame", nil, page), "well")
        box.heading = Kit.Text(box, "t-ui-strong", "gold")
        box.heading:SetPoint("TOPLEFT", 9, -10)
        T.BindLabel(box.heading, group[1])
        box.options = {}
        for i = 2, #group do
            local part = group[i]
            local item = checkRow(box, part[2], part[3], function()
                local d = getDraft()
                d.context[part[1]] = not d.context[part[1]]
                changed()
            end)
            item.key = part[1]
            box.options[#box.options + 1] = item
        end
        boxes[#boxes + 1] = box
    end
    function page:Refresh(draft, width)
        local total = 0
        for _, part in ipairs(T.Context.parts) do
            if draft.context[part.key] then total = total + 1 end
        end
        local n = #T.Context.parts
        master:Check(draft.extras)
        count:SetText(draft.extras and T.L("Выбрано {1} из {2}", total, n) or T.L("Доступ выключен"))
        local masterHeight = master:Place(1, 0, width - 2)
        count:SetPoint("TOPLEFT", 2, -masterHeight - 11)
        local actionsY = width < 414 and masterHeight + 37 or masterHeight + 11
        selectAll:SetPoint("TOPRIGHT", -deselect:GetWidth() - 13, -actionsY)
        deselect:SetPoint("TOPRIGHT", -2, -actionsY)
        for _, item in ipairs({ { selectAll, total ~= n }, { deselect, total ~= 0 } }) do
            local enabled = draft.extras and item[2]
            item[1]:SetEnabled(enabled)
            item[1]:SetAlpha(enabled and 1 or .48)
        end
        local y = actionsY + 32
        local cols = width >= 565 and 3 or width >= 422 and 2 or 1
        local gap = 9
        local span = (width - (cols - 1) * gap) / cols
        local maxHeight = 0
        for index, box in ipairs(boxes) do
            local col = (index - 1) % cols
            if index > 1 and col == 0 then
                y, maxHeight = y + maxHeight + gap, 0
            end
            box:SetPoint("TOPLEFT", col * (span + gap), -y)
            box:SetWidth(span)
            box.heading:SetAlpha(draft.extras and 1 or .55)
            local height = 38
            for _, option in ipairs(box.options) do
                option:Check(draft.context[option.key], draft.extras)
                height = height + option:Place(8, height, span - 16) + 4
            end
            box:SetHeight(height + 4)
            maxHeight = math.max(maxHeight, height + 4)
        end
        y = y + maxHeight + 12
        sample:SetEnabled(draft.extras and total > 0)
        sample:SetPoint("TOPRIGHT", -2, -y)
        self:SetHeight(y + sample:GetHeight() + 8)
    end
end

-- How journal research starts and how much of it a day may run. Each job spends the player's own allowance.
function T.BuildAdviserSettings(page, getDraft, changed)
    local Insights = T.Insights
    local title = Kit.Text(page, "t-title", "gold")
    title:SetPoint("TOPLEFT", 2, 0)
    T.BindLabel(title, "Исследования журнала")
    local mode = Kit.Segmented(page, { { value = "click", label = "Только по кнопке" }, { value = "auto", label = "Автоматически" } }, function(value)
        getDraft().research = value
        changed()
    end)
    local hint = Kit.Text(page, "t-meta", "ink-3")
    hint:SetWordWrap(true)
    hint:SetJustifyH("LEFT")
    local capLabel = Kit.Text(page, "t-ui", "ink")
    T.BindLabel(capLabel, "Исследований в сутки, не больше")
    local cap = Kit.Slider(page, Insights.CAP_MIN, Insights.CAP_MAX, 10, function(value)
        getDraft().researchCap = value
        changed()
    end, true, "")
    -- The adviser itself is chosen on the computer: the page only says which one answers.
    local adviser = Kit.Text(page, "t-ui", "ink")
    local where = Kit.Text(page, "t-meta", "ink-3")
    where:SetWordWrap(true)
    where:SetJustifyH("LEFT")
    T.BindLabel(where, "Сменить советника можно в меню Looking for Guidance на компьютере.")
    function page:Refresh(draft, width)
        local span = math.min(width - 4, 380)
        local y = 30
        mode:SetPoint("TOPLEFT", 2, -y)
        mode:SetWidth(span)
        mode:SetValue(draft.research)
        y = y + mode:GetHeight() + 9
        T.BindLabel(hint, draft.research == "auto"
            and "Советник сам готовит сводку исправлений и советы по классу после входа в игру, а остальные разделы — когда ты их открываешь. Каждое исследование расходует лимит твоей подписки."
            or "Советник готовит раздел, только когда ты нажмёшь в нём кнопку. Каждое исследование расходует лимит твоей подписки.")
        hint:SetPoint("TOPLEFT", 3, -y)
        hint:SetWidth(width - 8)
        y = y + hint:GetStringHeight() + 20
        capLabel:SetPoint("TOPLEFT", 3, -y)
        cap:SetPoint("TOPLEFT", 3, -y - 18)
        cap:SetWidth(span)
        cap:SetValue(draft.researchCap)
        y = y + 18 + cap:GetHeight() + 20
        local agent = T.Store.agent
        adviser:SetShown(agent ~= nil)
        if agent then
            adviser:SetText(T.L("Советник: {1}", agent == "claude" and "Claude Code" or "Codex"))
            adviser:SetPoint("TOPLEFT", 3, -y)
            y = y + adviser:GetStringHeight() + 4
        end
        where:SetPoint("TOPLEFT", 3, -y)
        where:SetWidth(width - 8)
        self:SetHeight(y + where:GetStringHeight() + 8)
    end
end

function T.BuildNoticeSettings(page, getDraft, changed)
    local title = Kit.Text(page, "t-title", "gold")
    title:SetPoint("TOPLEFT", 2, 0)
    T.BindLabel(title, "Уведомления")
    local rows = {}
    for _, item in ipairs({
        { "autoAdvice", "Автосоветы в обзоре" },
        { "alertChat", "Сообщение в игровом чате" },
        { "alertSound", "Звук, когда ответ готов" },
        { "minimap", "Кнопка помощника у мини-карты" }
    }) do
        local box = checkRow(page, item[2], nil, function()
            local d = getDraft()
            d[item[1]] = not d[item[1]]
            changed()
        end)
        box.key = item[1]
        rows[#rows + 1] = box
    end
    local test = Kit.Key(page, "Проверить уведомление")
    local example = Kit.Text(page, "t-ui", "gold")
    example:SetWordWrap(true)
    example:Hide()
    test:SetScript("OnClick", function()
        local draft = getDraft()
        if not draft then return end
        example:SetShown(draft.alertChat)
        if draft.alertSound then PlaySound(SOUNDKIT.TELL_MESSAGE) end
    end)
    page:HookScript("OnHide", function() example:Hide() end)
    function page:Refresh(draft, width)
        local y = 38
        for _, box in ipairs(rows) do
            box:Check(draft[box.key])
            y = y + box:Place(2, y, width - 4) + 7
        end
        test:SetPoint("TOPLEFT", 2, -y - 5)
        test:SetEnabled(draft.alertChat or draft.alertSound)
        T.BindLabel(example, "[Советник] Ответ готов.")
        if not draft.alertChat then example:Hide() end
        example:SetPoint("TOPLEFT", test, "BOTTOMLEFT", 0, -8)
        example:SetWidth(width - 8)
        self:SetHeight(y + 42 + example:GetStringHeight())
    end
end
