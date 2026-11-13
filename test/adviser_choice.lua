-- One adviser answers new chats; a chat keeps the adviser it began with, and its own models.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")
local payload = CopyTable(samples.payloads.ready)
-- The sample lists both advisers' models, as the original companion does; here the helper has made Claude the default.
for index = #payload.models, 1, -1 do
    if payload.models[index].id:find("^claude:") then table.remove(payload.models, index) else payload.models[index].default = false end
end
local efforts = { "low", "medium", "high", "xhigh", "max" }
table.insert(payload.models, 1, { id = "claude:sonnet", label = "Claude Sonnet", default = true, effort = "medium", efforts = efforts })
table.insert(payload.models, 2, { id = "claude:opus", label = "Claude Opus", effort = "medium", efforts = efforts })
payload.agent = "claude"
for _, thread in ipairs(payload.threads) do thread.state, thread.approval = "idle", nil end
local codexChat = payload.threads[1]
local claudeChat = CopyTable(codexChat)
claudeChat.id, claudeChat.agent, claudeChat.model, claudeChat.title = "5f0c6a8e-0001", "claude", "claude:sonnet", "Which talents next?"
table.insert(payload.threads, claudeChat)
wow.payload = payload
local T = wow.Load(here .. "/../looking-for-guidance")
T.Insights.Request = function() return false end
local Actions, Store = T.Actions, T.Store

local thread = Store.Find
local function ids(models)
    local out = {}
    for index, model in ipairs(models) do out[index] = model.id end
    return table.concat(out, ",")
end

assert(Store.agent == "claude", "the add-on knows the chosen adviser")
assert(ids(Actions.Models(nil)) == "claude:sonnet,claude:opus", "a new chat is offered the chosen adviser's models: " .. ids(Actions.Models(nil)))
assert(Actions.Choice(nil).model.id == "claude:sonnet" and Actions.Choice(nil).effort == "medium", "and starts with its default")

local old = thread(codexChat.id)
assert(old and old.agent == "codex")
for _, model in ipairs(Actions.Models(old)) do
    assert(Actions.Agent(model) == "codex", "an older Codex chat keeps Codex models")
end
assert(Actions.Choice(old).model.id == codexChat.model, "and shows its own model, not the new default")

-- A Claude chat can change its model for the next message, as a Codex chat can.
local mine = thread(claudeChat.id)
assert(ids(Actions.Models(mine)) == "claude:sonnet,claude:opus")
Actions.Choose("model", "claude:opus", mine)
assert(Actions.Choice(mine).model.id == "claude:opus" and Actions.Choice(mine).overridden, "the choice is kept for this chat")
local before = #wow.sent
Actions.Reply(mine, "And after that?")
local sent = wow.sent[#wow.sent]
assert(#wow.sent == before + 1 and sent.type == "reply" and sent.thread == claudeChat.id and sent.model == "claude:opus",
    "the next message carries the chosen model")

-- Without the field, as with the original companion, every model is offered.
local plain = CopyTable(payload)
plain.agent, plain.rev = nil, payload.rev + 1
wow.payload = plain
T.Sync.Check()
assert(Store.agent == nil and #Actions.Models(nil) == #plain.models, "no chosen adviser means no filter")
print("ok adviser_choice")
