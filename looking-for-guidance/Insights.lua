local _, T = ...
-- Researched advice for journal cards. The helper prepares it on the Mac and returns it in the payload.
-- A card asks only when the player opens it, presses a button, or once after login.
local Insights = {}
T.Insights = Insights

-- About twelve seconds of sending at most: a bag list with tooltip texts is the largest request.
local BUDGET = 4500
local wanted, deferred = {}, {}

local function id(kind, key) return kind .. "\n" .. key end

local function entry(kind, key)
    for _, item in ipairs(T.Store.insights or {}) do
        if item.kind == kind and item.key == key then return item end
    end
end

-- Shorten the longest lists first; the helper works with what arrives.
local function compact(packet, pass)
    local keep = ({ 12, 6, 0 })[pass]
    for name, value in pairs(packet.facts or {}) do
        if type(value) == "table" and #value > keep then
            if keep == 0 then
                packet.facts[name] = nil
            else
                for index = #value, keep + 1, -1 do value[index] = nil end
            end
        end
    end
end

-- ready, pending or failed with the helper's entry; nil when nothing was asked.
function Insights.State(kind, key)
    local found = entry(kind, key)
    local want = wanted[id(kind, key)]
    if found and not (want and want.force and found.state ~= "pending" and (found.at or 0) <= want.forcedAfter) then
        return found.state, found
    end
    if want then
        if want.seq and T.Store.Delivery(want.seq) == "failed" then
            return "failed", { note = T.L("Запрос не дошёл до помощника.") }
        end
        return "pending", { since = want.at }
    end
end

function Insights.Get(kind, key)
    local state, found = Insights.State(kind, key)
    return state == "ready" and found.data or nil, found
end

local function send(kind, key, facts, options)
    local packet = { type = "insight", kind = kind, key = key, facts = facts, force = options.force or nil,
        locale = T.GetUILocale(), gameLocale = GetLocale() }
    packet.character, packet.login = T.CharacterIdentity()
    packet.runId = T.Store.runId
    local seq, reason = T.Transport.Send(packet, BUDGET, compact)
    if not seq then
        if not options.background then T.Fire("Problem", reason) end
        return
    end
    local previous = entry(kind, key)
    wanted[id(kind, key)] = { at = time(), seq = seq, background = options.background,
        force = options.force, forcedAfter = previous and previous.at or 0, applied = T.Store.applied, tries = 0 }
    -- Every request is confirmed like a chat message: the check two seconds later also brings a cached answer,
    -- and a request lost on the way goes out once more. A background one then gets two late looks at most.
    T.Store.Track(seq)
    T.Fire("StoreChanged", {})
    return true
end

-- options.force asks again past the cache; options.background is for requests nobody is watching.
function Insights.Request(kind, key, facts, options)
    options = options or {}
    if not T.Store.received or T.Store.codex == "offline" or T.Store.codex == "restart" then return false end
    local state = Insights.State(kind, key)
    if not options.force and (state == "ready" or state == "pending") then return true end
    if type(InCombatLockdown) == "function" and InCombatLockdown() then
        deferred[id(kind, key)] = { kind, key, facts, options }
        return true
    end
    return send(kind, key, facts, options) or false
end

-- The player is looking at a card that waits for its answer.
function Insights.Watching()
    if not T.Window or not T.Window.IsShown() then return false end
    for _, want in pairs(wanted) do
        if not want.background then return true end
    end
    return false
end

function Insights.Pending()
    return next(wanted) ~= nil
end

T.On("StoreChanged", function()
    for key, want in pairs(wanted) do
        local kind, name = key:match("^(.-)\n(.*)$")
        local found = entry(kind, name)
        local answered = found and found.state ~= "pending" and not (want.force and (found.at or 0) <= want.forcedAfter)
        if answered or time() - want.at > 420 or (want.seq and T.Store.Delivery(want.seq) == "failed" and time() - want.at > 30) then
            wanted[key] = nil
        elseif want.background and want.applied ~= T.Store.applied and want.tries < 2 then
            -- A payload arrived and the answer is still being prepared.
            want.applied, want.tries = T.Store.applied, want.tries + 1
            T.Sync.In(want.tries == 1 and 40 or 100)
        end
    end
end)

local frame = CreateFrame("Frame")
frame:RegisterEvent("PLAYER_REGEN_ENABLED")
frame:SetScript("OnEvent", function()
    for key, request in pairs(deferred) do
        deferred[key] = nil
        Insights.Request(request[1], request[2], request[3], request[4])
    end
end)
