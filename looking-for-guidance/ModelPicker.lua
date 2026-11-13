local _, T = ...
local Kit, Format, Actions, Store = T.Kit, T.Format, T.Actions, T.Store
local Picker = {}
T.ModelPicker = Picker

local names = {
    low = "Низкий", medium = "Средний", high = "Высокий",
    xhigh = "Очень высокий", max = "Максимальный", ultra = "Ультра",
}
local WIDTH, ROW, FIRST = 266, 25, 5

function Picker.Build(owner)
    local panel = Kit.Popover(owner)
    panel:SetWidth(WIDTH)
    panel:SetPoint("BOTTOMRIGHT", owner, "TOPRIGHT", 0, 6)
    local heading = Kit.Text(panel, "t-title", "gold")
    local caption = Kit.Text(panel, "t-meta", "ink-3")
    local back = Kit.Key(panel, "‹")
    back:SetSize(23, 22)
    back:SetPoint("TOPLEFT", 8, -9)
    local scroll = Kit.Scroll(panel)
    scroll:SetPoint("TOPLEFT", 8, -51)
    scroll:SetPoint("TOPRIGHT", -8, -51)
    scroll.child:SetWidth(WIDTH - 16)
    Kit.Hairline(scroll, "edge", "TOP")
    local nextLevel = Kit.Link(panel, "t-ui", "ink-2", "gold", "down")
    nextLevel:SetPoint("BOTTOMLEFT", 14, 10)
    nextLevel:SetPoint("BOTTOMRIGHT", -14, 10)

    local rows, mode, expanded = {}, "model", false
    local function change(self)
        if self.kind == "more" then
            expanded = not expanded
        elseif self.kind == "model" then
            mode = "effort"
            Actions.Choose("model", self.value, panel.thread)
            local model = Actions.Choice(panel.thread).model
            if not model or #(model.efforts or {}) == 0 then mode = "model" end
        elseif self.kind == "effort" then
            mode = "model"
            Actions.Choose("effort", self.value, panel.thread)
        end
        panel:Sync(true)
        scroll:ScrollTo(0, true)
    end
    local function row(i, label, selected, kind, value)
        local button = rows[i]
        if not button then
            button = Kit.Option(scroll.child, change)
            button.text:SetPoint("RIGHT", -7, 0)
            button.text:SetJustifyH("LEFT")
            button.text:SetWordWrap(false)
            Kit.Tip(button, button.text)
            button.check = Kit.Glyph(button, "check", "gold")
            button.check:SetSize(11, 11)
            button.check:SetPoint("LEFT", -3, 0)
            rows[i] = button
        end
        button.kind, button.value = kind, value
        button:SetPoint("TOPLEFT", 8, -(i-1)*ROW)
        button:SetPoint("TOPRIGHT", -4, -(i-1)*ROW)
        button:SetHeight(ROW)
        button:Set(Format.Escape((kind == "model") and label or T.L(label)), selected, kind ~= "empty")
        button.mark:Hide()
        button.check:SetShown(selected)
        button:Show()
        return i + 1
    end
    local function allowedModels()
        return Actions.Models(panel.thread)
    end
    function panel:Sync(force)
        if not self:IsShown() then return end
        local choice = Actions.Choice(panel.thread)
        local model = choice.model
        if not force and self.catalog == Store.models and self.selected == model and self.effort == choice.effort then return end
        self.catalog, self.selected, self.effort = Store.models, model, choice.effort
        local effortPage = mode == "effort" and model and #(model.efforts or {}) > 0
        if not effortPage then mode = "model" end
        self.mode = mode
        local x = effortPage and 40 or 14
        heading:SetPoint("TOPLEFT", x, -8)
        heading:SetText(T.L(effortPage and "Уровень рассуждения" or "Модель"))
        caption:SetPoint("TOPLEFT", x, -31)
        local scope = T.L(panel.thread and "Для следующего ответа" or "Для нового чата")
        caption:SetText(effortPage and Format.Escape(model.label or model.id) .. " · " .. scope or scope)
        back:SetShown(not not effortPage)
        local i = 1
        if effortPage then
            for _, effort in ipairs(model.efforts) do
                local label = names[effort] and (T.L(names[effort]) .. "  ·  " .. effort) or effort
                i = row(i, label, effort == choice.effort, "effort", effort)
            end
        else
            local models = allowedModels()
            for index, m in ipairs(models) do
                if index == FIRST + 1 then
                    i = row(i, expanded and "Свернуть список" or T.L("Ещё модели ({1})", #models - FIRST), false, "more")
                    if not expanded then break end
                end
                i = row(i, m.label or m.id, m == model, "model", m.id)
            end
            if i == 1 then i = row(i, "Список моделей пока недоступен", false, "empty") end
        end
        for other = i, #rows do rows[other]:Hide() end
        local hasLevels = not effortPage and model and #(model.efforts or {}) > 0
        nextLevel:SetShown(not not hasLevels)
        if hasLevels then
            nextLevel.text:SetText(T.L("Глубина рассуждения") .. "  ·  " .. Format.Escape(T.L(names[choice.effort] or choice.effort or "Выбрать")))
        end
        local content = (i-1)*ROW
        local visible = math.min(content, 7*ROW)
        scroll.child:SetHeight(content)
        scroll:SetHeight(visible)
        panel:SetHeight(51 + visible + (hasLevels and 37 or 8))
    end
    function panel:Toggle()
        if self:IsShown() then self:Hide() return end
        mode, expanded = "model", false
        local chosen = Actions.Choice(panel.thread).model
        for index, model in ipairs(allowedModels()) do
            if model == chosen and index > FIRST then expanded = true end
        end
        self:Show()
        self:Sync(true)
        scroll:ScrollTo(0, true)
    end
    back:SetScript("OnClick", function() mode = "model" panel:Sync(true) scroll:ScrollTo(0,true) end)
    nextLevel:SetScript("OnClick", function() mode = "effort" panel:Sync(true) scroll:ScrollTo(0,true) end)
    owner:HookScript("OnHide", function() panel:Hide() end)
    panel.rows, panel.back, panel.level = rows, back, nextLevel
    return panel
end
