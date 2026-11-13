-- How the add-on spends the one-shot payload loads of a session.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")
wow.slots = 512
local broken = CopyTable(samples.payloads.ready)
broken.codex, broken.note, broken.threads = "error", "Подключаюсь к Codex…", {}
wow.payload = broken
local T = wow.Load(here .. "/../looking-for-guidance")
-- Journal research has its own test (test/insights.lua); here it would add packets and loads.
T.Insights.Request = function() return false end

assert(T.Transport.Slots() == 512, "the add-on counts the installed slot add-ons")
assert(#wow.loaded == 1 and T.Transport.SlotsLeft() == 511, "login spends one load")
assert(T.Store.Awaiting() and not T.Store.Working(), "a helper without Codex is waited on, but it is not work in progress")

wow.Run(600)
assert(#wow.loaded == 6, "a helper that stays broken gets one pass of the cadence, not an endless poll: " .. #wow.loaded)

T.Window.Show()
assert(#wow.loaded == 7, "opening the window asks again")
wow.Run(600)
assert(#wow.loaded == 12, "and gives the helper one more pass")

local ready = CopyTable(samples.payloads.ready)
ready.rev = broken.rev + 1
for _, thread in ipairs(ready.threads) do thread.state, thread.approval = "idle", nil end
wow.payload = ready
T.Sync.Check()
local loads = #wow.loaded
assert(T.Store.codex == "ready" and not T.Store.Awaiting())
wow.Run(600)
assert(#wow.loaded == loads, "a ready, idle helper is not polled")

T.Window.Toggle()
T.Window.Show()
loads = loads + 1
assert(#wow.loaded == loads, "a window opened after a long idle time refreshes once")
T.Window.Toggle()
wow.Run(120)
T.Window.Show()
assert(#wow.loaded == loads, "reopening the window within five minutes of a settled state spends nothing")
T.Window.Toggle()
wow.Run(400)
T.Window.Show()
assert(#wow.loaded == loads + 1, "after five minutes the window refreshes once")

-- A later background load does not replace an earlier scheduled one, and never runs on the reserve.
loads = #wow.loaded
T.Sync.In(3)
T.Sync.In(40)
wow.Run(10)
assert(#wow.loaded == loads + 1, "one deferred load serves both requests")

wow.slots = 0
local bare = dofile(here .. "/wow.lua")
bare.slots = 0
bare.payload = samples.payloads.offline
local reasons = {}
local T2 = bare.Load(here .. "/../looking-for-guidance")
T2.On("Problem", function(reason) reasons[#reasons + 1] = reason end)
T2.Sync.Check()
assert(reasons[1] == "uninstalled" and #bare.loaded == 0, "without slot add-ons the add-on asks for a game restart")
print("Slot budget: probed count, bounded start-up polling, settled refresh and deferred loads OK")
