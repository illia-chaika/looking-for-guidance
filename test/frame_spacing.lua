-- The game runs an overdue timer as soon as it can, and a repeating ticker then fires early to keep its schedule.
-- Frames written a few milliseconds apart are overwritten on disk before the helper has read the first one,
-- and one missed frame loses the whole packet. Measured in the game on 12.11: 6 of 382 frames came 9-15 ms apart.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")
wow.payload = samples.payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
T.Insights.Request = function() return false end
wow.Run(30)

local saves = {}
local save = C_AddOns.SaveAddOns
C_AddOns.SaveAddOns = function()
    table.insert(saves, wow.now)
    save()
end

local first = #wow.bits
local seq = assert(T.Transport.Send({ type = "read", text = string.rep("Where do I go next? ", 40) }))
local stalls = 0
while T.Transport.Sending(seq) do
    wow.Run(0.04)
    -- Every tenth frame the game freezes for 130 ms: three frames fall due while nothing runs.
    if #saves % 10 == 5 and stalls < #saves then
        stalls = #saves
        wow.now = wow.now + 0.13
    end
end
wow.Run(1)

assert(#saves > 20 and stalls > 0, "the packet took several frames and the game froze on the way: " .. #saves)
local closest = math.huge
for index = 2, #saves do
    closest = math.min(closest, saves[index] - saves[index - 1])
end
assert(closest >= 0.04 - 1e-6, string.format("two frames were written %.0f ms apart", closest * 1000))
for index = first + 2, #wow.bits do
    local was, now = wow.bits[index - 1]:byte(1) % 128, wow.bits[index]:byte(1) % 128
    assert(now == (was + 1) % 128, "the frames keep their order")
end
assert(#wow.bits - first == #saves, "every frame was written once")

-- A second packet queued while the first is still going out starts after it and keeps the same spacing.
local before = #saves
local one = assert(T.Transport.Send({ type = "read", text = "first" }))
local two = assert(T.Transport.Send({ type = "read", text = "second" }))
while T.Transport.Sending(one) or T.Transport.Sending(two) do wow.Run(0.01) end
wow.Run(1)
assert(#saves > before + 2)
for index = before + 1, #saves do
    assert(saves[index] - saves[index - 1] >= 0.04 - 1e-6, "queued packets keep the spacing")
end
local sent = #saves
wow.Run(5)
assert(#saves == sent, "an empty queue writes nothing")
print("ok frame_spacing: " .. #saves .. " frames, closest " .. string.format("%.0f", closest * 1000) .. " ms")
