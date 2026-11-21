local _, T = ...

local columns = { enUS = 1, enGB = 1, deDE = 2, frFR = 3, esES = 4, esMX = 4, ptBR = 5, koKR = 6, zhCN = 7, zhTW = 8 }
-- Italian and Ukrainian live in files of their own, keyed by the same Russian source (LocaleItalian.lua,
-- LocaleUkrainian.lua). The game has an Italian client; Ukrainian is chosen in the settings.
local added = { itIT = {}, ukUA = {} }
local mexico = { ["Настройки"] = "Configuración", ["Настройки советника"] = "Configuración del consejero", ["Сумки"] = "Bolsas" }

T.LocaleOptions = {
    { code = "auto", name = "Как в игре" }, { code = "enUS", name = "English (US)" },
    { code = "enGB", name = "English (UK)" }, { code = "deDE", name = "Deutsch" },
    { code = "frFR", name = "Français" }, { code = "esES", name = "Español (España)" },
    { code = "esMX", name = "Español (Latinoamérica)" }, { code = "ptBR", name = "Português (Brasil)" },
    { code = "ruRU", name = "Русский" }, { code = "koKR", name = "한국어" },
    { code = "zhCN", name = "简体中文" }, { code = "zhTW", name = "繁體中文" },
    { code = "itIT", name = "Italiano" }, { code = "ukUA", name = "Українська" },
}
local function supported(code) return code == "ruRU" or columns[code] ~= nil or added[code] ~= nil end

function T.LocaleChoice(code)
    return supported(code) and code or "auto"
end

function T.GetUILocale(code)
    code = code or (T.settings and T.settings.locale)
    if not supported(code) then code = type(GetLocale) == "function" and GetLocale() or "enUS" end
    return supported(code) and code or "enUS"
end

function T.AddLocaleRows(rows)
    for source, row in pairs(rows) do T.LocaleRows[source] = row end
end

function T.AddLocaleColumn(code, texts)
    for source, text in pairs(texts) do added[code][source] = text end
end

function T.LocaleText(code, source)
    return added[code] and added[code][source]
end

function T.LFor(code, source, ...)
    if type(source) ~= "string" then return source end
    local locale = T.GetUILocale(code)
    local row = T.LocaleRows[source]
    local pattern = locale == "esMX" and mexico[source] or added[locale] and (added[locale][source] or row and row[1])
        or row and row[columns[locale]] or source
    if select("#", ...) == 0 then return pattern end
    local args = { ... }
    return (pattern:gsub("{(%d+)}", function(index) return tostring(args[tonumber(index)] or "") end))
end

function T.L(source, ...) return T.LFor(nil, source, ...) end

local function needsAsianFont(text)
    if not text then return false end
    -- UTF-8 lead bytes cover ideographs / kana (U+3000–9FFF), Hangul syllables,
    -- and Korean jamo. Plain Latin, punctuation and Cyrillic stay in PT Sans.
    return text:find("[\228-\233][\128-\191][\128-\191]") or text:find("[\234-\237][\128-\191][\128-\191]")
        or text:find("\227[\128-\191][\128-\191]") or text:find("\225[\132-\135][\128-\191]")
end
function T.FontFile(file, text, code)
    local locale = T.GetUILocale(code)
    if needsAsianFont(text) or ((not text or text == "") and (locale == "koKR" or locale == "zhCN" or locale == "zhTW")) then
        return T.media .. "DroidSansFallback.ttf"
    end
    return T.media .. file
end

-- Preview labels follow the staged setting even before the player saves it.
function T.SetPreviewText(label, locale, source, size, spacing, ...)
    local style = T.Tokens.font[label.companionStyle]
    local text = T.LFor(locale, source, ...)
    label:SetFont(T.FontFile(style.file, text, locale), size or style.size, "")
    label:SetSpacing(spacing or style.line - style.size)
    label:SetText(text)
end

function T.ApplyEditFont(edit, spec)
    local path = T.FontFile(spec.file, edit:GetText())
    if edit.companionFont ~= path or edit.companionFontSize ~= spec.size then
        edit:SetFont(path, spec.size, "")
        edit.companionFont, edit.companionFontSize = path, spec.size
    end
end

-- The bundled fonts draw the no-break space but none of the typographic ones (thin, narrow no-break, figure…)
-- that an adviser puts into numbers such as "26 361"; those showed as boxes. Zero-width spaces are dropped.
function T.PlainSpaces(text)
    return (text:gsub("\226\128[\128-\138\175]", "\194\160"):gsub("\226\129\159", "\194\160"):gsub("\226\128\139", ""))
end

function T.SetContent(label, value)
    local style = label.companionStyle
    if type(value) == "string" then value = T.PlainSpaces(value) end
    value = T.DisplayEmoji(value, style and T.Tokens.font[style].size, type(value) == "string" and needsAsianFont(value))
    local text = type(value) == "string" and value or ""
    if style then
        local spec = T.Tokens.font[style]
        local fontFile = T.FontFile(spec.file, text)
        if fontFile ~= T.FontFile(spec.file) then
            label:SetFont(fontFile, spec.size, "")
            label:SetSpacing(spec.line - spec.size)
            label.companionTextFont = true
        elseif label.companionTextFont then
            label:SetFontObject(T.Kit.Font(style))
            label.companionTextFont = nil
        end
    end
    label:SetText(value)
end

local bindings = setmetatable({}, { __mode = "k" })
function T.BindLabel(widget, source, after)
    bindings[widget] = { source, after }
    widget:SetText(T.L(source))
    if after then after(widget) end
end

function T.RefreshLocale()
    if T.Kit then T.Kit.ApplyReading() end
    for widget, binding in pairs(bindings) do
        widget:SetText(T.L(binding[1]))
        if binding[2] then binding[2](widget) end
    end
    T.Fire("LOCALE_CHANGED")
end
