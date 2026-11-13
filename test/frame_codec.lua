-- The frames the add-on writes: data frames in blocks of 48, six check frames after each block, a head byte that
-- marks the packet's first and last frame and counts on across packets. With a file name as the argument the
-- script also writes the frames of one packet there for the helper's decoder test:
--   lua test/frame_codec.lua helper/testdata/frames.txt
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")
wow.payload = samples.payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
T.Insights.Request = function() return false end
wow.Run(30)

-- The game compresses packets with zlib. Stored blocks are valid zlib and need no compressor here.
local function zlib(text)
    local a, b = 1, 0
    for index = 1, #text do
        a = (a + text:byte(index)) % 65521
        b = (b + a) % 65521
    end
    local parts = { "\120\1" }
    for start = 1, math.max(#text, 1), 65535 do
        local block = text:sub(start, start + 65534)
        parts[#parts + 1] = string.pack("<BI2I2", start + 65535 > #text and 1 or 0, #block, 65535 - #block) .. block
    end
    parts[#parts + 1] = string.pack(">I4", b * 65536 + a)
    return table.concat(parts)
end
C_EncodingUtil.CompressString = zlib
local json
local serialize = C_EncodingUtil.SerializeJSON
C_EncodingUtil.SerializeJSON = function(packet)
    json = serialize(packet)
    return json
end

local function send(text)
    local first = #wow.bits
    local seq, _, body = T.Transport.Send({ type = "reply", thread = "t1", text = text })
    assert(seq and body, "the packet was accepted")
    while T.Transport.Sending(seq) do wow.Run(0.04) end
    local frames = {}
    for index = first + 1, #wow.bits do frames[#frames + 1] = wow.bits[index] end
    return frames, body
end

local function inspect(frames, body, name)
    local data = math.ceil((2 + #body) / 15)
    assert(#frames == data + 6 * math.ceil(data / 48), name .. ": " .. data .. " data frames travel as " .. #frames)
    for index, frame in ipairs(frames) do
        local head = frame:byte(1)
        assert((head >= 128) == (index == 1), name .. ": only the first frame is marked as the start")
        assert((head % 128 >= 64) == (index == #frames), name .. ": only the last frame is marked as the end")
        if index > 1 then
            assert(head % 64 == (frames[index - 1]:byte(1) % 64 + 1) % 64, name .. ": the counter goes up by one")
        end
    end
    local stream, position = {}, 1
    while position <= #frames do
        local count = math.min(48, data - #stream)
        for class = 0, 5 do
            local sum = {}
            for offset = class, count + 5, 6 do
                local frame = frames[position + offset]
                for byte = 2, 16 do sum[byte] = (sum[byte] or 0) ~ frame:byte(byte) end
            end
            for byte = 2, 16 do assert((sum[byte] or 0) == 0, name .. ": every group of a block cancels out") end
        end
        for offset = 0, count - 1 do stream[#stream + 1] = frames[position + offset]:sub(2) end
        position = position + count + 6
    end
    stream = table.concat(stream)
    assert(stream:byte(1) * 256 + stream:byte(2) == #body and stream:sub(3, 2 + #body) == body, name .. ": the data frames carry the packet")
end

local sizes = {}
for _, length in ipairs({ 0, 40, 520, 560, 600, 1300, 2600 }) do
    local frames, body = send(string.rep("Where do I go next? ", 200):sub(1, length))
    inspect(frames, body, length .. " characters")
    sizes[#sizes + 1] = #frames
end
assert(sizes[1] < 54 and sizes[#sizes] > 108, "the sizes cover a short block, full blocks and several blocks")

if arg[1] then
    local frames = send(string.rep("how do I get to Booty Bay, and what should I buy on the way there? ", 12))
    local lines = { "# " .. json }
    for _, frame in ipairs(frames) do
        lines[#lines + 1] = frame:gsub(".", function(c) return string.format("%02x", c:byte()) end)
    end
    local file = assert(io.open(arg[1], "w"))
    file:write(table.concat(lines, "\n"), "\n")
    file:close()
    print("wrote " .. #frames .. " frames to " .. arg[1])
end
print("ok frame_codec: " .. table.concat(sizes, ", ") .. " frames")
