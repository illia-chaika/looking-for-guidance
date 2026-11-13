local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local T = {}
for _, name in ipairs({"Core", "LocaleData", "Emoji", "Locale", "Tokens", "Format", "Kit", "Store", "Actions", "ModelPicker", "Composer"}) do
    assert(loadfile(here .. "/../looking-for-guidance/" .. name .. ".lua"))("looking-for-guidance", T)
end
wow.Event("ADDON_LOADED", "looking-for-guidance")
local composer = T.Composer.Build(UIParent, {})
local menu, meter = composer.picker, composer.meter
local active = {id="test-thread", agent="codex", model="a", effort="medium", contextLeft=91, contextUsed=53020, contextWindow=475000}
T.Store.models = {
    {id="a", label="A", default=true, effort="medium", efforts={"low", "medium", "high"}},
    {id="b", label="B", effort="low", efforts={"low", "x-extra"}},
    {id="c", label="C"}, {id="d", label="D"}, {id="e", label="E"}, {id="f", label="F"},
}
T.settings.choice.model, T.settings.choice.effort = "a", "medium"
composer:Set({context="Brill · Ур. 10", choice="A · medium", thread=active, placeholder=""})
local function choose(kind, value)
    for _, row in ipairs(menu.rows) do
        if row.kind == kind and row.value == value and row:IsShown() then row:Fire("OnClick") return end
    end
    error("missing visible choice: " .. kind .. " " .. tostring(value))
end
assert(meter.text:GetText():find("91%%"))
for i, n in ipairs({"53 020","421 980","475 000"}) do assert(meter.numbers[i]:GetText() == n) end
meter:Fire("OnEnter")
assert(meter.tip:IsShown())
meter:Fire("OnClick")
meter:Fire("OnLeave")
assert(meter.tip:IsShown() and meter.tip.pinned)
composer.choice:Fire("OnClick")
assert(menu:IsShown() and not meter.tip:IsShown())
choose("more")
choose("model", "f")
assert(T.Actions.Choice(active).model.id == "f" and not menu.level:IsShown())
local creates = #wow.sent
choose("model","b")
assert(T.Actions.Choice(active).effort == "low" and menu.mode == "effort")
choose("effort","x-extra")
assert(T.Actions.Choice(active).effort == "x-extra" and menu.mode == "model")
menu.level:Fire("OnClick")
assert(menu.mode == "effort")
menu.back:Fire("OnClick")
assert(menu.mode == "model")
assert(active.model == "a" and active.effort == "medium" and T.Actions.Choice().model.id == "a" and #wow.sent == creates)
menu:Hide()
composer:SetWidth(310)
composer:Set({context="Brill · Ур. 10", choice="B · x-extra", thread={contextLeft=0}, placeholder=""})
assert(meter.text:GetText() == "0%" and not meter.numbers[1]:IsShown())
assert(meter.note:GetText():find("0%%"))
assert(meter:GetWidth() + composer.choice:GetWidth() + composer.context:GetWidth() + 16 + 2*T.Kit.space["space-3"] <= composer:GetWidth()+0.01)
composer:Set({context="Brill", choice="B · x-extra", thread={}, placeholder=""})
assert(not meter.text:GetText():find("%%") and meter.note:GetText():find("Замер"))
print("Footer: live model and effort catalog, per-chat next-turn choice, zero/missing telemetry, token counts, pinned tooltip, narrow layout OK")
