local _, T = ...

local Kit, Format, Store, L = T.Kit, T.Format, T.Store, T.L
local List = {}
T.List = List

local ROW = 52
local HEAD = Kit.size["control-lg"]
local PAD = Kit.space["space-4"]

local function row(child, onSelect)
    local button = CreateFrame("Button", nil, child)
    button:SetHeight(ROW)
    button.hover = Kit.Fill(button, "surface-1")
    button.hover:Hide()
    button.current = Kit.Fill(button, "surface-2", "BACKGROUND", 1)
    button.rule = Kit.Hairline(button, "line", "TOP", { indent = PAD })
    button.mark = button:CreateTexture(nil, "ARTWORK")
    button.mark:SetSize(Kit.space["space-2"], Kit.space["space-2"])
    button.mark:SetPoint("TOPLEFT", Kit.space["space-3"], -(ROW / 2 - Kit.space["space-4"] + Kit.space["space-1"]))
    button.spinner = Kit.Spinner(button)
    button.spinner:SetPoint("LEFT", button.mark, "LEFT", 0, 0)
    button.title = Kit.Text(button, "t-ui", "ink")
    button.title:SetPoint("TOPLEFT", Kit.space["space-8"], -(ROW / 2 - Kit.space["space-4"]))
    button.title:SetPoint("RIGHT", -PAD, 0)
    button.title:SetHeight(Kit.space["space-4"])
    button.title:SetJustifyV("MIDDLE")
    button.title:SetWordWrap(false)
    button.status = Kit.Text(button, "t-meta", "ink-3")
    button.status:SetPoint("TOPLEFT", button.title, "BOTTOMLEFT")
    button.status:SetPoint("RIGHT", -PAD, 0)
    button.status:SetHeight(Kit.space["space-4"])
    button.status:SetJustifyV("MIDDLE")
    button.status:SetWordWrap(false)
    button:SetScript("OnEnter", function(self) self.hover:SetShown(not self.isCurrent) end)
    button:SetScript("OnLeave", function(self) self.hover:Hide() end)
    button:SetScript("OnClick", function(self) onSelect(self.id) end)
    Kit.Tip(button, button.title)
    return button
end

local function heading(child)
    local text = Kit.Text(child, "t-ui-strong", "ink-2")
    text:SetHeight(Kit.space["space-4"])
    text:SetJustifyV("MIDDLE")
    text:SetWordWrap(false)
    return text
end

local function sorted(order)
    local threads = Store.Threads()
    if order == "recent" then
        table.sort(threads, function(a, b) return (a.updated or 0) > (b.updated or 0) end)
    end
    return threads
end

local function grouped(threads, groupBy, now)
    if groupBy == "none" then
        return { { project = "", list = threads } }
    end
    local groups, seen = {}, {}
    local today, yesterday
    if groupBy == "day" then
        local noon = date("*t", now)
        noon.hour, noon.min, noon.sec = 12, 0, 0
        today = date("%Y-%m-%d", now)
        yesterday = date("%Y-%m-%d", time(noon) - 86400)
    end
    for _, thread in ipairs(threads) do
        local project = thread.project or ""
        if groupBy == "day" then
            local stamp = date("%Y-%m-%d", math.floor(thread.updated or now))
            project = L(stamp == today and "Сегодня" or stamp == yesterday and "Вчера" or "Раньше")
        end
        if not seen[project] then
            seen[project] = { project = project, list = {} }
            table.insert(groups, seen[project])
        end
        table.insert(seen[project].list, thread)
    end
    return groups
end

