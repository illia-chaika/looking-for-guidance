local _, T = ...

local Kit, Format, Store = T.Kit, T.Format, T.Store
local Transcript = {}
T.Transcript = Transcript

local PAD = Kit.space["space-4"]
local TURN = Kit.space["space-4"]
local GAP = Kit.space["space-2"]

local function sourceOf(block, link)
    local index = link:match("^addon:looking%-for%-guidance:source:(%d+)$")
    return index and block.sources and block.sources[tonumber(index)]
end

local function linked(block)
    block:SetHyperlinksEnabled(true)
    block:SetScript("OnHyperlinkEnter", function(self, link)
        GameTooltip:SetOwner(self, "ANCHOR_CURSOR")
        local source = sourceOf(self, link)
        if source then
            GameTooltip:SetText(source.host)
            GameTooltip:AddLine(T.L("Нажми, чтобы скопировать адрес"))
        elseif link:find("^addon:looking%-for%-guidance:waypoint:") then
            GameTooltip:SetText(T.L("Метка на карте"))
        else
            if not pcall(GameTooltip.SetHyperlink, GameTooltip, link) then GameTooltip:Hide(); return end
        end
        GameTooltip:Show()
    end)
    block:SetScript("OnHyperlinkLeave", function() GameTooltip:Hide() end)
    block:SetScript("OnHyperlinkClick", function(self, link, text, button)
        if not link:find("^addon:") and T.GameLinks.InsertFocused(text) then return end
        local waypoint = link:match("^addon:looking%-for%-guidance:waypoint:(%d+)$")
        local quest = link:match("^quest:(%d+):")
        local source = sourceOf(self, link)
        if source then
            self.showSource(source)
        elseif waypoint then
            if not T.Context.MarkQuest(tonumber(waypoint)) then T.Fire("Problem", "noWaypoint") end
        elseif quest then
            if not T.Context.OpenQuest(tonumber(quest)) then T.Fire("Problem", "noQuest") end
        elseif link:match("^item:%d+") or link:match("^spell:%d+") then
            SetItemRef(link, text, button, self)
        end
    end)
    return block
end

local make = {}

function make.journal(child)
    local block = CreateFrame("Frame", nil, child)
    block.header = Kit.Link(block, "t-ui-strong", "strong", "link", "down")
    block.header.text:SetWordWrap(true)
    block.header:SetPoint("TOPLEFT")
    block.header:SetPoint("TOPRIGHT")
    block.header:SetHeight(24)
    block.body = Kit.Text(block, "t-ui", "detail")
    block.body:SetPoint("TOPLEFT", block.header, "BOTTOMLEFT", 12, -3)
    block.header:SetScript("OnClick", function() block.toggle() end)
    return block
end

function make.user(child)
    local block = CreateFrame("Frame", nil, child)
    block.bubble = linked(Kit.Panel(CreateFrame("Frame", nil, block), "quote"))
    block.bubble:SetPoint("TOPRIGHT")
    block.text = Kit.Text(block.bubble, "t-body", "ink")
    block.meta = Kit.Text(block, "t-meta", "detail")
    block.meta:SetJustifyH("RIGHT")
    block.meta:SetPoint("TOPRIGHT", block.bubble, "BOTTOMRIGHT", 0, -Kit.space["space-1"])
    block.failed = Kit.Text(block, "t-meta", "error")
    T.BindLabel(block.failed, "Нет подтверждения ·")
    block.resend = Kit.Link(block, "t-ui", "error", "strong")
    block.resend:SetLabel("Повторить")
    block.resend:SetPoint("LEFT", block.failed, "RIGHT", Kit.space["space-1"], 0)
    block.resend:SetScript("OnClick", function() block.onResend() end)
    return block
end

function make.steps(child)
    local block = CreateFrame("Frame", nil, child)
    block.summary = CreateFrame("Button", nil, block)
    block.summary:SetPoint("TOPLEFT")
    block.summary:SetHeight(24)
    block.tick = Kit.Glyph(block.summary, "check", "detail")
    block.tick:SetPoint("LEFT")
    block.text = Kit.Text(block.summary, "t-ui-strong", "detail")
    block.text:SetPoint("LEFT", block.tick, "RIGHT", GAP, 0)
    block.chevron = Kit.Glyph(block.summary, "down", "detail")
    block.chevron:SetPoint("LEFT", block.text, "RIGHT", GAP, 0)
    block.summary:SetScript("OnEnter", function() Kit.Paint(block.text, "strong") end)
    block.summary:SetScript("OnLeave", function() Kit.Paint(block.text, "detail") end)
    block.summary:SetScript("OnClick", function() block.onToggle() end)
    block.items = {}
    return block
