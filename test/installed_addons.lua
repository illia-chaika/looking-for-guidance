-- Exercise the normal snapshot and the live inventory mailbox, including a scan larger than a single packet.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local guid = "Player-123-000A"
function UnitGUID(unit) if unit == "player" then return guid end end
local T = wow.Load(here .. "/../looking-for-guidance")
local _, login = T.CharacterIdentity()
local names = { "looking-for-guidance-bit-001", "Foo", "looking-for-guidance", "looking-for-guidance-slot-1024",
    "looking-for-guidance-payload", "looking-for-guidance-Extra", "Blizzard_Test", "NotYetLoaded", "NeedsLibrary" }
for i = 1, 120 do names[#names + 1] = string.format("Addon%03d", i) end
local countReads, stateReads = 0, 0
local api = C_AddOns
Enum.AddOnSecurityStatus = { Secure = 0, Insecure = 1, Banned = 2, NotAvailable = 3 }
Enum.AddOnEnableState = { None = 0, Some = 1, All = 2 }
api.GetNumAddOns = function() countReads = countReads + 1; return #names end
api.GetAddOnName = function(index) return names[index] end
api.GetAddOnTitle = function(index) return "|cff123456" .. names[index] .. "|r" end
api.GetAddOnMetadata = function(_, key) assert(key == "Version"); return "2.3" end
api.GetAddOnDependencies = function(index)
    if names[index] == "NeedsLibrary" then return "LibraryOne", "Foo", "LibraryThree", "LibraryFour" end
end
api.DoesAddOnHaveLoadError = function(index)
    if names[index] == "looking-for-guidance-slot-1024" then error("module load-error flag unavailable") end
    return names[index] == "Addon001" or names[index] == "looking-for-guidance-payload"
end
api.GetAddOnEnableState = function(index, character)
    assert(character == guid); stateReads = stateReads + 1
    if names[index] == "NotYetLoaded" then return 0 end
    return 2
end
api.IsAddOnLoaded = function(index) return true, names[index] ~= "NotYetLoaded" end
api.IsAddOnLoadOnDemand = function(index) return names[index] == "NotYetLoaded" end
api.IsAddOnLoadable = function(index, character)
    assert(character == guid)
    if names[index] == "NeedsLibrary" then return false, "DEP_MISSING" end
    return true, ""
end
api.GetAddOnSecurity = function(index)
    local byName = { Foo = "BANNED", NeedsLibrary = 2, Blizzard_Test = "SECURE_PROTECTED", NotYetLoaded = "FUTURE_STATUS", Addon003 = 3,
        ["looking-for-guidance-bit-001"] = "BANNED", ["looking-for-guidance-payload"] = "FUTURE_STATUS",
        ["looking-for-guidance-slot-1024"] = "NOT_AVAILABLE" }
    return byName[names[index]] or "INSECURE"
end

local marker = "Installed add-ons: available on request"
local ordinary = T.Context.ForQuestion("run-a")
assert(ordinary:find(marker, 1, true) and not ordinary:find("NeedsLibrary", 1, true) and countReads == 0)
assert(T.Context.Preview():find(marker, 1, true) and countReads == 0, "preview must not enumerate the installation")
local serial = 0
local function request(scan, offset, id, session, character)
    serial = serial + 1
    local outer = id or ("addons-" .. serial)
    wow.payload.detailRequest = { id = outer, session = session or T.Transport.session, character = character or guid, login = login,
        addons = { id = string.rep(scan, 32), offset = offset } }
    local previous = #wow.sent
    looking_for_guidance_receive("slot")
    for i = #wow.sent, previous + 1, -1 do
        local packet = wow.sent[i]
        if packet.requestId == outer and packet.addons then return packet.addons, packet.seq end
    end
end
local first, firstSeq = request("a", 0, "first")
assert(first and firstSeq)
assert(first.total == #names - 3 and #first.rows < first.total and #first.rows > 0)
assert(countReads == 1 and stateReads == first.total, "transport modules must not be probed individually")
assert(request("a", 0, "first") == nil and countReads == 1, "a repeated mailbox must not rescind or regenerate the reply")
local seen = {}
local function collect(rows)
    for _, row in ipairs(rows) do assert(not seen[row.name]); seen[row.name] = row end
end
collect(first.rows)
local nextOffset = #first.rows
names[#names] = "ChangedBetweenPages"
assert(request("b", 0).total == first.total, "another chat can use the same mailbox")
while nextOffset < first.total do
    local part = assert(request("a", nextOffset))
    assert(part.offset == nextOffset and part.total == first.total and #part.rows > 0)
    collect(part.rows)
    nextOffset = nextOffset + #part.rows
end
assert(countReads == 2 and seen.Addon120 and not seen.ChangedBetweenPages, "pages are one coherent observation")
local parent = seen["looking-for-guidance"]
assert(parent.groupedModules == 3 and parent.bannedModules == 1 and parent.unknownSecurityModules == 1 and parent.unavailableSecurityModules == 1
    and parent.loadErrorModules == 1 and parent.unknownLoadErrorModules == 1 and parent.loadError == false
    and parent.security == "INSECURE" and seen["looking-for-guidance-Extra"])
assert(seen.Foo.security == "BANNED" and seen.Foo.title == "Foo" and seen.Foo.version == "2.3")
assert(seen.NeedsLibrary.reason == "DEP_MISSING" and not seen.NeedsLibrary.loadable and seen.NeedsLibrary.security == "BANNED")
assert(seen.NeedsLibrary.dependenciesKnown and table.concat(seen.NeedsLibrary.dependencies, ",") == "LibraryOne,Foo,LibraryThree,LibraryFour")
assert(seen.Foo.dependenciesKnown and seen.Foo.dependencies == nil)
assert(seen.Addon001.loadError == true and seen.Addon002.loadError == false)
assert(seen.Addon003.security == "NOT_AVAILABLE")
assert(seen.Blizzard_Test.security == "SECURE_PROTECTED" and seen.Addon001.security == "INSECURE")
assert(seen.NotYetLoaded.loaded == false and seen.NotYetLoaded.enabled == false and seen.NotYetLoaded.loadOnDemand == true
    and seen.NotYetLoaded.security == "UNKNOWN")

T.settings.context.addons = false
assert(not T.Context.Text():find(marker, 1, true))
T.Fire("PacketSent", firstSeq)
wow.now = wow.now + 3
local retry = T.Transport.Retry
T.Transport.Retry = function(seq) assert(seq ~= firstSeq, "must not replay an inventory packet after access is revoked"); return false end
T.Store.Apply(wow.payload)
T.Transport.Retry = retry
local denied = request("b", 1)
assert(denied.error and not denied.rows and not denied.total and countReads == 2)
T.settings.context.addons = true
assert(request("b", 1).error and countReads == 2, "revoked scans cannot be continued after re-enabling access")
T.settings.extras = false
assert(request("c", 0).error and countReads == 2)
T.settings.extras = true
assert(request("c", 0, nil, "foreign") == nil and countReads == 2)
assert(request("c", 0, nil, nil, "Player-123-000B").error and countReads == 2)

names = {}
local empty = request("c", 0)
assert(empty.total == 0 and not empty.error)
api.GetNumAddOns = nil
assert(request("d", 0).error, "unavailable is different from a successful empty scan")
names = { "PrivateName" }
api.GetNumAddOns = function() return #names end
local secret = {}
issecretvalue = function(v) return v == secret end
api.GetAddOnDependencies = function() error("dependency information unavailable") end
local depError = request("1", 0).rows[1]
assert(depError.dependenciesKnown == false and depError.dependencies == nil and depError.loadError == false)
api.GetAddOnDependencies = nil
assert(request("2", 0).rows[1].dependenciesKnown == false)
api.GetAddOnDependencies = function() return "PublicLibrary", secret end
local protected = request("3", 0).rows[1]
assert(protected.dependenciesKnown == false and protected.dependencies == nil)
api.DoesAddOnHaveLoadError = function() return secret end
assert(request("4", 0).rows[1].loadError == nil)
api.DoesAddOnHaveLoadError = nil
assert(request("5", 0).rows[1].loadError == nil)
api.GetAddOnSecurity = function() return secret end
api.IsAddOnLoaded = nil
api.GetAddOnDependencies = function() return secret end
local partial = request("e", 0).rows[1]
assert(partial.security == "UNKNOWN" and partial.loaded == nil and partial.dependencies == nil)
api.GetAddOnTitle = function() return string.rep("title", 1800) end
local oversized = request("7", 0)
assert(oversized.error and not oversized.total and not oversized.rows, "one oversized entry cannot be quietly dropped")
api.GetAddOnName = function() return secret end
assert(request("f", 0).error, "an unidentified add-on must not disappear from a supposedly complete list")
print("Installed add-ons: permissions, identities, client status, grouping, paging and unavailable APIs OK")
