local _, T = ...

-- An interface reload or a second client can start within the same second.
local Transport = { session = string.format("s%d-%06x", time(), math.random(0, 0xffffff)) }
T.Transport = Transport

local VERSION = 4
local SLOT_LIMIT = 1024
local BITS = 128
local FRAME_DATA = BITS / 8 - 1
local FRAME_SECONDS = 0.04
-- After every BLOCK data frames come CHECKS check frames, and after the last, shorter block too. In a block, the
-- frames whose offsets are equal modulo CHECKS XOR to zero, so the helper rebuilds up to CHECKS missed frames in a
-- row. It reads the file from outside and has been away from it for a fifth of a second beside a busy game.
local BLOCK, CHECKS = 48, 6

local bits = {}
for index = 1, BITS do
    bits[index] = string.format("looking-for-guidance-bit-%03d", index)
end

local slot, seq, counter = 1, 0, time() % 64
local installed

local function slotName(index)
    return string.format("looking-for-guidance-slot-%03d", index)
end

-- The helper decides how many one-shot payload add-ons exist. Count them once:
-- the game lists add-on folders only when the client starts.
local function slots()
    if not installed then
        installed = 0
        while installed < SLOT_LIMIT and C_AddOns.DoesAddOnExist(slotName(installed + 1)) do
            installed = installed + 1
        end
    end
    return installed
end
local queue, shown, sent = {}, {}, {}
local stepping, shownAt = false, 0
-- Real time where the client has it: the frame's own time stands still while the game is busy.
local clock = GetTimePreciseSec or GetTime

local function show(frame)
    for byte = 1, #frame do
        local value = frame:byte(byte)
        for bit = 0, 7 do
            local index = (byte - 1) * 8 + bit + 1
            local on = math.floor(value / 2 ^ (7 - bit)) % 2 == 1
            if shown[index] ~= on then
                if on then
                    C_AddOns.EnableAddOn(bits[index])
                else
                    C_AddOns.DisableAddOn(bits[index])
                end
                shown[index] = on
            end
        end
    end
    C_AddOns.SaveAddOns()
end

local function cancel(index)
    local packet = table.remove(queue, index)
    -- Drop the bytes but retain the guard for callers replaying their own copy of the envelope.
    if sent[packet.seq] then sent[packet.seq].body = nil end
    T.Fire("PacketCancelled", packet.seq)
end

function Transport.CancelDisallowed()
    for i = #queue, 1, -1 do
        local packet = queue[i]
        if packet.canSend and not packet.canSend() then cancel(i) end
    end
end

