-- Every caption of the journal cards added in 0.10 has all eight translations.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
local files = { "HubCheckup.lua", "HubNews.lua", "HubQuestCheck.lua", "HubGold.lua", "HubDungeon.lua", "HubBags.lua", "HubLeveling.lua", "Insights.lua" }
local missing, seen, count = {}, {}, 0
local function need(key, file)
    if seen[key] or not key:find("[\208\209]") then return end
    seen[key], count = true, count + 1
    local row = T.LocaleRows[key]
    local complete = type(row) == "table"
    for column = 1, 8 do complete = complete and type(row[column]) == "string" and row[column] ~= "" end
    if complete then
        for index in key:gmatch("{(%d+)}") do
            for column = 1, 8 do complete = complete and row[column]:find("{" .. index .. "}", 1, true) ~= nil end
        end
    end
    if not complete then missing[#missing + 1] = file .. ": " .. key end
end
for _, file in ipairs(files) do
    local handle = io.open(here .. "/../looking-for-guidance/" .. file)
    if handle then
        local source = handle:read("*a")
        handle:close()
        -- Every Russian string literal in these files is a caption key.
        for key in source:gmatch('"([^"\n]*)"') do need(key, file) end
    end
end
need("Касается тебя напрямую", "HubNews.lua")
need("Полезно знать", "HubNews.lua")
need("Проверено: {1}", "HubPage.lua")
need("Каналов связи осталось: {1}", "Alerts.lua")
if #missing > 0 then
    error("captions without eight translations:\n" .. table.concat(missing, "\n"))
end
print("Locale: " .. count .. " new journal captions have all eight translations and their placeholders")
