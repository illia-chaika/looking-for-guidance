local _, T = ...

--[[
Unicode character names and property data

UNICODE LICENSE V3

COPYRIGHT AND PERMISSION NOTICE

Copyright © 1991-2026 Unicode, Inc.

NOTICE TO USER: Carefully read the following legal agreement. BY
DOWNLOADING, INSTALLING, COPYING OR OTHERWISE USING DATA FILES, AND/OR
SOFTWARE, YOU UNEQUIVOCALLY ACCEPT, AND AGREE TO BE BOUND BY, ALL OF THE
TERMS AND CONDITIONS OF THIS AGREEMENT. IF YOU DO NOT AGREE, DO NOT
DOWNLOAD, INSTALL, COPY, DISTRIBUTE OR USE THE DATA FILES OR SOFTWARE.

Permission is hereby granted, free of charge, to any person obtaining a
copy of data files and any associated documentation (the "Data Files") or
software and any associated documentation (the "Software") to deal in the
Data Files or Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, and/or sell
copies of the Data Files or Software, and to permit persons to whom the
Data Files or Software are furnished to do so, provided that either (a)
this copyright and permission notice appear with all copies of the Data
Files or Software, or (b) this copyright and permission notice appear in
associated Documentation.

THE DATA FILES AND SOFTWARE ARE PROVIDED "AS IS", WITHOUT WARRANTY OF ANY
KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT OF
THIRD PARTY RIGHTS.

IN NO EVENT SHALL THE COPYRIGHT HOLDER OR HOLDERS INCLUDED IN THIS NOTICE
BE LIABLE FOR ANY CLAIM, OR ANY SPECIAL INDIRECT OR CONSEQUENTIAL DAMAGES,
OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS,
WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION,
ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THE DATA
FILES OR SOFTWARE.

Except as contained in this notice, the name of a copyright holder shall
not be used in advertising or otherwise to promote the sale, use or other
dealings in these Data Files or Software without prior written
authorization of the copyright holder.

SPDX-License-Identifier: Unicode-3.0
]]