end

function make.paragraph(child)
    local block = linked(CreateFrame("Frame", nil, child))
    block.text = Kit.Text(block, "t-body", "prose")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.item(child)
    local block = linked(CreateFrame("Frame", nil, child))
    block.mark = block:CreateTexture(nil, "ARTWORK")
    block.mark:SetSize(Kit.space["space-1"], Kit.space["space-1"])
    block.mark:SetPoint("TOPLEFT", Kit.space["space-1"], -GAP)
    Kit.Paint(block.mark, "detail")
    block.text = Kit.Text(block, "t-body", "prose")
    Kit.Place(block.text, block, Kit.space["space-4"], 0)
    return block
end

function make.heading(child)
    local block = linked(CreateFrame("Frame", nil, child))
    block.text = Kit.Text(block, "t-title", "strong")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.expand(child)
    local block = CreateFrame("Frame", nil, child)
    block.link = Kit.Link(block, "t-ui-strong", "link", "strong", "down")
    block.link:SetPoint("LEFT")
    block.link:SetScript("OnClick", function() block.toggle() end)
    return block
end

function make.quest(child)
    local block = CreateFrame("Button", nil, child)
    Kit.Hairline(block, "rule", "TOP")
    Kit.Hairline(block, "rule", "BOTTOM")
    block.icon = block:CreateTexture(nil, "ARTWORK")
    block.icon:SetSize(22, 22)
    block.icon:SetPoint("LEFT", 1, 0)
    block.icon:SetTexture("Interface\\GossipFrame\\ActiveQuestIcon")
    block.name = Kit.Text(block, "t-ui-strong", "strong")
    block.name:SetPoint("TOPLEFT", 31, -4)
    block.meta = Kit.Text(block, "t-meta", "detail")
    block.cta = Kit.Text(block, "t-ui", "link")
    block.cta:SetPoint("RIGHT", -3, 0)
    T.BindLabel(block.cta, "В журнал ›")
    block:SetScript("OnEnter", function() Kit.Paint(block.cta, "strong") end)
    block:SetScript("OnLeave", function() Kit.Paint(block.cta, "link") end)
    block:SetScript("OnClick", function() if not T.Context.OpenQuest(block.id) then T.Fire("Problem", "noQuest") end end)
    return block
end

function make.compaction(child)
    local block = CreateFrame("Frame", nil, child)
    block.tick = Kit.Glyph(block, "check", "detail")
    block.tick:SetPoint("LEFT", 0, -2)
    block.text = Kit.Text(block, "t-meta", "detail")
    block.text:SetPoint("LEFT", block.tick, "RIGHT", GAP, 0)
    T.BindLabel(block.text, "Контекст сжат")
    Kit.Hairline(block, "line", "TOP")
    return block
end

function make.note(child)
    local block = CreateFrame("Frame", nil, child)
    block.text = Kit.Text(block, "t-meta", "error")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.source(child)
    local block = CreateFrame("Frame", nil, child)
    block.text = Kit.Text(block, "t-meta", "detail")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.card(child)
    local block = Kit.Panel(CreateFrame("Frame", nil, child), "surface-1")
    block.stripe = Kit.Stripe(block, "agent-needs")
    block.title = Kit.Text(block, "t-ui-strong", "ink")
    Kit.Place(block.title, block, PAD, Kit.space["space-3"])
    block.command = Kit.Recess(CreateFrame("Frame", nil, block))
    block.commandText = Kit.Text(block.command, "t-ui", "ink")
    block.reason = Kit.Text(block, "t-meta", "ink-2")
    block.allow = Kit.Key(block, "Разрешить", true)
    block.allow:SetPoint("BOTTOMRIGHT", -Kit.space["space-3"], Kit.space["space-3"])
    block.retry = Kit.Key(block, "Повторить")
    block.retry:SetPoint("BOTTOMRIGHT", -Kit.space["space-3"], Kit.space["space-3"])
    block.retry:SetScript("OnClick", function() block.onRetry() end)
    block.deny = Kit.Key(block, "Отклонить")
    block.deny:SetPoint("RIGHT", block.allow, "LEFT", -GAP, 0)
    block.allow:SetScript("OnClick", function() block.onAnswer(true) end)
    block.deny:SetScript("OnClick", function() block.onAnswer(false) end)
    return block
