-- Exercise stored and newly displayed UTF-8 through both transcript renderers.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local snapshot = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local thread = snapshot.threads[1]
local user = "Это оно 🙂? 日本語 é"
local answer = "Undercity с его кругами и этажами поначалу легко путает 🙂"
thread.turns = {{role = "user", text = user}, {role = "assistant", text = answer}}
wow.payload = snapshot
local T = wow.Load(here .. "/../looking-for-guidance")
T.Window.Show()
T.Window.Select(thread.id)
local function visible(text)
    for _, region in ipairs(wow.texts) do
        if region:IsVisible() and region:GetText() == text then return true end
    end
end
local function expectedIcon(key, size)
    local atlas = T.EmojiAtlas
    local tile = assert(T.EmojiIndex[key], "missing atlas key: " .. key)
    local page = math.floor(tile / (atlas.rows * atlas.columns)) + atlas.firstPage
    local x, y = (tile % atlas.columns)*atlas.cell, (math.floor(tile/atlas.columns) % atlas.rows)*atlas.cell
    return string.format("|T%s:%d:%d:0:0:%d:%d:%d:%d:%d:%d|t",
        T.media .. atlas.prefix .. string.format(atlas.pageFormat,page) .. atlas.suffix,
        size,size,atlas.size,atlas.size,x+atlas.gutter,x+atlas.cell-atlas.gutter,y+atlas.gutter,y+atlas.cell-atlas.gutter)
end
local size = T.Tokens.font["t-body"].size
local face = expectedIcon("🙂",size)
assert(visible("Это оно " .. face .. "? 日本語 é"), "stored user's smile is a native inline texture")
assert(visible("Undercity с его кругами и этажами поначалу легко путает " .. face), "stored advisor's smile")
assert(T.Store.Turns(T.Store.Find(thread.id))[1].text == user)
assert(T.Store.Turns(T.Store.Find(thread.id))[2].text == answer)
local render = T.DisplayEmoji
assert(render("日本語 한국어 𠀋 ㊗ café © ™ 1#*") == "日本語 한국어 𠀋 ㊗ café © ™ 1#*")
assert(render("™︎") == "™" and render("™️") == expectedIcon("™",17))
assert(render("↔︎♥︎",17,true) == "↔♥" and render("↔︎",17,false) == "")
assert(render("↔️",17,true) == expectedIcon("↔",17))
for _, entry in ipairs({
    {"🇺🇦","🇺🇦"}, {"#️⃣","#⃣"}, {"1⃣","1⃣"}, {"*️⃣","*⃣"}, {"👍🏽","👍🏽"},
    {"👩🏽‍💻","👩🏽‍💻"}, {"👨‍👩‍👧‍👦","👨‍👩‍👧‍👦"}, {"❤️‍🔥","❤‍🔥"}, {"✅","✅"},
}) do
    assert(render(entry[1],23) == expectedIcon(entry[2],23), "one texture at current reading size " .. entry[1])
end
assert(T.EmojiIndex["#⃣"] == 0, "tile zero is a valid image")
local subdivision = utf8.char(0x1F3F4, 0xE0067, 0xE0062, 0xE0073, 0xE0063, 0xE0074, 0xE007F)
assert(render(subdivision) == expectedIcon(subdivision,17))
local link = "|cffccddff|Hplayer:🙂||h♥|h[🙂]|h|r"
assert(render(link) == "|cffccddff|Hplayer:🙂||h♥|h[" .. face .. "]|h|r")
assert(render("||Hplain🙂||h") == "||Hplain" .. face .. "||h")
assert(render("|TFoo🙂:12|t❤") == "|TFoo🙂:12|t" .. expectedIcon("❤",17))
assert(render("|Aatlas🙂:12|a🙂") == "|Aatlas🙂:12|a" .. face)
assert(render("||TFake🙂:16||t") == "||TFake" .. face .. ":16||t", "escaped pipes stay literal")
assert(render("https://example.org/🙂?q=🇵🇱 🙂") == "https://example.org/🙂?q=🇵🇱 " .. face)
local missing = utf8.char(0x1faff)
assert(T.EmojiIndex[missing] == nil)
assert(render("Слева " .. missing .. " справа") == "Слева справа")
assert(render("Порядок " .. missing .. "!") == "Порядок!")
assert(render(missing .. " Текст " .. missing) == "Текст")
assert(render("🙂‍" .. missing) == "", "drop the complete unsupported ZWJ sequence")
assert(render("🇿🇿")=="" and render("🇿")==expectedIcon("🇿",17), "unknown flag and supported single indicator")
assert(render(face) == face, "formatting is idempotent")
for glyph in pairs(T.EmojiIndex) do
    local qualified = utf8.len(glyph) == 1 and glyph .. utf8.char(0xfe0f) or glyph
    assert(render(qualified, 17) == expectedIcon(glyph, 17), "atlas entry does not render as one icon: " .. glyph)
end
-- Arrows, check marks and shapes are not in the PT faces and are not emoji: the game would draw a box.
assert(render("Вложи очко в Improved Corruption → 1/5") == "Вложи очко в Improved Corruption › 1/5", "an arrow becomes the guillemet the face has")
assert(render("A ⇒ B ← C") == "A › B ‹ C")
assert(render("Урон ↑, мана ↓", 17) == "Урон " .. expectedIcon("⬆", 17) .. ", мана " .. expectedIcon("⬇", 17))
assert(render("✓ сделано, ✗ нет, ★ важно", 13) == expectedIcon("✔", 13) .. " сделано, " .. expectedIcon("✖", 13) .. " нет, " .. expectedIcon("⭐", 13) .. " важно")
assert(render("■ □ ● ►", 17) == expectedIcon("▪", 17) .. " " .. expectedIcon("▫", 17) .. " " .. expectedIcon("⚫", 17) .. " " .. expectedIcon("▶", 17))
assert(render("→︎ и ✓️", 17) == "› и " .. expectedIcon("✔", 17), "a trailing presentation selector goes with the symbol")
assert(render("日本語 → 한국어", 17, true) == "日本語 → 한국어", "the CJK face draws these symbols itself")
assert(render("|Hitem:1|h[A → B]|h → C") == "|Hitem:1|h[A › B]|h › C", "link targets are untouched, captions are text")
assert(render("https://example.org/a→b → c") == "https://example.org/a→b › c", "addresses stay byte for byte")
assert(render("• — – … « » № ≈ ≥ × °") == "• — – … « » № ≈ ≥ × °", "symbols the face has are left alone")
local shown = render("**Improved Corruption → 1/5**")
assert(render(shown) == shown, "the replacement is stable when applied twice")
print("Emoji: native color tags in stored user and advisor messages, known clusters, font sizing, omitted unsupported clusters, stand-ins for arrows and marks the faces lack, literal links and URLs OK")