-- Unicode 16 Emoji and reserved Extended_Pictographic code points. This also
-- recognizes unsupported clusters so they can be removed as one unit.
local ranges = {
    0xa9, 0xa9,  0xae, 0xae,  0x203c, 0x203c,  0x2049, 0x2049,  0x2122, 0x2122,
    0x2139, 0x2139,  0x2194, 0x2199,  0x21a9, 0x21aa,  0x231a, 0x231b,  0x2328, 0x2328,
    0x23cf, 0x23cf,  0x23e9, 0x23f3,  0x23f8, 0x23fa,  0x24c2, 0x24c2,  0x25aa, 0x25ab,
    0x25b6, 0x25b6,  0x25c0, 0x25c0,  0x25fb, 0x25fe,  0x2600, 0x2604,  0x260e, 0x260e,
    0x2611, 0x2611,  0x2614, 0x2615,  0x2618, 0x2618,  0x261d, 0x261d,  0x2620, 0x2620,
    0x2622, 0x2623,  0x2626, 0x2626,  0x262a, 0x262a,  0x262e, 0x262f,  0x2638, 0x263a,
    0x2640, 0x2640,  0x2642, 0x2642,  0x2648, 0x2653,  0x265f, 0x2660,  0x2663, 0x2663,
    0x2665, 0x2666,  0x2668, 0x2668,  0x267b, 0x267b,  0x267e, 0x267f,  0x2692, 0x2697,
    0x2699, 0x2699,  0x269b, 0x269c,  0x26a0, 0x26a1,  0x26a7, 0x26a7,  0x26aa, 0x26ab,
    0x26b0, 0x26b1,  0x26bd, 0x26be,  0x26c4, 0x26c5,  0x26c8, 0x26c8,  0x26ce, 0x26cf,
    0x26d1, 0x26d1,  0x26d3, 0x26d4,  0x26e9, 0x26ea,  0x26f0, 0x26f5,  0x26f7, 0x26fa,
    0x26fd, 0x26fd,  0x2702, 0x2702,  0x2705, 0x2705,  0x2708, 0x270d,  0x270f, 0x270f,
    0x2712, 0x2712,  0x2714, 0x2714,  0x2716, 0x2716,  0x271d, 0x271d,  0x2721, 0x2721,
    0x2728, 0x2728,  0x2733, 0x2734,  0x2744, 0x2744,  0x2747, 0x2747,  0x274c, 0x274c,
    0x274e, 0x274e,  0x2753, 0x2755,  0x2757, 0x2757,  0x2763, 0x2764,  0x2795, 0x2797,
    0x27a1, 0x27a1,  0x27b0, 0x27b0,  0x27bf, 0x27bf,  0x2934, 0x2935,  0x2b05, 0x2b07,
    0x2b1b, 0x2b1c,  0x2b50, 0x2b50,  0x2b55, 0x2b55,  0x3030, 0x3030,  0x303d, 0x303d,
    0x3297, 0x3297,  0x3299, 0x3299,  0x1f004, 0x1f004,  0x1f02c, 0x1f02f,  0x1f094, 0x1f09f,
    0x1f0af, 0x1f0b0,  0x1f0c0, 0x1f0c0,  0x1f0cf, 0x1f0d0,  0x1f0f6, 0x1f0ff,  0x1f170, 0x1f171,
    0x1f17e, 0x1f17f,  0x1f18e, 0x1f18e,  0x1f191, 0x1f19a,  0x1f1ae, 0x1f1ff,  0x1f201, 0x1f20f,
    0x1f21a, 0x1f21a,  0x1f22f, 0x1f22f,  0x1f232, 0x1f23a,  0x1f23c, 0x1f23f,  0x1f249, 0x1f25f,
    0x1f266, 0x1f321,  0x1f324, 0x1f393,  0x1f396, 0x1f397,  0x1f399, 0x1f39b,  0x1f39e, 0x1f3f0,
    0x1f3f3, 0x1f3f5,  0x1f3f7, 0x1f4fd,  0x1f4ff, 0x1f53d,  0x1f549, 0x1f54e,  0x1f550, 0x1f567,
    0x1f56f, 0x1f570,  0x1f573, 0x1f57a,  0x1f587, 0x1f587,  0x1f58a, 0x1f58d,  0x1f590, 0x1f590,
    0x1f595, 0x1f596,  0x1f5a4, 0x1f5a5,  0x1f5a8, 0x1f5a8,  0x1f5b1, 0x1f5b2,  0x1f5bc, 0x1f5bc,
    0x1f5c2, 0x1f5c4,  0x1f5d1, 0x1f5d3,  0x1f5dc, 0x1f5de,  0x1f5e1, 0x1f5e1,  0x1f5e3, 0x1f5e3,
    0x1f5e8, 0x1f5e8,  0x1f5ef, 0x1f5ef,  0x1f5f3, 0x1f5f3,  0x1f5fa, 0x1f64f,  0x1f680, 0x1f6c5,
    0x1f6cb, 0x1f6d2,  0x1f6d5, 0x1f6e5,  0x1f6e9, 0x1f6e9,  0x1f6eb, 0x1f6f0,  0x1f6f3, 0x1f6ff,
    0x1f777, 0x1f77a,  0x1f7da, 0x1f7ff,  0x1f80c, 0x1f80f,  0x1f848, 0x1f84f,  0x1f85a, 0x1f85f,
    0x1f888, 0x1f88f,  0x1f8ae, 0x1f8af,  0x1f8bc, 0x1f8bf,  0x1f8c2, 0x1f8ff,  0x1f90c, 0x1f93a,
    0x1f93c, 0x1f945,  0x1f947, 0x1f9ff,  0x1fa54, 0x1fa5f,  0x1fa6e, 0x1faff,  0x1fc00, 0x1fffd,
}

local function emojiBase(code)
    if not code then return false end
    if code <= 127 then return code == 35 or code == 42 or (code >= 48 and code <= 57) end
    if code == 0xe50a then return true end -- Legacy private-use entry included by Twemoji.
    if code > 0x1fffd or (code > 0x3299 and code < 0x1f004) then return false end
    local low, high = 1, #ranges / 2
    while low <= high do
        local mid = math.floor((low + high) / 2)
        if code < ranges[2 * mid - 1] then high = mid - 1
        elseif code > ranges[2 * mid] then low = mid + 1
        else return true end
    end
    return false
end

