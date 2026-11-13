local _, T = ...

local Sync = { at = 0 }
T.Sync = Sync

local CADENCE = { 2, 3, 5, 8, 13 }
-- A journal insight alone is slower than a chat turn and has no detail requests to answer.
local QUIET = { 2, 8, 13, 21 }
local STALE, RESTING = 30, 300
local LOW = 16

local timer, due, step
-- Loads spent only because the helper has not connected to Codex yet.
local idle = 0

local function schedule(delay)
    if timer then
        timer:Cancel()
    end
    timer = delay and C_Timer.NewTimer(delay, Sync.Now) or nil
    due = delay and GetTime() + delay or nil
end

function Sync.Now()
    schedule(nil)
    local loaded, reason = T.Transport.Load()
    if not loaded then
        T.Fire("Problem", reason)
        return
    end
    Sync.at = time()
    local left = T.Transport.SlotsLeft()
    if left <= LOW then
        T.Fire("Problem", left == 0 and "exhausted" or "low")
    end
    if left <= 0 then
        step = nil
    elseif T.Store.Working() then
        idle = 0
        step = math.min((step or 0) + 1, #CADENCE)
        schedule(CADENCE[step])
    elseif left > LOW and T.Insights and T.Insights.Watching() then
        idle = 0
        step = math.min((step or 0) + 1, #QUIET)
        schedule(QUIET[step])
    elseif T.Store.Awaiting() and idle < #CADENCE then
        -- A helper that is still starting gets one pass. Opening the window or
        -- "Проверить связь" asks again; an endless wait would spend the session's loads.
        idle = idle + 1
        step = nil
        schedule(CADENCE[idle])
    else
        step = nil
        if not T.Store.Awaiting() then
            idle = 0
        end
    end
end

-- The player asked for a check, so a stalled start may be polled again.
function Sync.Check()
    idle = 0
    Sync.Now()
end

function Sync.Fresh()
    local settled = T.Store.received and T.Store.codex == "ready" and not (T.Insights and T.Insights.Pending())
    if time() - Sync.at >= (settled and RESTING or STALE) then
        Sync.Check()
    end
end

-- One later load for work nobody is watching. An earlier scheduled load serves it too.
function Sync.In(delay)
    if T.Transport.SlotsLeft() <= LOW then
        return
    end
    if not due or due > GetTime() + delay then
        schedule(delay)
    end
end

function Sync.Start()
    local character, login = T.CharacterIdentity()
    T.Transport.Send({ type = "hello", addon = T.version, character = character, login = login })
    Sync.Now()
end

T.On("PacketSent", function(seq)
    if T.Store.Tracked(seq) and T.Store.Awaiting() then
        idle = 0
        step = 1
        schedule(CADENCE[1])
    end
end)