function List.Build(parent, onSelect, onNew, onClose)
    local list = CreateFrame("Frame", nil, parent)
    local head = CreateFrame("Frame", nil, list)
    head:SetPoint("TOPLEFT")
    head:SetPoint("TOPRIGHT")
    head:SetHeight(85)
    Kit.Hairline(head, "line", "BOTTOM")
    local scroll = Kit.Scroll(list)
    scroll:SetPoint("TOPLEFT", head, "BOTTOMLEFT")
    scroll:SetPoint("BOTTOMRIGHT")
    local rows, headings, shown = {}, {}, {}

    local create = T.JournalSkin.Tab(head, "Новый чат", onNew)
    create:SetPoint("TOPLEFT", 7, -6)
    create:SetPoint("TOPRIGHT", -32, -6)
    create:SetHeight(23)
    create:SetActive(true)
    local close = Kit.Tool(head, "close", onClose)
    close:SetPoint("TOPRIGHT", -3, -5)
    close:HookScript("OnEnter", function(self)
        GameTooltip:SetOwner(self, "ANCHOR_TOP")
        GameTooltip:SetText(L("Скрыть список чатов"))
        GameTooltip:Show()
    end)
    local portrait = head:CreateTexture(nil, "ARTWORK")
    portrait:SetSize(23, 23)
    portrait:SetPoint("TOPLEFT", 8, -35)
    local character = Kit.Text(head, "t-ui-strong", "gold")
    character:SetPoint("LEFT", portrait, "RIGHT", 5, 0)
    character:SetPoint("RIGHT", -5, 0)
    character:SetWordWrap(false)
    local function player()
        local name = UnitName("player")
        if not (issecretvalue and issecretvalue(name)) and type(name) == "string" then T.SetContent(character, Format.Escape(name)) end
        if SetPortraitTexture then SetPortraitTexture(portrait, "player") end
    end
    list:HookScript("OnShow", player)
    player()
    local sort = Kit.Link(head, "t-meta", "ink-3", "ink-2", "down")
    sort:SetPoint("BOTTOMLEFT", Kit.space["space-3"], 5)
    local menu = Kit.Popover(sort)
    menu:SetPoint("TOPLEFT", head, "BOTTOMLEFT", Kit.space["space-2"], -Kit.space["space-1"])
    sort:SetScript("OnClick", function()
        if menu:IsShown() then
            Kit.Leave(menu)
        else
            Kit.Rise(menu)
        end
    end)
    menu:HookScript("OnShow", function() sort:SetHeld(true) end)
    menu:HookScript("OnHide", function() sort:SetHeld(false) end)

    local options, y = {}, Kit.space["space-2"]
    local function label(text)
        Kit.Label(menu, text):SetPoint("TOPLEFT", Kit.space["space-3"], -y)
        y = y + Kit.size.control
    end
    local function option(key, value, text)
        local button = Kit.Option(menu, function()
            T.settings.list[key] = value
            list:Render(list.current, time())
        end)
        button:SetPoint("TOPLEFT", Kit.space["space-3"], -y)
        button:SetPoint("RIGHT", -Kit.space["space-3"], 0)
        button.key, button.value, button.name = key, value, text
        table.insert(options, button)
        y = y + Kit.size.control
    end
    label("Группировка")
    option("group", "day", "По дате")
    option("group", "project", "По папкам")
    option("group", "none", "Без группировки")
    y = y + Kit.space["space-2"]
    label("Сортировка")
    option("sort", "recent", "Недавние")
    option("sort", "needs", "Требуют внимания")
    menu:SetSize(168, y + Kit.space["space-3"])

    function list:Render(current, now)
        self.current = current
        close:SetShown(parent.overlay == true)
        create:SetPoint("TOPRIGHT", parent.overlay and -32 or -7, -6)
        local choice = T.settings.list
        local byProject = choice.group == "project"
        sort:SetLabel(L(byProject and "Папки" or choice.group == "day" and "По дате" or "Все") .. " · " .. L(choice.sort == "recent" and "Недавние" or "Требуют внимания"))
        for _, button in ipairs(options) do
            button:Set(L(button.name), choice[button.key] == button.value)
        end

        local top, used, labels, previous = 0, 0, 0, nil
        wipe(shown)
        for _, group in ipairs(grouped(sorted(choice.sort), choice.group, now)) do
            if group.project ~= "" then
                labels = labels + 1
                local text = headings[labels] or heading(scroll.child)
                headings[labels] = text
                T.SetContent(text, Format.Escape(group.project))
                text:ClearAllPoints()
                text:SetPoint("TOPLEFT", Kit.space["space-8"], -(top + HEAD - Kit.space["space-1"] - Kit.space["space-4"]))
                text:SetPoint("RIGHT", -PAD, 0)
                text:Show()
                table.insert(shown, text)
                top = top + HEAD
                previous = nil
            end
            for _, thread in ipairs(group.list) do
                used = used + 1
                local button = rows[used] or row(scroll.child, onSelect)
                rows[used] = button
                button.id = thread.id
                button.isCurrent = thread.id == current
                button.current:SetShown(button.isCurrent)
                button.rule:SetShown(previous ~= nil and not button.isCurrent and not previous.isCurrent)
                button.title:SetFontObject(Kit.Font(Store.IsUnread(thread) and "t-ui-strong" or "t-ui"))
                T.SetContent(button.title, Format.Escape(Store.Title(thread)))
                local status = Format.Agent(thread) .. " · " .. Format.Status(thread, now)
                if choice.group == "none" and thread.project then
                    status = status .. " · " .. thread.project
                end
                button.status:SetText(Format.Escape(status))
                local rank = Store.Rank(thread)
                button.mark:SetShown(rank == 0)
                Kit.Paint(button.mark, thread.state == "error" and "agent-error" or "agent-needs")
                button.spinner:SetShown(rank == 1)
                button:ClearAllPoints()
                button:SetPoint("TOPLEFT", scroll.child, "TOPLEFT", 0, -top)
                button:SetPoint("RIGHT", scroll.child, "RIGHT")
                button:Show()
                table.insert(shown, button)
                top = top + ROW
                previous = button
            end
        end
        for index = used + 1, #rows do
            rows[index]:Hide()
        end
        for index = labels + 1, #headings do
            headings[index]:Hide()
        end
        scroll.child:SetHeight(top + Kit.space["space-2"])
    end

    function list:Reveal()
        for index, region in ipairs(shown) do
            Kit.Rise(region, Kit.Stagger(index))
        end
    end

    return list
end