-- WoW's Lua does not require the utf8 library. Leave malformed input literal.
local function codeAt(text, pos)
    local a, b, c, d = text:byte(pos, pos + 3)
    if not a then return nil, pos end
    if a < 0xc2 or a > 0xf4 or not b or b < 0x80 or b > 0xbf then return a, pos + 1 end
    if a < 0xe0 then return (a - 0xc0) * 64 + b - 0x80, pos + 2 end
    if not c or c < 0x80 or c > 0xbf then return a, pos + 1 end
    if a < 0xf0 then return (a - 0xe0) * 4096 + (b - 0x80) * 64 + c - 0x80, pos + 3 end
    if not d or d < 0x80 or d > 0xbf then return a, pos + 1 end
    return (a - 0xf0) * 262144 + (b - 0x80) * 4096 + (c - 0x80) * 64 + d - 0x80, pos + 4
end

local VS15, VS16, ZWJ = 0xfe0e, 0xfe0f, 0x200d
local emojiSelector = string.char(0xef, 0xb8, 0x8f)
local function regional(c) return c and c >= 0x1f1e6 and c <= 0x1f1ff end

local function extensions(text, pos)
    local code, tail = codeAt(text, pos)
    while code and (code == VS16 or code == VS15 or (code >= 0x1f3fb and code <= 0x1f3ff)
        or (code >= 0xe0020 and code <= 0xe007f)) do
        pos = tail
        code, tail = codeAt(text, pos)
    end
    return pos
end

-- Emoji-capable monochrome characters in the cmaps of the bundled faces.
local latinText = {[0xa9]=true, [0xae]=true, [0x2122]=true}
local cjkText = {}
for _, code in ipairs({
    0xae,0x203c,0x2049,0x2122,0x2194,0x2195,0x2196,0x2197,0x2198,0x2199,
    0x21a9,0x21aa,0x25aa,0x25b6,0x25c0,0x260e,0x2640,0x2642,0x265f,0x2660,
    0x2663,0x2665,0x2666,0x2668,0x2702,0x2708,0x2709,0x270c,0x270d,0x270f,
    0x2712,0x2714,0x2716,0x271d,0x2721,0x2733,0x2734,0x2744,0x2747,0x2763,
    0x2764,0x27a1,
}) do cjkText[code] = true end
local cjkPunctuation = {[0x3030]=true, [0x303d]=true, [0x3297]=true, [0x3299]=true}

local function cluster(text, first, pos, textForms)
    local nextCode, after = codeAt(text, pos)
    if first < 128 then
        if nextCode == VS16 then nextCode, after = codeAt(text, after) end
        if nextCode == 0x20e3 then return after, "icon" end
        return pos
    end
    if nextCode == VS15 then return after, textForms[first] and "text" or "omit" end
    if regional(first) then return extensions(text, regional(nextCode) and after or pos), "icon" end
    if (textForms[first] or cjkPunctuation[first]) and nextCode ~= VS16 and nextCode ~= ZWJ
        and not (nextCode and nextCode >= 0x1f3fb and nextCode <= 0x1f3ff) then return pos end
    pos = extensions(text, pos)
    local join, tail = codeAt(text, pos)
    while join == ZWJ do
        local other, endOfOther = codeAt(text, tail)
        if not other or other < 128 or not emojiBase(other) then return tail, "omit" end
        pos = extensions(text, endOfOther)
        join, tail = codeAt(text, pos)
    end
    return pos, "icon"
end

-- Symbols an advisor's answer commonly uses that the bundled PT faces do not draw; the game shows a box for
-- them. A string is a look-alike the face has, a number is the emoji whose picture stands in.
-- The CJK fallback face draws them itself.
local stand = {
    [0x2192] = "›", [0x21d2] = "›", [0x27f6] = "›", [0x2794] = "›", [0x279c] = "›", [0x279d] = "›", [0x279e] = "›", [0x21e8] = "›",
    [0x2190] = "‹", [0x21d0] = "‹", [0x27f5] = "‹",
    [0x2191] = 0x2b06, [0x21d1] = 0x2b06, [0x2193] = 0x2b07, [0x21d3] = 0x2b07,
    [0x2713] = 0x2714, [0x2717] = 0x2716, [0x2718] = 0x2716,
    [0x2605] = 0x2b50, [0x2606] = 0x2b50, [0x25a0] = 0x25aa, [0x25a1] = 0x25ab, [0x25cf] = 0x26ab, [0x25cb] = 0x26aa,
    [0x25ba] = 0x25b6, [0x25b8] = 0x25b6, [0x25c4] = 0x25c0,
}

