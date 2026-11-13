local _, T = ...
local Memory = {}
T.HubMemory = Memory

local scopes = {character = "Для этого персонажа", session = "До выхода из игры", day = "До конца дня"}
local order = {"character", "session", "day"}

local function ownView(guid, login)
    local value = T.Store.memory
    if guid and login and value and value.character == guid and value.login == login then return value end
end

local function noteByID(notes, id)
    for _, note in ipairs(notes or {}) do if note.id == id then return note end end
end

function Memory.Create(parent)
    local frame = T.HubPage.Create(parent)
    local editor = CreateFrame("EditBox", nil, frame.scroll.child, "InputBoxTemplate")
    editor:SetAutoFocus(false)
    editor:SetMaxLetters(400)
    editor:SetHeight(30)
    editor:SetFontObject(T.Kit.Font("t-ui"))
    editor:Hide()

    local identity, requestedFor, pending, draft, shownSource, message
    local function endEdit()
        draft = nil
        editor:ClearFocus()
        editor:Hide()
    end
    local function request(action, note)
        if pending then return end
        local guid, login = T.CharacterIdentity()
        if not guid or not login or not T.Store.runId then
            message = T.L("Соединение с Companion ещё не установлено.")
            frame:Refresh()
            return
        end
        local packet = {type = "memory", character = guid, login = login, runId = T.Store.runId, memoryAction = action}
        if note then
            packet.noteId, packet.revision = note.id, note.revision
            if action == "edit" then
                packet.noteText = strtrim(editor:GetText()):gsub("[%c]", " ")
                packet.scope = draft.scope
                if packet.noteText == "" then return end
            end
        end
        local seq, reason = T.Transport.Send(packet)
        if seq then
            pending = { seq = seq, action = action, id = packet.noteId, revision = packet.revision,
                text = packet.noteText, scope = packet.scope }
            message = nil
            T.Store.Track(seq)
        else
            T.Fire("Problem", reason)
            message = T.L("Не удалось отправить запрос.")
        end
        frame:Refresh()
    end

    local function edit(note)
        draft = { id = note.id, revision = note.revision, scope = note.scope, original = note.text }
        message = nil
        editor:SetText(note.text)
        frame:Refresh()
        frame:Top()
        editor:SetFocus()
    end

    editor:SetScript("OnEscapePressed", function() if not pending then endEdit(); frame:Refresh() end end)
    editor:SetScript("OnEnterPressed", function(self)
        if draft and strtrim(self:GetText()) ~= "" and not draft.confirm then request("edit", draft) end
    end)
    editor:SetScript("OnTextChanged", function(self)
        T.ApplyEditFont(self, T.Tokens.font["t-ui"])
    end)

    local function row(note)
        frame:Text(note.text)
        frame:Action(T.L(shownSource == note.id and "Скрыть источник" or "Источник"), function()
            shownSource = shownSource ~= note.id and note.id or nil
            frame:Refresh()
        end)
        if shownSource == note.id then
            local source = note.source
            if type(source) == "table" then
                frame:Text(T.L(source.manual and "Изменено игроком" or "Реплика игрока"), "t-meta", "detail")
                local quote = source.quote
                if type(quote) == "string" and quote ~= "" then frame:Text("«" .. quote .. "»", "t-ui", "detail") end
                local thread = source.threadId and T.Store.Find(source.threadId)
                if thread and T.Window and T.Window.Select then
                    frame:Action(T.L("Открыть чат"), function() T.Window.OpenPage("chat"); T.Window.Select(thread.id) end)
                end
            elseif type(source) == "string" and source ~= "" then frame:Text(source, "t-ui", "detail")
            else frame:Text(T.L("Источник недоступен"), "t-meta", "detail") end
        end
        frame:Action(T.L("Изменить"), function() edit(note) end, not pending)
    end

    local function editing(view)
        local latest = noteByID(view and view.notes, draft.id)
        frame:Text(T.L("Изменить"), "t-title", "strong")
        local child, width, y = frame:Reserve(38)
        editor:ClearAllPoints()
        editor:SetPoint("TOPLEFT", child, "TOPLEFT", 20, -y)
        editor:SetWidth(math.max(45, width - 10))
        editor:Show()
        if not latest or latest.revision ~= draft.revision then
            frame:Text(T.L("Запись изменилась. Открой её заново перед сохранением."), "t-ui", "error")
            if latest then frame:Text(latest.text, "t-ui", "detail") end
        end
        frame:Action(T.L(scopes[draft.scope]), function()
            draft.scope = draft.scope == "character" and "session" or draft.scope == "session" and "day" or "character"
            frame:Refresh()
        end, not pending)
        local current = latest and latest.revision == draft.revision
        if draft.confirm then
            frame:Text(T.L("Удалить эту запись?"))
            frame:Action(T.L("Удалить"), function() request("forget", draft) end, current and not pending)
        else
            frame:Action(T.L("Сохранить"), function() request("edit", draft) end, current and not pending)
            frame:Action(T.L("Удалить из памяти"), function() draft.confirm = true; frame:Refresh() end, current and not pending)
        end
        frame:Action(T.L("Отмена"), function() message = nil; endEdit(); frame:Refresh() end, not pending)
    end

    function frame:Refresh()
        if not self:IsVisible() then return end
        local guid, login = T.CharacterIdentity()
        local key = guid and login and guid .. ":" .. login .. ":" .. (T.Store.runId or "") or nil
        if identity ~= key then
            identity, requestedFor, pending, shownSource, message = key, nil, nil, nil, nil
            endEdit()
        end
        local view = ownView(guid, login)
        if pending then
            if view and view.requestSeq == pending.seq and view.session == T.Transport.session then
                if view.error then
                    message = T.L("Память не обновлена.") .. " " .. view.error
                else
                    if pending.action ~= "list" then endEdit() end
                    message = nil
                end
                pending = nil
            elseif view and T.Store.Delivery(pending.seq) == "delivered" then
                local changed = noteByID(view.notes, pending.id)
                local applied = pending.action == "list" or (pending.action == "forget" and not changed)
                    or (pending.action == "edit" and changed and changed.revision > pending.revision
                        and changed.text == pending.text and changed.scope == pending.scope)
                if applied then
                    if pending.action ~= "list" then endEdit() end
                    message = nil
                else
                    message = T.L("Companion не подтвердил запрос. Обнови список.")
                end
                pending = nil
            elseif T.Store.Delivery(pending.seq) == "failed" then
                pending = nil
                message = T.L("Companion не подтвердил запрос. Обнови список.")
            end
        end
        self:Begin()
        self:Text(T.L("Память персонажа"), "t-display", "strong")
        self:Text(T.L("Советник берёт отсюда твои планы и предпочтения для новых советов."), "t-ui", "detail")
        if message then self:Text(message, "t-meta", "error") end
        if pending then self:Text(T.L("Ожидание Companion…"), "t-meta", "detail") end
        if draft then
            editing(view)
        elseif view and view.notes then
            local count = 0
            for _, scope in ipairs(order) do
                local first = true
                for _, note in ipairs(view.notes) do
                    if note.scope == scope and type(note.text) == "string" and (not note.expires or note.expires > time()) then
                        if first then self:Text(T.L(scopes[scope]), "t-title", "strong"); first = false end
                        count = count + 1
                        row(note)
                    end
                end
            end
            if count == 0 then self:Text(T.L("Память этого персонажа пока пуста.")) end
            self:Action(T.L("Обновить"), function() request("list") end, not pending)
        else
            self:Text(T.L("Загрузка памяти персонажа…"), "t-ui", "detail")
            self:Action(T.L("Обновить"), function() request("list") end, not pending and guid ~= nil and login ~= nil and T.Store.runId ~= nil)
        end
        self:Finish()
        if not view and not pending and key and T.Store.runId and requestedFor ~= key then
            requestedFor = key
            request("list")
        end
    end
    frame:SetScript("OnShow", function(self) self:Refresh() end)
    frame:SetScript("OnHide", function() editor:ClearFocus() end)
    frame:SetScript("OnSizeChanged", function(self) self:Refresh() end)
    return frame
end
