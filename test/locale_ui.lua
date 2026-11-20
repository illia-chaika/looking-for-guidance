-- Exercise GetLocale, staged changes, non-ASCII text and saved chat content through the same dialog used in WoW.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.locale = "enUS"
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
T.Window.Show()
local function control(match)
    for _, widget in ipairs(wow.frames) do
        if widget:IsVisible() and match(widget) then return widget end
    end
    error("expected control is not visible", 2)
end
local function named(text)
    return control(function(v) return (v.label and v.label:GetText() == text) or (v.text and v.text:GetText() == text) end)
end
local function visibleText(value)
    for _, widget in ipairs(wow.texts) do
        if widget:IsVisible() and widget:GetText() == value then return true end
    end
    return false
end
local function openLanguage()
    control(function(v) return v.glyph and v.glyph.name == "gear" end):Fire("OnClick")
    named(T.L("Язык")):Fire("OnClick")
end
local snapshot = T.Store.Threads()[1].title
assert(T.GetUILocale() == "enUS" and T.settings.locale == "auto")
assert(#T.LocaleOptions == 12 and T.L("Сохранить") == "Save")
assert(T.FontFile("PTSans-Regular.ttf", "Which item?") == T.media .. "PTSans-Regular.ttf")
assert(T.FontFile("PTSans-Regular.ttf", "어느 아이템?") == T.media .. "DroidSansFallback.ttf")
assert(T.FontFile("PTSans-Regular.ttf", "物品") == T.media .. "DroidSansFallback.ttf")
openLanguage()
for _, choice in ipairs(T.LocaleOptions) do
    local name = choice.code == "auto" and T.L(choice.name) or choice.name
    named(name):Fire("OnClick")
    local question = T.LFor(choice.code, "Куда идти дальше?")
    local title = T.LFor(choice.code, "Отнеси приказы в Подгород.")
    assert(visibleText(question) and visibleText(title), "language preview must follow staged " .. choice.code)
    if choice.code ~= "ruRU" then assert(title ~= "Отнеси приказы в Подгород.") end
    named("Appearance"):Fire("OnClick")
    local highlight = "|cff" .. T.Readers.journal.quest
    local example = T.LFor(choice.code, "Там можно сдать {1}два задания{2} за одну поездку. В журнале указаны оба получателя.", highlight, "|r")
    assert(visibleText(question) and visibleText(title) and visibleText(example), "appearance preview must follow staged " .. choice.code)
    assert(example:find(highlight, 1, true) and example:find("|r", 1, true))
    named("Language"):Fire("OnClick")
end
named("한국어"):Fire("OnClick")
assert(T.GetUILocale() == "enUS")
named("Cancel"):Fire("OnClick")
assert(T.settings.locale == "auto")
openLanguage()
named("한국어"):Fire("OnClick")
named("Save"):Fire("OnClick")
assert(T.GetUILocale() == "koKR" and T.L("Сохранить") == "저장")
assert(T.Store.Threads()[1].title == snapshot)
for _, choice in ipairs(T.LocaleOptions) do
    if choice.code ~= "ruRU" then assert(T.LFor(choice.code,"Сохранить") ~= "Сохранить") end
end
openLanguage()
named("Русский"):Fire("OnClick")
named("저장"):Fire("OnClick")
assert(T.L("Данные персонажа") == "Данные персонажа")
openLanguage()
named("Как в игре"):Fire("OnClick")
named("Сохранить"):Fire("OnClick")
assert(T.settings.locale == "auto" and T.GetUILocale() == "enUS")
local probe = CreateFrame("Frame", nil, UIParent):CreateFontString()
T.SetContent(probe, "всего 26\226\128\175361, 1\226\128\137000 и\226\128\139 ещё")
assert(probe:GetText() == "всего 26\194\160361, 1\194\160000 и ещё", "typographic spaces become the no-break space the fonts draw")
print("Locale: auto and 11 regional choices, staged Apply/Cancel, CJK font, live control updates and original transcript OK")