local function utf8Of(code)
    if code < 0x800 then return string.char(0xc0 + math.floor(code / 64), 0x80 + code % 64) end
    return string.char(0xe0 + math.floor(code / 4096), 0x80 + math.floor(code / 64) % 64, 0x80 + code % 64)
end

local markupBySize = {}
local function tileMarkup(tile, size)
    local cache = markupBySize[size]
    if not cache then cache = {}; markupBySize[size] = cache end
    if cache[tile] then return cache[tile] end
    local atlas = T.EmojiAtlas
    local capacity = atlas.columns * atlas.rows
    local slot = tile % capacity
    local page = string.format(atlas.pageFormat, atlas.firstPage + math.floor(tile / capacity))
    local left = (slot % atlas.columns) * atlas.cell + atlas.gutter
    local top = math.floor(slot / atlas.columns) * atlas.cell + atlas.gutter
    local extent = atlas.cell - 2 * atlas.gutter
    -- Same pixel coordinates and argument order as Blizzard's CreateTextureMarkup.
    local tag = string.format("|T%s:%d:%d:0:0:%d:%d:%d:%d:%d:%d|t",
        T.media .. atlas.prefix .. page .. atlas.suffix, size, size, atlas.size, atlas.size,
        left, left + extent, top, top + extent)
    cache[tile] = tag
    return tag
end

local terminators = { H = "|h", T = "|t", A = "|a", K = "|k" }
local function skipMarkup(text, pos)
    local marker = text:sub(pos + 1, pos + 1)
    if marker == "|" then return pos + 2 end
    local terminator = terminators[marker]
    if terminator then
        local at = text:find("|", pos + 2, true)
        while at do
            local tag = text:sub(at, at + 1)
            if tag == terminator then return at + 2 end
            at = text:find("|", at + (tag == "||" and 2 or 1), true)
        end
        return #text + 1
    end
    return pos + 1
end

-- Called only for display. Hyperlink targets, existing textures and URLs remain
-- byte-for-byte identical; the visible caption is processed as ordinary text.
function T.DisplayEmoji(text, fontSize, useCJK)
    if type(text) ~= "string" or not text:find("[\194\226\227\238\240]") then return text end
    local size = math.max(1, math.floor((fontSize or 17) + .5))
    local forms = useCJK and cjkText or latinText
    local output, omitted
    local function append(chunk, final)
        if omitted and (final or chunk ~= "") then
            local previous = output[#output]
            if not previous or previous:find("%s$") then chunk = chunk:gsub("^ +", "") end
            if previous and ((final and chunk == "") or chunk:find("^[,.;:!?)%]%}\n]")) then
                output[#output] = previous:gsub(" +$", "")
            end
            omitted = false
        end
        if chunk ~= "" then output[#output + 1] = chunk end
    end
    local start, pos = 1, 1
    while pos <= #text do
        local code, nextPos = codeAt(text, pos)
        if code == 124 then
            pos = skipMarkup(text, pos)
        elseif code == 104 and (text:sub(pos,pos+7) == "https://" or text:sub(pos,pos+6) == "http://") then
            pos = text:find("[%s|]", nextPos) or #text + 1
        elseif not useCJK and stand[code] then
            local swap = stand[code]
            if type(swap) == "number" then
                local tile = T.EmojiIndex and T.EmojiIndex[utf8Of(swap)]
                swap = tile ~= nil and tileMarkup(tile, size) or ""
            end
            output = output or {}
            append(text:sub(start, pos - 1))
            if swap == "" then omitted = true else append(swap) end
            -- A text or emoji presentation selector after the symbol has nothing left to select.
            local selector, after = codeAt(text, nextPos)
            start = (selector == VS15 or selector == VS16) and after or nextPos
            pos = start
        elseif emojiBase(code) then
            local finish, mode = cluster(text, code, nextPos, forms)
            if mode then
                local replacement = ""
                if mode == "text" then replacement = text:sub(pos, nextPos - 1)
                elseif mode == "icon" then
                    local key = text:sub(pos, finish - 1):gsub(emojiSelector, "")
                    local tile = T.EmojiIndex and T.EmojiIndex[key]
                    if tile ~= nil then replacement = tileMarkup(tile, size) end
                end
                output = output or {}
                append(text:sub(start, pos - 1))
                if replacement == "" then omitted = true else append(replacement) end
                start = finish
            end
            pos = finish
        else
            pos = nextPos
        end
    end
    if not output then return text end
    append(text:sub(start), true)
    return table.concat(output)
end
