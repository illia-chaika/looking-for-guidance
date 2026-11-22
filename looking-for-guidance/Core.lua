local addonName, T = ...

T.version = C_AddOns.GetAddOnMetadata(addonName, "Version")
T.media = "Interface\\AddOns\\" .. addonName .. "\\Media\\"

local listeners = {}

function T.On(event, handler)
    listeners[event] = listeners[event] or {}
    table.insert(listeners[event], handler)
end

function T.Fire(event, ...)
    for _, handler in ipairs(listeners[event] or {}) do
        handler(...)
    end
end

local defaults = {
    scale = 1,
    opacity = 1,
    alertChat = true,
    alertSound = true,
    minimap = true,
    minimapAngle = 225,
    window = { sidebar = true },
    choice = {},
    list = { group = "day", sort = "recent" },
    extras = true,
    context = {
        character = true, location = true, quests = true, gear = true, bags = true, talents = true,
        skills = true, spells = true, target = true, reputation = true, buffs = true, addons = true,
    },
}

local function merge(saved, fallback)
    for key, value in pairs(fallback) do
        if saved[key] == nil then
            saved[key] = type(value) == "table" and CopyTable(value) or value
        elseif type(value) == "table" and type(saved[key]) == "table" then
            merge(saved[key], value)
        end
    end
    return saved
end

local events = CreateFrame("Frame")
events:RegisterEvent("ADDON_LOADED")
events:RegisterEvent("PLAYER_LOGIN")
events:SetScript("OnEvent", function(self, event, name)
    if event == "ADDON_LOADED" and name == addonName then
        looking_for_guidance_settings = merge(looking_for_guidance_settings or {}, defaults)
        T.settings = looking_for_guidance_settings
        -- Earlier versions saved false for every unselected category. Switch the
        -- existing installation once, then respect all later choices on reload.
        if not T.settings.fullContextDefaults then
            T.settings.context = CopyTable(defaults.context)
            T.settings.extras = true
            T.settings.fullContextDefaults = true
        end
        -- Existing installs keep their saved width and sidebar choice.
        T.settings.compactLayout = true
    elseif event == "PLAYER_LOGIN" then
        self:UnregisterAllEvents()
        T.Window.Init()
        T.Alerts.Init()
        T.Sync.Start()
    end
end)

SLASH_LOOKING_FOR_GUIDANCE1 = "/ask"
SLASH_LOOKING_FOR_GUIDANCE2 = "/guidance"
SlashCmdList.LOOKING_FOR_GUIDANCE = function()
    T.Window.Toggle()
end

function looking_for_guidance_on_addon_compartment_click()
    T.Window.Toggle()
end

function looking_for_guidance_on_addon_compartment_enter(_, button)
    T.Alerts.Tooltip(button)
end

function looking_for_guidance_on_addon_compartment_leave()
    GameTooltip:Hide()
end
