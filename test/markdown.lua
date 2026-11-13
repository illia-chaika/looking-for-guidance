-- What the advisers write by default, a table, a heading, a numbered list, is drawn and not printed as markup.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")
wow.payload = samples.payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
T.Insights.Request = function() return false end
T.settings.appearance = "journal"

local passed = 0
local function check(name, ok)
    assert(ok, name)
    passed = passed + 1
end

local bags = table.concat({
    "У Jonathan Chambers три сумки.",
    "",
    "| Сумка | Ячеек | Цена |",
    "|---|:---:|---:|",
    "| **Brown Leather Satchel** | 8 | 25 серебра |",
    "| Heavy Brown Bag | 10 | [2 золота](https://example.org/bags) |",
    "| Huge Brown Sack | 12 |",
    "",
    "Бери первую.",
}, "\n")

-- Parsing.
local sources = {}
local blocks = T.Format.Blocks(bags, "ffffffff", sources)
local tab = blocks[2]
check("a header row, a ruling and rows make one table between two paragraphs",
    #blocks == 3 and blocks[1].kind == "paragraph" and tab.kind == "table" and blocks[3].kind == "paragraph")
check("the table keeps its header, its rows and the alignment of each column",
    #tab.head == 3 and tab.head[1] == "Сумка" and #tab.rows == 3
    and tab.align[1] == "LEFT" and tab.align[2] == "CENTER" and tab.align[3] == "RIGHT")
check("cells keep emphasis and links", tab.rows[1][1] == "|cffffffffBrown Leather Satchel|r"
    and tab.rows[2][3]:find("|Haddon:looking%-for%-guidance:source:1|h%[2 золота%]") ~= nil and sources[1].host == "example.org")
check("a short row is filled with empty cells", #tab.rows[3] == 3 and tab.rows[3][3] == "")

local loose = T.Format.Blocks("Выбор: меч | щит\nБез линейки это просто строка.", "ffffffff")
check("a line with a bar and no ruling under it stays a paragraph", #loose == 2 and loose[1].kind == "paragraph" and loose[1].text:find("||", 1, true) ~= nil)
local uneven = T.Format.Blocks("| a | b |\n|---|\n| 1 | 2 |", "ffffffff")
check("a ruling that does not match the header is not a table", uneven[1].kind == "paragraph")
local fenced = T.Format.Blocks("```\n| a | b |\n|---|---|\n```", "ffffffff")
check("a table inside a code block stays code", #fenced == 2 and fenced[1].kind == "code" and fenced[2].kind == "code")

local outline = T.Format.Blocks("# Итог\n## Путь A: копить ##\n1. **Сначала** сдай задание\n2) Потом волки\n- просто пункт\n#хештег", "ffffffff")
check("headings lose their marks", outline[1].kind == "heading" and outline[1].text == "Итог" and outline[2].kind == "heading" and outline[2].text == "Путь A: копить")
check("numbered steps keep their number apart from the text", outline[3].kind == "item" and outline[3].number == "1."
    and outline[3].text == "|cffffffffСначала|r сдай задание" and outline[4].number == "2." and outline[4].text == "Потом волки")
check("a plain item has no number, and a word after # is not a heading", outline[5].kind == "item" and outline[5].number == nil and outline[6].kind == "paragraph")

check("a table too wide for the window reads as one line per row", (function()
    local rows = T.Format.TableRows(tab)
    return #rows == 3 and rows[1] == "|cffffffffBrown Leather Satchel|r · Ячеек: 8 · Цена: 25 серебра"
        and rows[3] == "|cffffffffHuge Brown Sack|r · Ячеек: 12"
end)())

-- Drawing.
local probe, story
probe = T.Transcript.Build(CreateFrame("Frame"), { refresh = function() probe:Render(story, wow.now) end })
local function draw(width, text)
    probe.child:SetWidth(width)
    story = { id = "markdown", state = "idle", turns = { { role = "user", text = "Что купить?" }, { role = "assistant", text = text } } }
    probe:Render(story, wow.now)
end
local function shown(value)
    for _, region in ipairs(wow.texts) do
        if region._text == value and region:IsVisible() then return region end
    end
end
local function markup()
    for _, region in ipairs(wow.texts) do
        if region:IsVisible() and (region._text:find("||", 1, true) or region._text:find("---", 1, true) or region._text:find("^#")) then return region._text end
    end
end
local function at(region) return region._points[1].x, -region._points[1].y end
local function strong(text) return "|c" .. T.Kit.Hex("strong") .. text .. "|r" end

draw(734, bags)
local name, slots, price = shown(strong("Brown Leather Satchel")), shown("8"), shown("25 серебра")
check("each cell of a table that fits is a text of its own", name and slots and price and shown("Сумка") and shown("Huge Brown Sack"))
check("nothing of the table's markup is printed", markup() == nil)
do
    local x1, y1 = at(name)
    local x2, y2 = at(slots)
    local x3, y3 = at(price)
    local _, headY = at(shown("Сумка"))
    local _, nextY = at(shown("Heavy Brown Bag"))
    check("cells of a row share a line and columns go left to right", y1 == y2 and y2 == y3 and x1 < x2 and x2 < x3)
    check("rows follow the header downwards", headY < y1 and y1 < nextY)
    check("the table stays inside the answer's width", x3 + price._width <= 734 - 2 * T.Kit.space["space-4"])
end
check("the paragraphs around the table are still there", shown("У Jonathan Chambers три сумки.") and shown("Бери первую."))

local wide = table.concat({
    "| Сумка | Ячеек | Цена | Цена за ячейку | Доступна при 67 серебра 22 меди |",
    "|---|---|---|---|---|",
    "| Brown Leather Satchel | 8 | 25 серебра | около 3 серебра | да |",
    "| Heavy Brown Bag | 10 | 2 золота | 20 серебра | нет, не хватает 1 золота 32 серебра 78 меди |",
}, "\n")
draw(734, "Сравнение.\n" .. wide)
do
    local cell = shown("нет, не хватает 1 золота 32 серебра 78 меди")
    local first = shown("Brown Leather Satchel")
    local x = at(cell)
    check("columns that ask for more than there is give way and wrap, none thinner than a readable one",
        cell and first and cell._width < #cell._text * 6 and cell._width >= 72 and first._width >= 72
        and x + cell._width <= 734 - 2 * T.Kit.space["space-4"])
end

draw(355, "Сравнение.\n" .. wide)
check("in a narrow window the same table is a list, one row per item",
    shown(strong("Brown Leather Satchel") .. " · Ячеек: 8 · Цена: 25 серебра · Цена за ячейку: около 3 серебра · Доступна при 67 серебра 22 меди: да")
    and not shown("Brown Leather Satchel") and not shown("8") and markup() == nil)

draw(734, "# Итог\nКопи на маунта.\n## Порядок\n1. Сдай задание\n2. Убей волков")
local step = shown("Сдай задание")
check("a heading is shown without its marks, in the heading's style", shown("Итог").companionStyle == "t-title" and shown("Порядок").companionStyle == "t-title" and markup() == nil)
check("a numbered step shows its number beside the text", step and shown("1.") and shown("2.") and (at(step)) > (at(shown("1."))))
check("the grid drawn before is gone", not shown("Brown Leather Satchel") and not shown("Сумка"))

print("markdown checks passed: " .. passed)
