local _, T = ...
local freshLogin = false

local function identity()
    if not T.settings or not UnitGUID then return end
    local ok, guid = pcall(UnitGUID, 'player')
    if not ok or (issecretvalue and issecretvalue(guid)) or type(guid) ~= 'string'
        or not guid:match('^Player%-%d+%-%x+$') then return end
    local previous = T.settings.memoryLogin
    if freshLogin or type(previous) ~= 'table' or previous.character ~= guid or not previous.token then
        previous = {character=guid, token=T.Transport.session}
        T.settings.memoryLogin = previous
        freshLogin = false
    end
    return guid, previous.token
end
T.CharacterIdentity = identity

local frame = CreateFrame('Frame')
frame:RegisterEvent('PLAYER_ENTERING_WORLD')
frame:SetScript('OnEvent', function(_, _, initialLogin)
    if initialLogin then
        local previous = T.settings and T.settings.memoryLogin
        freshLogin = true
        -- PLAYER_LOGIN can precede this event. Both calls use the same
        -- transport token on a new login; /reload preserves the saved token.
        local guid, login = identity()
        if guid and previous and previous.token ~= login then
            local seq = T.Transport.Send({type='hello', addon=T.version, character=guid, login=login})
            if seq then T.Store.Track(seq) end
        end
    end
end)
