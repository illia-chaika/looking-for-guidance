local _, T = ...
local Links = {}
T.GameLinks = Links

-- Keep the native hyperlink in the edit box. WoW renders only its caption but
-- GetText preserves the instance's enchants, random suffix, bonus IDs, etc.
local function reference(payload, caption)
    if #payload > 4096 or payload:find('[%c|]') or #caption > 1024 then return end
    local kind, tail = payload:match('^([A-Za-z][A-Za-z0-9_]*):(.+)$')
    if not kind or #kind > 64 then return end
    -- The caption may include a class icon, textures, a nested colour or no brackets.
    local name = caption:gsub('|T.-|t', ''):gsub('|A.-|a', ''):gsub('|c%x%x%x%x%x%x%x%x', ''):gsub('|cn[%w_]+:', ''):gsub('|r', '')
    name = name:match('^%s*%[(.*)%]%s*$') or name
    if name:find('[%c|]') then return end
    name = name:gsub('%[','('):gsub('%]',')')
    if #name > 512 then return end
    local first = tail:match('^([^:]+)')
    local id = first and first:match('^%d+$') and tonumber(first)
    if id and (id <= 0 or id > 2147483647) then id = nil end
    if (kind == 'item' or kind == 'spell' or kind == 'quest') and not id then return end
    return {kind=kind, id=id, link=payload, name=name ~= '' and name or kind}
end
local linkPattern = '|H([^|]+)|h(.-)|h'
function Links.RestoreReferences(stored)
    local refs = {}
    for _, entry in ipairs(stored or {}) do
        if type(entry.link) == "string" and type(entry.name) == "string" then
            local ref = reference(entry.link, entry.name)
            if ref then refs[#refs+1] = ref end
        end
    end
    return refs
end
local function insertable(link)
    if (issecretvalue and issecretvalue(link)) or type(link) ~= 'string' or #link > 8192 or link == '' then return false end
    if not link:find('|',1,true) then return not link:find('%c') end
    local body = link:gsub('^|c%x%x%x%x%x%x%x%x',''):gsub('^|cn[%w_]+:',''):gsub('|r$','')
    local payload, caption = body:match('^' .. linkPattern .. '$')
    return payload and reference(payload, caption)
end

function Links.Prepare(text)
    -- Only game links are converted. Typed prose, brackets and newlines survive.
    local refs = {}
    local function normalize(payload, name)
        local ref = reference(payload, name)
        if not ref then return name end
        refs[#refs+1] = ref
        local address = (ref.kind == 'item' or ref.kind == 'quest' or ref.kind == 'spell')
            and (ref.kind .. ':' .. ref.id) or ('wowref:' .. #refs)
        return '[' .. ref.name .. '](' .. address .. ')'
    end
    local pieces, start = {}, 1
    while true do
        local first, last, payload, name = text:find(linkPattern, start)
        if not first then break end
        local before = text:sub(start, first-1)
        local prefix = before:match('()|c%x%x%x%x%x%x%x%x$') or before:match('()|cn[%w_]+:$')
        if prefix and text:sub(last+1,last+2) == '|r' then
            before, last = before:sub(1,prefix-1), last+2
        end
        pieces[#pieces+1] = before
        pieces[#pieces+1] = normalize(payload, name)
        start = last+1
    end
    pieces[#pieces+1] = text:sub(start)
    return table.concat(pieces), refs
end

function Links.Display(text, colour, attachments)
    local used = {}
    local escaped = T.Format.Escape(text)
    local ink = type(colour) == 'string' and colour:match('^%x%x%x%x%x%x%x%x$') or 'ffdbba72'
    return (escaped:gsub('%[([^%[%]|]+)%]%((%a+):(%d+)%)', function(name,kind,id)
        if kind == 'wowref' then
            local attached = (attachments or {})[tonumber(id)]
            if attached and attached.name == name and type(attached.link) == 'string' and reference(attached.link, name) then
                return '|c' .. ink .. '|H' .. attached.link .. '|h[' .. name .. ']|h|r'
            end
            return '[' .. name .. ']'
        end
        if kind ~= 'item' and kind ~= 'quest' and kind ~= 'spell' then return nil end
        local ref = reference(kind..':'..id,name)
        if not ref then return nil end
        local payload = ref.link
        if kind == 'quest' then payload = payload .. ':0' end
        for i, a in ipairs(attachments or {}) do
            if not used[i] and a.name == name and type(a.link) == 'string' then
                local exact = reference(a.link,name)
                if exact and exact.kind == kind and exact.id == ref.id then
                    used[i],payload = true,exact.link
                    break
                end
            end
        end
        return '|c' .. ink .. '|H' .. payload .. '|h[' .. name .. ']|h|r'
    end))
end

local draftBox, hooked
function Links.InsertFocused(link)
    local box = draftBox
    if box and box:IsVisible() and box:HasFocus() and
        (not IsModifiedClick or IsModifiedClick('CHATLINK')) and insertable(link) then
        box:Insert(link .. ' ')
        return true
    end
end

function Links.Bind(edit)
    draftBox = edit
    if hooked then return end
    local function route(original)
        return function(link, ...)
            -- Mark the gesture handled so the standard UI does not also untrack
            -- a quest or split the stack. Other edit boxes retain normal handling.
            if Links.InsertFocused(link) then return true end
            return original(link, ...)
        end
    end
    -- Public UI dispatch in the Forever branch. Leave all unfamiliar links and
    -- other edit boxes with Blizzard / previously installed addon handlers.
    if ChatFrameUtil and type(ChatFrameUtil.InsertLink) == 'function' then
        ChatFrameUtil.InsertLink = route(ChatFrameUtil.InsertLink)
        hooked = true
    end
    if type(ChatEdit_InsertLink) == 'function' then
        ChatEdit_InsertLink = route(ChatEdit_InsertLink)
        hooked = true
    end
end