-- Each frame waits a full interval after the previous one. A repeating ticker fires early after a late tick to keep
-- its schedule, and two frames 10 ms apart are overwritten on disk before the helper has read the first:
-- one missed frame loses the whole packet.
local function step()
    local packet = queue[1]
    -- The next start frame resets the helper's decoder if permission changed mid-packet.
    while packet and packet.canSend and not packet.canSend() do
        cancel(1)
        packet = queue[1]
    end
    if not packet then
        stepping = false
        return
    end
    local wait = shownAt + FRAME_SECONDS - clock()
    if wait > 0.005 then
        C_Timer.After(wait, step)
        return
    end
    -- The head says where the packet starts and ends; its counter runs on across packets.
    local head = (packet.next == 1 and 128 or 0) + (packet.next == #packet.frames and 64 or 0) + counter
    counter = (counter + 1) % 64
    show(string.char(head) .. packet.frames[packet.next])
    shownAt = clock()
    C_Timer.After(FRAME_SECONDS, step)
    packet.next = packet.next + 1
    if packet.next > #packet.frames then
        table.remove(queue, 1)
        T.Fire("PacketSent", packet.seq)
    end
end

local function frames(body)
    local stream = string.char(math.floor(#body / 256), #body % 256) .. body
    local list, start = {}, 1
    while start <= #stream do
        local sums, count = {}, 0
        while count < BLOCK and start <= #stream do
            local chunk = stream:sub(start, start + FRAME_DATA - 1)
            chunk = chunk .. string.rep("\0", FRAME_DATA - #chunk)
            list[#list + 1] = chunk
            local sum = sums[count % CHECKS] or {}
            sums[count % CHECKS] = sum
            for index = 1, FRAME_DATA do
                sum[index] = bit.bxor(sum[index] or 0, chunk:byte(index))
            end
            start, count = start + FRAME_DATA, count + 1
        end
        for offset = count, count + CHECKS - 1 do
            local sum, check = sums[offset % CHECKS], {}
            for index = 1, FRAME_DATA do
                check[index] = string.char(sum and sum[index] or 0)
            end
            list[#list + 1] = table.concat(check)
        end
    end
    return list
end

local function encode(packet)
    return C_EncodingUtil.CompressString(C_EncodingUtil.SerializeJSON(packet), Enum.CompressionMethod.Zlib)
end

local function enqueue(number, body, canSend)
    table.insert(queue, { seq = number, next = 1, frames = frames(body), canSend = canSend })
    if not stepping then
        stepping = true
        C_Timer.After(FRAME_SECONDS, step)
    end
end

-- The latest packets as sent, so one that the helper never confirmed can go out again unchanged.
local KEPT = 24

function Transport.Send(packet, byteBudget, compact, canSend)
    if not C_AddOns.DoesAddOnExist(bits[BITS]) then
        return nil, "uninstalled"
    end
    seq = seq + 1
    sent[seq - KEPT] = nil
    packet.seq, packet.session, packet.at = seq, Transport.session, time()
    local ok, body = pcall(encode, packet)
    if ok and type(body) == "string" and byteBudget and #body > byteBudget and compact then
        for pass = 1, 3 do
            compact(packet, pass)
            ok, body = pcall(encode, packet)
            if not ok or type(body) ~= "string" or #body <= byteBudget then break end
        end
    end
    if not ok or type(body) ~= "string" or #body == 0 then
        return nil, "unencodable"
    elseif #body > (byteBudget or 65535) then
        return nil, "oversized"
    end
    enqueue(seq, body, canSend)
    sent[seq] = { body = body, canSend = canSend }
    return seq, nil, body
end

-- Frames can be lost on the way: the game rewrites AddOns.txt every 40 ms and the helper may miss one.
-- The helper recognises a repeated session and sequence, so sending the same envelope twice is safe.
function Transport.Retry(number)
    local packet = sent[number]
    if not packet or not packet.body or (packet.canSend and not packet.canSend()) or Transport.Sending(number) or not C_AddOns.DoesAddOnExist(bits[BITS]) then
        return false
    end
    enqueue(number, packet.body, packet.canSend)
    return true
end

-- The helper deduplicates by session and sequence. Replay the exact original
-- envelope (including capture time and state) when the acknowledgement was lost.
function Transport.Resend(number, body, canSend)
    local saved = sent[number]
    canSend = (saved and saved.canSend) or canSend
    if canSend and not canSend() then return nil, "revoked" end
    if not C_AddOns.DoesAddOnExist(bits[BITS]) then
        return nil, "uninstalled"
    end
    if not Transport.Sending(number) then
        enqueue(number, body, canSend)
    end
    return number
end

function Transport.Sending(packetSeq)
    for _, packet in ipairs(queue) do
        if packet.seq == packetSeq then
            return true
        end
    end
    return false
end

function Transport.SlotsLeft()
    return slots() - slot + 1
end

function Transport.Slots()
    return slots()
end

function Transport.Load()
    if slots() == 0 then
        return false, "uninstalled"
    elseif slot > slots() then
        return false, "exhausted"
    end
    local name = slotName(slot)
    C_AddOns.EnableAddOn(name)
    local loaded, reason = C_AddOns.LoadAddOn(name)
    if not loaded then
        return false, reason
    end
    slot = slot + 1
    return true
end

function looking_for_guidance_receive(json)
    local ok, data = pcall(C_EncodingUtil.DeserializeJSON, json)
    if ok and type(data) == "table" and data.v == VERSION then
        T.Store.Apply(data)
        T.GameDetails.Receive(data.detailRequest)
    else
        T.Fire("Problem", "unreadable")
    end
end
