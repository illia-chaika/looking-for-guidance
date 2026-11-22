local addonName, T = ...
local Inventory = {}
T.InstalledAddons = Inventory

local BYTE_BUDGET, MAX_PAGE = 5500, 256
local scans, last = {}

local function plain(value)
    if issecretvalue and issecretvalue(value) then return nil end
    return value
end

local function read(fn, ...)
    if fn then
        local ok, a, b = pcall(fn, ...)
        if ok then return plain(a), plain(b) end
    end
end

local function boolean(value)
    if type(value) == "boolean" then return value end
end

local function text(value)
    if type(value) ~= "string" then return end
    value = value:gsub("|T.-|t", ""):gsub("|A.-|a", ""):gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""):gsub("[%c]+", " ")
    if value ~= "" then return value end
end

local securityNames = { Banned = "BANNED", Secure = "SECURE", Insecure = "INSECURE", SecureProtected = "SECURE_PROTECTED",
    NotAvailable = "NOT_AVAILABLE" }
local function security(value)
    local enum = Enum and Enum.AddOnSecurityStatus
    for key, label in pairs(securityNames) do
        if value == label or value == key or (type(value) == "number" and enum and value == enum[key]) then
            return label
        end
    end
    return "UNKNOWN"
end

local function dependencies(index)
    if not C_AddOns.GetAddOnDependencies then return nil, false end
    local values = { pcall(C_AddOns.GetAddOnDependencies, index) }
    if not values[1] then return nil, false end
    local names = {}
    for i = 2, #values do
        local name = plain(values[i])
        if type(name) ~= "string" or name == "" then return nil, false end
        names[#names + 1] = name
    end
    if #names > 0 then return names, true end
    return nil, true
end

local function describe(index, name, guid)
    local api = C_AddOns
    local state = read(api.GetAddOnEnableState, index, guid)
    local enabled
    local states = Enum and Enum.AddOnEnableState
    if state ~= nil and states then
        if state == states.None then enabled = false
        elseif state == states.Some or state == states.All then enabled = true end
    end
    local _, loaded = read(api.IsAddOnLoaded, index)
    local loadable, reason = read(api.IsAddOnLoadable, index, guid)
    local deps, depsKnown = dependencies(index)
    return { name = name, title = text(read(api.GetAddOnTitle, index)), version = text(read(api.GetAddOnMetadata, index, "Version")),
        enabled = enabled, loaded = boolean(loaded), loadError = boolean(read(api.DoesAddOnHaveLoadError, index)),
        loadable = boolean(loadable), reason = text(reason),
        loadOnDemand = boolean(read(api.IsAddOnLoadOnDemand, index)), security = security(read(api.GetAddOnSecurity, index)),
        dependencies = deps, dependenciesKnown = depsKnown }
end

local function transportModule(name)
    if name == addonName .. "-payload" then return true end
    if name:sub(1, #addonName + 1) ~= addonName .. "-" then return false end
    local channel, number = name:sub(#addonName + 2):match("^(%a+)%-(%d+)$")
    local limit = channel == "bit" and 128 or channel == "slot" and 1024
    local n = tonumber(number)
    return limit and n and n >= 1 and n <= limit and number == string.format("%03d", n)
end

local function scan(guid)
    local api = C_AddOns or {}
    local count = read(api.GetNumAddOns)
    if type(count) ~= "number" or count < 0 or count % 1 ~= 0 then
        return nil, "The game could not enumerate installed add-ons on this build."
    end
    local names, parent = {}
    for index = 1, count do
        local name = read(api.GetAddOnName or api.GetAddOnInfo, index)
        if type(name) ~= "string" or name == "" then
            return nil, "The game could not identify every installed add-on. Ask again for a fresh scan."
        end
        names[index] = name
        if name == addonName then parent = index end
    end
    local rows, parentRow = {}, nil
    local grouped, banned, unknown, unavailable = 0, 0, 0, 0
    local moduleLoadErrors, unknownLoadErrors = 0, 0
    for index, name in ipairs(names) do
        if parent and transportModule(name) then
            grouped = grouped + 1
            local status = security(read(api.GetAddOnSecurity, index))
            if status == "BANNED" then banned = banned + 1
            elseif status == "UNKNOWN" then unknown = unknown + 1
            elseif status == "NOT_AVAILABLE" then unavailable = unavailable + 1 end
            local loadError = boolean(read(api.DoesAddOnHaveLoadError, index))
            if loadError then moduleLoadErrors = moduleLoadErrors + 1
            elseif loadError == nil then unknownLoadErrors = unknownLoadErrors + 1 end
        else
            local row = describe(index, name, guid)
            rows[#rows + 1] = row
            if index == parent then parentRow = row end
        end
    end
    if parentRow and grouped > 0 then
        parentRow.groupedModules = grouped
        if banned > 0 then parentRow.bannedModules = banned end
        if unknown > 0 then parentRow.unknownSecurityModules = unknown end
        if unavailable > 0 then parentRow.unavailableSecurityModules = unavailable end
        if moduleLoadErrors > 0 then parentRow.loadErrorModules = moduleLoadErrors end
        if unknownLoadErrors > 0 then parentRow.unknownLoadErrorModules = unknownLoadErrors end
    end
    return rows
end

-- Each page shares the first page's observations, even when two chats interleave their requests.
-- The helper advances by the actual number of complete rows that fit on the wire.
local function page(request)
    local query = request.addons
    local state = scans[query.id]
    if state and (state.character ~= request.character or state.login ~= request.login or state.run ~= T.Store.runId) then
        state = nil
    end
    if not state then
        if query.offset ~= 0 then return { error = "The add-on scan expired. Ask again for a fresh scan." } end
        local rows, err = scan(request.character)
        if not rows then return { error = err } end
        local id, token = query.id, {}
        state = { rows = rows, character = request.character, login = request.login, run = T.Store.runId, token = token }
        scans[query.id] = state
        C_Timer.After(180, function() if scans[id] and scans[id].token == token then scans[id] = nil end end)
    end
    local total = #state.rows
    if query.offset > total or (query.offset == total and total > 0) then
        return { error = "Invalid add-on scan offset. Ask again for a fresh scan." }
    end
    local result = { offset = query.offset, total = total }
    if total > 0 then
        result.rows = {}
        for i = query.offset + 1, math.min(total, query.offset + MAX_PAGE) do
            result.rows[#result.rows + 1] = state.rows[i]
        end
    end
    return result
end

local function send(request, result, canSend)
    local packet = { type = "details", requestId = request.id, runId = T.Store.runId,
        character = request.character, login = request.login, addons = result }
    while true do
        local seq, err, body = T.Transport.Send(packet, BYTE_BUDGET, nil, not result.error and canSend or nil)
        if seq then return seq, body end
        local rows = result.rows
        if not rows or (err ~= "oversized" and err ~= "unencodable") then return end
        if #rows == 1 or err == "unencodable" then
            result.rows, result.offset, result.total = nil, nil, nil
            result.error = "The game could not send the add-on inventory in full. An entry could not be encoded within the transport limit."
        else
            for i = #rows, math.ceil(#rows / 2) + 1, -1 do rows[i] = nil end
        end
    end
end

-- Re-enabling sharing before the next frame still invalidates older requests and frozen scans.
local sharingRevision = 0
T.On("SettingsSaved", function()
    if not T.settings.extras or not T.settings.context.addons then
        sharingRevision = sharingRevision + 1
        scans = {}
        T.Transport.CancelDisallowed()
    end
end)

local function access(request)
    if not T.settings.extras or T.settings.context.addons ~= true then
        return "Add-on access is off. Enable Add-ons in Settings > Character data and send a new question."
    end
    local guid, login = T.CharacterIdentity()
    if guid ~= request.character or login ~= request.login then
        return "The active character or login changed. Send a new question from the current character."
    end
end

function Inventory.Receive(request)
    if not request then last = nil; return end
    local query = request.addons
    if request.session ~= T.Transport.session or type(request.id) ~= "string" or #request.id > 80
        or type(request.character) ~= "string" or type(request.login) ~= "string" or type(query) ~= "table"
        or type(query.id) ~= "string" or #query.id ~= 32 or not query.id:match("^%x+$")
        or type(query.offset) ~= "number" or query.offset < 0 or query.offset % 1 ~= 0 then return end

    local err = access(request)
    if err then scans = {} end
    local run = T.Store.runId
    if last and last.id == request.id and last.run == run then
        if not last.canSend or last.canSend() then
            if T.Store.Delivery(last.seq) == "failed" then
                T.Transport.Resend(last.seq, last.body, last.canSend)
                T.Store.Track(last.seq, last.canSend)
            end
            return
        end
        err = err or "Add-on access changed. Send a new question."
    end
    local result = err and { error = err } or page(request)
    local revision, token = sharingRevision, scans[query.id] and scans[query.id].token
    local function canSend()
        if revision ~= sharingRevision or run ~= T.Store.runId or access(request) then
            revision = nil
            if scans[query.id] and scans[query.id].token == token then scans[query.id] = nil end
            return false
        end
        return true
    end
    local seq, body = send(request, result, canSend)
    if result.error or (seq and result.offset + #(result.rows or {}) == result.total) then scans[query.id] = nil end
    last = nil
    if seq then
        last = { id = request.id, run = run, seq = seq, body = body, canSend = not result.error and canSend or nil }
        T.Store.Track(seq, last.canSend)
    end
end
