local _, T = ...

local Actions = {}
T.Actions = Actions

local function send(packet)
    local seq, reason, body = T.Transport.Send(packet)
    if seq then
        T.Store.Track(seq)
    else
        T.Fire("Problem", reason)
    end
    return seq, body
end

local function find(list, key, value)
    for _, item in ipairs(list) do
        if item[key] == value then
            return item
        end
    end
end

function Actions.Agent(model)
    return model.id:find("^claude:") and "claude" or "codex"
end

-- A chat keeps the adviser it began with. New chats go to the one chosen in the companion; without that choice,
-- as in the original app, every model is offered.
function Actions.Models(thread)
    local agent = thread and thread.agent or T.Store.agent
    if not agent then return T.Store.models end
    local own = {}
    for _, model in ipairs(T.Store.models) do
        if Actions.Agent(model) == agent then own[#own + 1] = model end
    end
    return own
end

function Actions.Choice(thread)
    local override = thread and (T.settings.threadChoices or {})[thread.id]
    local saved, models = T.settings.choice, Actions.Models(thread)
    if thread then
        saved = {model = override and override.model or thread.model,
            effort = override and override.effort or thread.effort, cwd = thread.cwd}
    end
    local model = find(models, "id", saved.model) or find(models, "default", true) or models[1]
    local effort = model and (tContains(model.efforts or {}, saved.effort) and saved.effort or model.effort)
    return {
        overridden = override ~= nil,
        model = model,
        effort = effort,
        project = find(T.Store.projects, "cwd", saved.cwd) or T.Store.projects[1],
    }
end

function Actions.Choose(key, value, thread)
    local pick = Actions.Choice(thread)
    local saved = T.settings.choice
    if thread then
        if thread.id:match("^local:") then return end
        T.settings.threadChoices = T.settings.threadChoices or {}
        saved = T.settings.threadChoices[thread.id] or {model = pick.model and pick.model.id, effort = pick.effort}
        T.settings.threadChoices[thread.id] = saved
    end
    saved[key] = value
    if key == "model" and (not pick.model or pick.model.id ~= value) then
        local model = find(T.Store.models, "id", value)
        saved.effort = model and model.effort
    end
    T.Fire("StoreChanged", {})
end

local function message(packet, refs)
    packet.character, packet.login = T.CharacterIdentity()
    packet.runId = T.Store.runId
    local blocks
    if not packet.retrySnapshot then packet.contextText, blocks = T.Context.ForQuestion(packet.runId, refs) end
    packet.plain = not packet.contextText and not T.settings.extras or nil
    local seq, body = send(packet)
    T.Context.Sent(seq, packet.runId, blocks)
    return seq, body
end

function Actions.Ask(text, refs, journal, retrySnapshot)
    local choice = Actions.Choice()
    if not choice.model or not choice.project then
        T.Fire("Problem", "unready")
        return
    end
    local seq, body = message({ type = "create", text = text, cwd = choice.project.cwd, model = choice.model.id, effort = choice.effort,
        journal = journal, retrySnapshot = retrySnapshot }, refs)
    return seq and T.Store.AddTask({
        text = text, references = refs, journal = journal, cwd = choice.project.cwd, project = choice.project.label,
        model = choice.model.id, effort = choice.effort, agent = Actions.Agent(choice.model),
    }, seq, body)
end

function Actions.Reply(thread, text, refs, journal, retrySnapshot)
    -- A local placeholder has no Codex chat ID until the helper confirms creation.
    if not thread or thread.id:match("^local:") then return end
    local packet = { type = "reply", thread = thread.id, text = text, journal = journal, retrySnapshot = retrySnapshot }
    local pick = Actions.Choice(thread)
    if pick.overridden and pick.model then packet.model, packet.effort = pick.model.id, pick.effort end
    local seq, body = message(packet, refs)
    if seq then
        T.Store.AddReply(thread, text, seq, body, refs, journal)
    end
    return seq
end

function Actions.Resend(thread, seq)
    local outgoing = T.Store.Outgoing(thread, seq)
    if not outgoing or not outgoing.body or T.Store.Delivery(seq) ~= "failed" then
        return
    end
    if outgoing.runId ~= T.Store.runId or time() - outgoing.at >= 3600 then
        T.Fire("Problem", "staleDelivery")
        return
    end
    local again, reason = T.Transport.Resend(seq, outgoing.body)
    if again then
        T.Store.Track(seq)
        T.Fire("StoreChanged", {})
    else
        T.Fire("Problem", reason)
    end
    return again
end

function Actions.Rename(thread, title)
    if send({ type = "rename", thread = thread.id, title = title }) then
        T.Store.Rename(thread, title)
    end
end

function Actions.Answer(thread, allow)
    local approval = thread.approval
    if approval and send({ type = allow and "approve" or "deny", thread = thread.id, approval = approval.id }) then
        T.Store.Answer(thread, approval.id)
    end
end

function Actions.Stop(thread)
    send({ type = "stop", thread = thread.id })
end

function Actions.Read(thread)
    if T.Store.MarkRead(thread) then
        T.Transport.Send({ type = "read", thread = thread.id })
    end
end