end

function make.working(child)
    local block = CreateFrame("Frame", nil, child)
    block.spinner = Kit.Spinner(block)
    block.spinner:SetPoint("LEFT")
    block.text = Kit.Text(block, "t-ui", "detail")
    block.text:SetPoint("LEFT", block.spinner, "RIGHT", GAP, 0)
    block.stop = Kit.Link(block, "t-ui", "detail", "strong")
    block.stop:SetLabel("Остановить")
    block.stop:SetPoint("RIGHT")
    block.stop:SetScript("OnClick", function() block.onStop() end)
    block.clock = Kit.Text(block, "t-meta", "detail")
    block.clock:SetPoint("RIGHT", block.stop, "LEFT", -GAP, 0)
    block.text:SetPoint("RIGHT", block.clock, "LEFT", -GAP, 0)
    block.text:SetWordWrap(false)
    return block
end

function Transcript.Build(parent, handlers)
    local scroll = Kit.Scroll(parent)
    local child = scroll.child
    local pools, open, opening = {}, {}, {}
    local questionOffsets = {}
    local shown, anchor, tail, clock

    local copyBox
    local function showSource(source)
        if not copyBox then
            copyBox = Kit.CopyBox(parent)
            scroll:HookScript("OnHide", function() copyBox:Hide() end)
        end
        copyBox:Open(source.host, source.url)
    end

    local function stamp(turn)
        return turn.at and (turn.role .. "@" .. turn.at)
    end

    local function take(kind)
        local pool = pools[kind] or { used = 0 }
        pools[kind] = pool
        pool.used = pool.used + 1
        pool[pool.used] = pool[pool.used] or make[kind](child)
        local block = pool[pool.used]
        block:Show()
        return block
    end

    local function place(block, y, height)
        block:ClearAllPoints()
        block:SetPoint("TOPLEFT", child, "TOPLEFT", PAD, -y)
        block:SetPoint("RIGHT", child, "RIGHT", -PAD, 0)
        block:SetHeight(height)
        return y + height
    end

    local function measure(text, width)
        text:SetWidth(width)
        return Kit.TextHeight(text)
    end

    local function user(turn, y, width)
        local block = take("user")
        local limit = math.floor(width * 0.85)
        block.bubble.showSource = showSource
        T.SetContent(block.text, T.GameLinks and T.GameLinks.Display(turn.text, Kit.Hex("gold"), turn.references) or Format.Escape(turn.text))
        local bubble = math.min(limit, math.ceil(block.text:GetUnboundedStringWidth()) + 2 * Kit.space["space-3"] + 2)
        local height = measure(block.text, bubble - 2 * Kit.space["space-3"] - 2) + 2 * GAP + 2
        block.text:ClearAllPoints()
        Kit.Place(block.text, block.bubble, Kit.space["space-3"] + 1, GAP + 1)
        block.bubble:SetSize(bubble, height)
        local failed = turn.seq and Store.Delivery(turn.seq) == "failed"
        local summary = turn.snapshotSummary
        block.meta:SetShown(not failed and summary ~= nil and summary ~= "")
        if block.meta:IsShown() then
            T.SetContent(block.meta, T.L("Снимок") .. " · " .. Format.Escape(Format.ToolTrace(summary)))
            height = height + Kit.space["space-1"] + measure(block.meta, width)
        end
        block.failed:SetShown(failed)
        block.resend:SetShown(failed)
        block.onResend = function() handlers.resend(turn.seq) end
        if failed then
            block.failed:ClearAllPoints()
            block.failed:SetPoint("TOPRIGHT", block.bubble, "BOTTOMRIGHT", -block.resend:GetWidth() - Kit.space["space-1"], -Kit.space["space-1"])
            height = height + Kit.space["space-1"] + Kit.space["space-4"]
        end
        return block, place(block, y, height)
    end

    local function journal(turn, key, y, width)
        local block = take("journal")
        local card = turn.journal
        local title = T.L("Из полевого журнала") .. " · " .. Format.Escape(card.title or turn.text)
        T.SetContent(block.header.text, title)
        block.header.glyph:SetGlyph(open[key] and "up" or "down")
        block.body:SetShown(not not open[key])
        local height = math.max(24, measure(block.header.text, width - 24) + 4)
        block.header:SetHeight(height)
        if turn.seq and Store.Delivery(turn.seq) == "failed" then
            block.header.text:SetText(title .. " · " .. T.L("Повторить"))
            block.toggle = function() handlers.resend(turn.seq) end
        else
            block.toggle = function() open[key] = not open[key]; handlers.refresh() end
        end
        if open[key] then
            T.SetContent(block.body, T.GameLinks.Display(card.facts or "", Kit.Hex("link"), turn.references))
            height = height + measure(block.body, math.max(1, width - 15)) + 8
        end
        return block, place(block, y, height)
    end

    local function steps(turn, key, y, width)
        local block = take("steps")
        block.open = open[key]
        local summary = turn.text or T.L("Действия помощника")
        if summary:match("^Used %d+ tool") then summary = T.L("Источники · {1}", #(turn.items or {})) end
        T.SetContent(block.text, Format.Escape(Format.ToolTrace(summary)))
        local lead = block.tick:GetWidth() + GAP
        block.text:SetWidth(math.max(1, math.min(block.text:GetUnboundedStringWidth(), width - lead * 2)))
        block.text:SetWordWrap(true)
        local height = math.max(24, Kit.TextHeight(block.text) + GAP)
        block.summary:SetSize(lead * 2 + block.text:GetWidth(), height)
        block.chevron:SetGlyph(block.open and "up" or "down")
        block.chevron:SetInk("detail")
        block.tick:SetInk("detail")
        block.chevron:Show()
        block.onToggle = function()
            open[key] = not open[key]
            opening[key] = open[key]
            scroll:ScrollTo(scroll:GetVerticalScroll(), true)
            scroll.atEnd = false
            handlers.refresh()
        end
        for _, item in ipairs(block.items) do item:Hide() end
        if block.open then
            local entries = turn.events and #turn.events > 0 and turn.events or turn.items or {}
            for index, event in ipairs(entries) do
                local item = block.items[index]
                if not item then
                    item = linked(CreateFrame("Frame", nil, block))
                    item.rule = Kit.Stripe(item, "rule")
                    item.title = Kit.Text(item, "t-ui-strong", "prose")
                    item.detail = Kit.Text(item, "t-meta", "detail")
                    item.showSource = showSource
                    block.items[index] = item
                end
                local title, detail, url, failed
                if type(event) == "table" then
                    local state = ({ running = "выполняется", failed = "ошибка", partial = "частично", unavailable = "недоступно" })[event.status] or ""
                    title = Format.Escape(T.L(event.label or event.kind or "Инструмент")) .. (state ~= "" and (" · " .. T.L(state)) or "")
                    detail, url, failed = Format.Escape(event.kind == "mcp" and Format.ToolTrace(event.detail) or event.detail), event.url, event.status == "failed"
                else
                    title = Format.Escape(event)
                    if title == "Searching the web" then title = T.L("Поиск в интернете") end
                    detail = ""
                end
                local host = Format.SourceHost(url)
                item.sources = nil
                if host then
                    item.sources = { { url = url, host = host } }
                    detail = (detail ~= "" and detail .. "  " or "") .. "|c" .. Kit.Hex("link")
                        .. "|Haddon:looking-for-guidance:source:1|h[" .. host .. "]|h|r"
                end
                local inset = 12
                local measureWidth = math.max(1, width - GAP - inset)
                item.title:ClearAllPoints()
                Kit.Place(item.title, item, inset, 1)
                Kit.Paint(item.title, failed and "error" or "prose")
                T.SetContent(item.title, title)
                local rowHeight = measure(item.title, measureWidth) + 2
                item.detail:SetShown(detail ~= "")
                if detail ~= "" then
                    item.detail:ClearAllPoints()
                    Kit.Place(item.detail, item, inset, rowHeight)
                    T.SetContent(item.detail, detail)
                    rowHeight = rowHeight + measure(item.detail, measureWidth)
                end
                item:SetHeight(rowHeight)
                item:ClearAllPoints()
                item:SetPoint("TOPLEFT", GAP, -height)
                item:SetPoint("RIGHT")
                item:Show()
                if opening[key] then Kit.Rise(item, Kit.Stagger(index)) end
                height = height + rowHeight + 5
            end
        end
        opening[key] = nil
        return block, place(block, y, height)
    end

    local function answer(turn, key, y, width)
        local first, last = nil, y
        local sources, questID = {}, nil
        local pieces = Format.Blocks(turn.text, Kit.Hex("strong"), sources)
        local tracker = T.settings.appearance == "tracker" and #pieces > 1
        for index, piece in ipairs(pieces) do
            if tracker and not open[key] and index > 1 then break end
            local kind = piece.kind == "item" and "item" or "paragraph"
            if index == 1 and #pieces > 1 and kind == "paragraph" and #piece.text < 160 then kind = "heading" end
            local block = take(kind)
            block.sources, block.showSource = sources, showSource
            T.SetContent(block.text, piece.text)
            local indent = kind == "item" and Kit.space["space-4"] or 0
            last = place(block, last + (index > 1 and GAP or 0), measure(block.text, width - indent))
            first = first or block
            questID = questID or piece.text:match("|Hquest:(%d+):")
        end
        if tracker then
            local block = take("expand")
            block.link:SetLabel(open[key] and "Свернуть ответ" or "Полный ответ")
            block.link.glyph:SetGlyph(open[key] and "up" or "down")
            block.toggle = function()
                open[key] = not open[key]
                scroll:ScrollTo(scroll:GetVerticalScroll(), true)
                scroll.atEnd = false
                handlers.refresh()
            end
            last = place(block, last + 5, 23)
        elseif questID and T.settings.appearance == "journal" then
            local id = tonumber(questID)
            local info = T.Context.Quest(id)
            if info then
                local block = take("quest")
                block.id = id
                T.SetContent(block.name, Format.Escape(info.title))
                local w = math.max(80, width - 42 - block.cta:GetUnboundedStringWidth())
                local h = measure(block.name, w)
                block.meta:ClearAllPoints()
                Kit.Place(block.meta, block, 31, h + 5)
                local status = C_QuestLog.IsComplete and C_QuestLog.IsComplete(id)
                local label = info.level and info.level > 0 and T.L("Уровень {1}", info.level) .. " · " or ""
                block.meta:SetText(label .. T.L(status and "Готово к сдаче" or "В журнале"))
                last = place(block, last + GAP, h + measure(block.meta, w) + 12)
            end
        end
        if turn.source and turn.source ~= "" then
            local block = take("source")
            T.SetContent(block.text, Format.Escape(Format.ToolTrace(turn.source)))
            last = place(block, last + GAP, measure(block.text, width))
            first = first or block
        end
        return first, last
    end

    local function card(thread, y, width)
        local block = take("card")
        local approval = thread.approval
        Kit.Paint(block.stripe, approval and "agent-needs" or "agent-error")
        local agent = Format.Agent(thread)
        block.title:SetText(T.L(approval and (approval.kind == "file" and "{1} запрашивает изменение файлов" or "{1} запрашивает запуск команды") or "{1} остановился", agent))
        local inner = width - PAD - Kit.space["space-3"]
        local height = Kit.space["space-3"] + Kit.TextHeight(block.title)
        block.command:SetShown(approval ~= nil)
        if approval then
            block.commandText:SetText(Format.Escape(approval.text))
            block.commandText:ClearAllPoints()
            Kit.Place(block.commandText, block.command, GAP + 1, Kit.space["space-1"] + 1)
            local command = measure(block.commandText, inner - 2 * GAP - 2) + 2 * Kit.space["space-1"] + 2
            block.command:ClearAllPoints()
            block.command:SetPoint("TOPLEFT", PAD, -(height + GAP))
            block.command:SetPoint("TOPRIGHT", -Kit.space["space-3"], -(height + GAP))
            block.command:SetHeight(command)
            height = height + GAP + command
        end
        local reason = approval and approval.reason or thread.error
        if not reason or reason == "" then
            reason = not approval and T.L("Не удалось получить ответ. Вопрос сохранён.") or nil
        end
        block.reason:SetShown(reason ~= nil)
        if reason then
            T.SetContent(block.reason, Format.Escape(reason))
            block.reason:ClearAllPoints()
            Kit.Place(block.reason, block, PAD, height + GAP)
            height = height + GAP + measure(block.reason, inner)
        end
        local answered = approval and Store.Answered(thread)
        block.allow:SetShown(approval ~= nil)
        block.deny:SetShown(approval ~= nil)
        block.allow:SetEnabled(not answered)
        block.deny:SetEnabled(not answered)
        block.onAnswer = function(allow) handlers.answer(allow) end
        local lastUser = not approval and Store.LastQuestion(thread)
        block.retry:SetShown(lastUser ~= nil)
        block.retry:SetEnabled(not Store.Delivering(thread))
        block.onRetry = function() handlers.retry(thread, lastUser) end
        if approval or lastUser then
            height = height + Kit.space["space-3"] + Kit.size.control
        end
        return block, place(block, y, height + Kit.space["space-3"])
    end

    local function working(thread, y, now, delivery)
        local block = take("working")
        local active = thread.state == "active"
        local activity = Format.Activity(thread)
        local label = thread.compacting and activity
            or delivery == "sending" and T.L("Передаю вопрос в {1}", Format.Agent(thread))
            or delivery == "sent" and T.L("Жду подтверждения от помощника")
            or active and activity or T.L("Помощник получил вопрос")
        block.text:SetText(label)
        block.clock:SetShown(active)
        block.stop:SetShown(active)
        block.clock:SetText(Format.Clock(now - (thread.since or thread.updated)))
        block.onStop = function() handlers.stop() end
        clock = active and { text = block.clock, since = thread.since or thread.updated } or nil
        return block, place(block, y, 24)
    end

    function scroll:Render(thread, now)
        if copyBox and shown ~= thread.id then copyBox:Hide() end
        for _, pool in pairs(pools) do
            for index = 1, pool.used do
                pool[index]:Hide()
            end
            pool.used = 0
        end
        clock = nil
        local width = child:GetWidth() - 2 * PAD
        local turns = Store.Turns(thread)
        local fresh
        if shown == thread.id and anchor then
            local kept = 0
            for index, turn in ipairs(turns) do
                if stamp(turn) == anchor then
                    kept = index
                end
            end
            if kept > 0 and #turns > kept + tail then
                fresh = kept + tail + 1
            end
        end
        local atEnd = shown ~= thread.id or self.atEnd
        local y, previous, delivery, exchange = PAD, nil, nil, 0
        wipe(questionOffsets)
        for index, turn in ipairs(turns) do
            local gap = previous and ((previous == "activity" and turn.role == "assistant") and Kit.space["space-3"] or TURN) or 0
            local block
            if turn.role == "user" then
                questionOffsets[index] = y + gap - PAD
                exchange = turn.at or index
                if turn.journal then
                    block, y = journal(turn, thread.id .. "@journal:" .. exchange .. ":" .. index, y + gap, width)
                else
                    block, y = user(turn, y + gap, width)
                end
                if turn.seq then delivery = Store.Delivery(turn.seq) end
            elseif turn.role == "compaction" then
                block = take("compaction")
                y = place(block, y + gap, math.max(23, Kit.TextHeight(block.text) + GAP))
            elseif turn.role == "activity" then
                block, y = steps(turn, thread.id .. "@" .. exchange .. ":" .. index, y + gap, width)
            elseif turn.role == "assistant" then
                block, y = answer(turn, thread.id .. "@reply:" .. (turn.at or index), y + gap, width)
            else
                block = take("note")
                T.SetContent(block.text, Format.Escape(turn.text))
                y = place(block, y + gap, measure(block.text, width))
            end
            if fresh and index >= fresh and block then
                Kit.Rise(block)
            end
            previous = turn.role
        end
        if thread.approval or thread.state == "error" then
            local _, bottom = card(thread, y + TURN, width)
            y = bottom
        elseif Store.Rank(thread) == 1 or Store.Pending(thread) then
            local _, bottom = working(thread, y + TURN, now, delivery)
            y = bottom
        end
        child:SetHeight(y + PAD)
        shown, anchor, tail = thread.id, nil, 0
        for index, turn in ipairs(turns) do
            if stamp(turn) then
                anchor, tail = stamp(turn), #turns - index
            end
        end
        if atEnd then
            self:ScrollToEnd()
        end
    end

    function scroll:JumpTo(index)
        if questionOffsets[index] then self:ScrollTo(questionOffsets[index], true) end
    end

    function scroll:Tick(now)
        if clock then
            clock.text:SetText(Format.Clock(now - clock.since))
        end
    end

    return scroll
end
