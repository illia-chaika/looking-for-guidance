local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
local guid
T.CharacterIdentity = function() return guid, "test-login" end
local function build()
    return T.Composer.Build(CreateFrame("Frame", nil, UIParent), {send = function() return true end})
end
local function set(composer, key, offline)
    composer:Set({offline=offline, disabled=offline, thread=key and {id=key, state="idle"}, placeholder="Write here", context=""})
end
local item = "|cffffffff|Hitem:35:7:0:-15|h[Ветхий посох]|h|r"
local text = "Сравни " .. item .. "\nещё вопрос"
local composer = build()
set(composer, nil, true)
composer.edit:SetText(text)
guid = "Player-1-A"
wow.Event("PLAYER_ENTERING_WORLD")
assert(T.settings.characterDrafts[guid].offline == text, "GUID became available after editing")
set(composer, "first")
assert(composer.edit:GetText() == text and not T.settings.characterDrafts[guid].offline, "first online chat receives scratchpad")
set(composer, "second")
assert(composer.edit:GetText() == "")
composer.edit:SetText("второй вопрос")
set(composer, "first")
assert(composer.edit:GetText() == text)

-- Simulate a fresh edit box on /reload with the saved character settings.
composer = build()
set(composer, "first")
assert(composer.edit:GetText() == text)
set(composer, nil, true)
composer.edit:SetText("пока без связи")
set(composer, "second")
assert(composer.edit:GetText() == "второй вопрос", "offline text must not overwrite an existing thread draft")
set(composer, nil)
assert(composer.edit:GetText() == "пока без связи", "conflicting scratchpad remains accessible in a new chat")
composer:AdoptDraft("new", "local:first")
assert(composer.edit:GetText() == "пока без связи")
set(composer, "second")
composer:AdoptDraft("local:first", "created")
set(composer, "created")
assert(composer.edit:GetText() == "пока без связи" and not T.settings.characterDrafts[guid]["local:first"])
set(composer, nil)
composer.edit:SetText("сохрани черновик при обсуждении")
set(composer, "local:already-selected")
composer:AdoptDraft("new", "local:already-selected")
assert(composer.edit:GetText() == "сохрани черновик при обсуждении")
guid = "Player-2-B"
set(composer, "first")
assert(composer.edit:GetText() == "", "draft from a different character is hidden")
composer.edit:SetText("другой персонаж")
guid = "Player-1-A"
set(composer, "first")
assert(composer.edit:GetText() == text)

-- Drive native cursor coordinates and simulate wrapped/explicit newlines.
local edit = composer.edit
local viewport = edit:GetParent()
local field = viewport:GetParent()
local line = T.Tokens.font["t-body"].line
viewport:SetSize(215, line * 6 + 4)
function viewport:GetVerticalScrollRange() return math.max(0, edit:GetHeight() - self:GetHeight()) end
function edit:GetNumLines() return select(2, self:GetText():gsub("\n","")) + 1 end
local long = string.rep("Куда дальше?\n", 10)
edit:SetText(long)
edit:Fire("OnCursorChanged", 0, -10 * line, 1, line)
composer:Fire("OnUpdate")
assert(edit:GetWidth() == 215 and edit:GetHeight() >= 11 * line)
assert(field:GetHeight() < edit:GetHeight() and viewport:GetVerticalScroll() > 0)
edit:Fire("OnCursorChanged", 0, 0, 1, line)
composer:Fire("OnUpdate")
assert(viewport:GetVerticalScroll() == 0, "first line must scroll back into view")
viewport:SetWidth(440)
viewport:Fire("OnSizeChanged")
edit:SetText("")
composer:Fire("OnUpdate")
assert(edit:GetWidth() == 440 and edit:GetHeight() >= line and viewport:GetVerticalScroll() == 0)
local visible = false
for _, widget in ipairs(wow.texts) do
    if widget:GetText() == "Write here" and widget:IsVisible() and widget:GetWidth() > 2 then visible = true end
end
assert(visible and field:GetHeight() > line, "empty composer retains a visible placeholder and a full text line")
local function prompt()
    for _, widget in ipairs(wow.texts) do
        local owner = widget
        while owner and owner ~= composer do owner = owner:GetParent() end
        if owner and widget:GetText() == "Write here" then return widget:IsVisible() end
    end
end
edit:Fire("OnEditFocusGained")
assert(not prompt(), "the prompt makes way for the cursor while the field has focus")
set(composer, "first")
assert(not prompt(), "a state change keeps it hidden while focused")
edit:Fire("OnEditFocusLost")
set(composer, "third")
assert(edit:GetText() == "" and prompt(), "an empty field without focus shows the prompt again")

-- The client drew no cursor in this field, so the composer draws one where the edit box reports it.
local caret, height = composer.caret, composer:GetHeight()
edit:Fire("OnEditFocusGained")
assert(caret:IsShown(), "a caret shows as soon as the field has focus")
edit:Fire("OnCursorChanged", 40, -24, 2, 20)
local _, _, _, x, y = caret:GetPoint(1)
assert(x == 40 and y == -24 and caret:GetHeight() == 20 and caret:IsShown(), "it stands where the cursor is")
composer:Fire("OnUpdate", 0.6)
assert(not caret:IsShown(), "and blinks")
composer:Fire("OnUpdate", 0.6)
assert(caret:IsShown())
assert(composer:GetHeight() == height, "focus does not move the field: the link hint is always there")
edit:Fire("OnEditFocusLost")
assert(not caret:IsShown(), "no caret without focus")
local send = composer.send
local point, _, _, right, bottom = send:GetPoint(1)
assert(point == "BOTTOMRIGHT" and right == -bottom and send:GetHeight() == field:GetHeight() - 2 * bottom,
    "the send key sits with equal margins, centred beside a single line")
print("Composer: measured viewport, multiline cursor, empty field, per-character reload, offline and adopted drafts OK")
