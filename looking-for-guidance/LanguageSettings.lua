local _, T = ...
local Kit = T.Kit

-- An example dialog, separate from the player's saved conversations.
function T.BuildLanguageSettings(page, choose)
    local title = Kit.Text(page, "t-title", "gold")
    T.BindLabel(title, "Язык интерфейса")
    title:SetPoint("TOPLEFT", 2, 0)
    local list = Kit.Panel(CreateFrame("Frame", nil, page), "well")
    local scroll = Kit.Scroll(list)
    scroll:SetPoint("TOPLEFT", 4, -4)
    scroll:SetPoint("BOTTOMRIGHT", -5, 4)
    local choices, gameName = {}, nil
    local gameCode = T.GetUILocale("auto")
    for i, locale in ipairs(T.LocaleOptions) do
        local row = Kit.Option(scroll.child, function(self) choose(self.code) end)
        row.code = locale.code
        row:SetPoint("TOPLEFT", 2, -(i-1)*25)
        row:SetHeight(24)
        row.text:SetPoint("RIGHT", -4, 0)
        row.text:SetJustifyH("LEFT")
        row.background = Kit.Fill(row, "surface-3")
        row.background:Hide()
        choices[i] = row
        if locale.code == gameCode then gameName = locale.name end
    end
    scroll.child:SetHeight(#choices * 25)
    local client = Kit.Text(page, "t-ui", "ink-2")
    client:SetWordWrap(true)

    local caption = Kit.Text(page, "t-title", "gold")
    T.BindLabel(caption, "Предпросмотр")
    local sample = Kit.Panel(CreateFrame("Frame", nil, page), "well")
    local heading = Kit.Text(sample, "t-title", "gold")
    heading:SetPoint("TOPLEFT", 12, -10)
    local question = Kit.Text(sample, "t-ui", "ink-2")
    question:SetPoint("TOPRIGHT", -12, -40)
    question:SetWordWrap(true)
    question:SetJustifyH("RIGHT")
    local speaker = Kit.Text(sample, "t-meta", "gold")
    speaker:SetPoint("TOPLEFT", question, "BOTTOMLEFT", 0, -13)
    local answer = Kit.Text(sample, "t-body", "ink")
    answer:SetWordWrap(true)
    answer:SetPoint("TOPLEFT", speaker, "BOTTOMLEFT", 0, -4)
    local composer = Kit.Text(sample, "t-ui", "ink-3")
    composer:SetWordWrap(true)
    composer:SetPoint("TOPLEFT", answer, "BOTTOMLEFT", 0, -14)
    local hint = Kit.Text(page, "t-ui", "ink-2")
    T.BindLabel(hint, "Советник отвечает на языке сообщения игрока.")
    hint:SetWordWrap(true)

    function page:Refresh(draft, width)
        local wide = width >= 515
        local left = wide and math.floor(width * .39) or width - 4
        local right = wide and width - left - 18 or width - 4
        local listHeight = wide and 237 or 141
        list:SetPoint("TOPLEFT", 2, -30)
        list:SetSize(left, listHeight)
        scroll.child:SetWidth(math.max(1, left - 12))
        for i, row in ipairs(choices) do
            local choice = T.LocaleOptions[i]
            local label = choice.code == "auto" and T.L(choice.name) or choice.name
            row:SetWidth(math.max(1, left - 16))
            row:Set(label, row.code == draft.locale, true)
            T.SetContent(row.text, label)
            row.background:SetShown(row.code == draft.locale)
        end
        client:SetWidth(left)
        local language = (gameName or gameCode) .. " (" .. gameCode .. ")"
        T.SetContent(client, T.L("Язык игры: {1}", language))
        client:SetPoint("TOPLEFT", 3, -listHeight - 38)
        local previewY = wide and 0 or listHeight + 53 + client:GetStringHeight()
        local previewX = wide and left + 16 or 2
        caption:SetPoint("TOPLEFT", previewX, -previewY)
        sample:SetPoint("TOPLEFT", previewX, -previewY - 30)
        sample:SetWidth(right)
        local inward = math.max(50, right - 24)
        question:SetWidth(inward)
        answer:SetWidth(inward)
        composer:SetWidth(inward)
        T.SetPreviewText(heading, draft.locale, "Чат")
        T.SetPreviewText(speaker, draft.locale, "Советник")
        T.SetPreviewText(composer, draft.locale, "Спроси про игру…")
        T.SetPreviewText(question, draft.locale, "Куда идти дальше?")
        T.SetPreviewText(answer, draft.locale, "Отнеси приказы в Подгород.")
        local sampleHeight = math.max(170, 85 + question:GetStringHeight() + speaker:GetStringHeight() + answer:GetStringHeight() + composer:GetStringHeight())
        sample:SetHeight(sampleHeight)
        hint:SetWidth(right)
        hint:SetPoint("TOPLEFT", previewX, -previewY - sampleHeight - 40)
        self:SetHeight(math.max(listHeight + 54 + client:GetStringHeight(), previewY + sampleHeight + 54 + hint:GetStringHeight()))
    end
end
