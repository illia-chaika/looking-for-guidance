-- A completed operation may be the only update WoW sees between two polls.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local initial = wow.Samples(here .. "/../samples/samples.json").payloads.ready
wow.payload = CopyTable(initial)
local T = wow.Load(here .. "/../looking-for-guidance")
T.Window.Show()
local id = "019a7c2e-0002"
T.Window.Select(id)
local function visibleStrings(label)
    local count = 0
    for _, labelFrame in ipairs(wow.texts) do
        if labelFrame:IsVisible() and labelFrame:GetText() == label then count = count + 1 end
    end
    return count
end
local snapshot = CopyTable(initial)
local thread
for _, entry in ipairs(snapshot.threads) do
    if entry.id == id then thread = entry break end
end
assert(thread)
thread.state = "active"
thread.compacting = true
thread.since = wow.now
local function deliver()
    snapshot.rev = snapshot.rev + 1
    wow.payload = snapshot
    T.Sync.Now()
end
deliver()
assert(visibleStrings("Сжимаю контекст…") == 1 and visibleStrings("Контекст сжат") == 0,
    "active compaction must be visible without a premature marker")
thread.state, thread.compacting = "idle", nil
table.insert(thread.turns, {role="compaction", text="Контекст сжат"})
table.insert(thread.turns, {role="assistant", text="После сжатия разговор продолжается."})
deliver()
T.Window.Refresh()
assert(visibleStrings("Контекст сжат") == 1 and visibleStrings("Сжимаю контекст…") == 0, "one completed marker")
assert(visibleStrings("После сжатия разговор продолжается.") == 1, "answer after compaction is readable")
T.Window.Select("019a7c2e-0003")
assert(visibleStrings("Контекст сжат") == 0, "marker leaked into a different chat")
T.Window.Select(id)
assert(visibleStrings("Контекст сжат") == 1, "completed marker survives reselecting its chat")
thread.compacting, thread.state = true, "active"
deliver()
thread.compacting, thread.state = nil, "idle"
deliver()
assert(visibleStrings("Контекст сжат") == 1, "interrupted operation created a second success marker")
print("Compaction UI: start, completion, interrupted operation and restored chat OK")
