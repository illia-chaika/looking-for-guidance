local _, T = ...
local Recipes = {}
T.Recipes = Recipes

-- Forever 1.60.1 uses Blizzard_Professions/Camelot. The selected form and the
-- catalog are separate sources: a learned recipe can still require a fire.
local maxRecipes, maxBooks = 160, 10
local dirty, reading = true, nil

local function read(fn, ...)
    if type(fn) ~= "function" then return nil end
    local ok, value = pcall(fn, ...)
    if ok and not (issecretvalue and issecretvalue(value)) then return value end
end

local function integer(v)
    if not (issecretvalue and issecretvalue(v)) and type(v) == "number" and v > 0 and v == math.floor(v) then return v end
end

local function enabled()
    return T.settings and T.settings.extras and T.settings.context and T.settings.context.skills
end

local function books(create)
    local guid = T.CharacterIdentity and T.CharacterIdentity()
    if not guid or not enabled() then return nil end
    if create and not T.settings.recipeBooks then T.settings.recipeBooks = {} end
    local cache = T.settings.recipeBooks
    if not cache then return nil end
    local locale = read(GetLocale) or ""
    if create and (not cache[guid] or cache[guid].locale ~= locale) then cache[guid] = {locale=locale, books={}} end
    local own = cache[guid]
    return own and own.locale == locale and own.books or nil
end

local function ownBook()
    local api, frame = C_TradeSkillUI, ProfessionsFrame
    if not api or not frame or not read(frame.IsVisible,frame) or read(api.IsTradeSkillReady) ~= true
        or read(api.IsDataSourceChanging) == true then return nil end
    -- The game's own predicate excludes NPC, linked, guild and guild-member views.
    if read(Professions and Professions.InLocalCraftingMode) ~= true or read(api.IsRuneforging) == true then return nil end
    local prof = read(api.GetBaseProfessionInfo)
    if not prof or not integer(prof.professionID) or not integer(prof.skillLevel)
        or type(prof.professionName) ~= "string" or prof.professionName == "" then return nil end
    return prof
end

local function selectedRecipe()
    local page = ProfessionsFrame and ProfessionsFrame.CraftingPage
    local form = page and page.SchematicForm
    if form and read(form.IsVisible,form) then
        local recipe = read(form.GetRecipeInfo,form)
        return recipe and integer(recipe.recipeID)
    end
end

local function ownRow(id, prof)
    local api = C_TradeSkillUI
    if not integer(id) or read(api.IsRecipeInSkillLine, id, prof.professionID) ~= true
        and read(api.IsRecipeInBaseSkillLine,id) ~= true then return nil end
    local r = read(api.GetRecipeInfo,id)
    if not r or r.recipeID ~= id or r.isDummyRecipe or type(r.name) ~= "string" or r.name == "" then return nil end
    local learned = type(r.learned) == "boolean" and (r.learned and 1 or 0) or -1
    return {id,r.name,learned}
end

local function collect(prof)
    local api, stored = C_TradeSkillUI, books(true)
    if not stored then return end
    local ids, coverage = read(api.GetAllRecipeIDs), "reported_catalog"
    if type(ids) ~= "table" then ids, coverage = read(api.GetFilteredRecipeIDs), "filtered" end
    if type(ids) ~= "table" then ids, coverage = {}, "selection_only" end
    local seen, recipes = {}, {}
    local function add(id)
        if not id or seen[id] then return end
        seen[id] = true
        local row = ownRow(id, prof)
        if row then recipes[#recipes+1] = row end
    end
    add(selectedRecipe())
    -- Reserve space for the selected recipe and the player's current list
    -- before filling the bounded packet with the larger unfiltered catalog.
    if coverage == "reported_catalog" then
        for _, id in ipairs(read(api.GetFilteredRecipeIDs) or {}) do
            if #recipes >= maxRecipes then break end
            add(id)
        end
    end
    for _, id in ipairs(ids) do
        if #recipes >= maxRecipes then coverage = "bounded"; break end
        add(id)
    end
    table.sort(recipes,function(a,b) return a[1] < b[1] end)
    local book = {kind="recipe_book",id=prof.professionID,name=prof.professionName,rank=prof.skillLevel,
        maxRank=prof.maxSkillLevel,observedAt=time(),coverage=coverage,recipes=recipes}
    stored[tostring(book.id)] = book
    local count, oldest, remove = 0, math.huge, nil
    for k, other in pairs(stored) do
        count = count + 1
        if other ~= book and other.observedAt < oldest then oldest,remove=other.observedAt,k end
    end
    if count > maxBooks and remove then stored[remove] = nil end
    reading, dirty = book, false
end

-- Fetch volatile requirements only for the actually displayed recipe and only
-- at question time. None of the counts or met flags enter SavedVariables.
local function details(id, prof, bagCounts)
    local row = ownRow(id, prof)
    if not row then return end
    local api = C_TradeSkillUI
    local out = {recipeID=row[1],name=row[2]}
    if row[3] >= 0 then out.learned = row[3] == 1 end
    if row[3] == 0 then
        local source = read(api.GetRecipeSourceText,id)
        if type(source) == "string" and source ~= "" and #source <= 1000 then out.sourceText = source end
    end
    local requirements = read(api.GetRecipeRequirements,id)
    if type(requirements) == "table" then
        out.requirements = {}
        for i = 1, math.min(16,#requirements) do
            local r = requirements[i]
            if type(r.name) == "string" and type(r.met) == "boolean" then
                out.requirements[#out.requirements+1] = {name=r.name, met=r.met, type=r.type}
            end
        end
    end
    local schematic = read(api.GetRecipeSchematic,id,false)
    if schematic and schematic.recipeID == id then
        out.outputItemID = integer(schematic.outputItemID)
        local slots = schematic.reagentSlotSchematics
        if type(slots)=="table" then
            out.reagentSlots = {}
            for i = 1,math.min(12,#slots) do
                local slot = slots[i]
                local result = {required=slot.required,quantityRequired=slot.quantityRequired, alternatives={}}
                for j, reagent in ipairs(slot.reagents or {}) do
                    if j > 6 then break end
                    local itemID = integer(reagent.itemID)
                    if itemID then
                        local alt = {itemID=itemID}
                        if bagCounts and T.settings.context.bags then
                            alt.bagCount = read(C_Item and C_Item.GetItemCount or GetItemCount,itemID,false,false,false,false)
                        end
                        result.alternatives[#result.alternatives+1]=alt
                    end
                end
                if #result.alternatives>0 then out.reagentSlots[#out.reagentSlots+1]=result end
            end
        end
    end
    return out
end

-- A Forever client can hide unlearned recipes from its list. An exact lookup
-- uses the native info API without changing that list or opening a window.
function Recipes.Probe(id, character, login, bagCounts)
    local card = {kind="recipe", id=id, status="unavailable"}
    local guid, currentLogin = T.CharacterIdentity()
    if not enabled() or not guid or character ~= guid or login ~= currentLogin then
        card.reason="Skills unavailable for this character/session."
        return card
    end
    local prof = ownBook()
    if not prof then card.reason="Open your own profession book to check this recipe."; return card end
    local out = details(id,prof,bagCounts)
    if not out or type(out.learned) ~= "boolean" then
        card.reason="The client did not report this recipe's learned flag for this profession."
        return card
    end
    out.kind, out.id, out.status = "recipe", id, "ready"
    out.professionID, out.professionName = prof.professionID, prof.professionName
    out.rank, out.maxRank = prof.skillLevel, prof.maxSkillLevel
    return out
end

function Recipes.Append(cards)
    if not enabled() then return end
    local prof = ownBook()
    local stored = books() or {}
    if prof and (dirty or not reading or stored[tostring(prof.professionID)] ~= reading) then collect(prof) end
    local ordered = {}
    for _, book in pairs(books() or {}) do ordered[#ordered+1]=book end
    table.sort(ordered,function(a,b) return a.id<b.id end)
    for _, cached in ipairs(ordered) do
        local card = {}
        for k,v in pairs(cached) do card[k]=v end
        if prof and cached==reading and not dirty then
            card.freshness="open"
            local selected = selectedRecipe()
            if selected then card.selected=details(selected,prof,T.settings.context.bags) end
        else
            card.freshness=cached.invalidated and "stale" or "last_seen"
        end
        cards[#cards+1]=card
    end
end

local events = CreateFrame("Frame")
local scheduled = false
events:SetScript("OnEvent", function(_, event)
    dirty, reading = true, nil
    if not enabled() then return end
    if event=="NEW_RECIPE_LEARNED" or event=="SKILL_LINES_CHANGED" or event=="SPELLS_CHANGED" then
        for _, book in pairs(books() or {}) do book.invalidated=true end
    end
    if event=="TRADE_SKILL_CLOSE" or event=="PLAYER_ENTERING_WORLD" then return end
    if not scheduled then
        scheduled=true
        C_Timer.After(.2,function()
            scheduled=false
            local prof=enabled() and ownBook()
            if prof and dirty then collect(prof) end
        end)
    end
end)
for _, event in ipairs({"PLAYER_ENTERING_WORLD","TRADE_SKILL_SHOW","TRADE_SKILL_LIST_UPDATE","TRADE_SKILL_DETAILS_UPDATE",
    "TRADE_SKILL_DATA_SOURCE_CHANGED","TRADE_SKILL_CLOSE","NEW_RECIPE_LEARNED","SKILL_LINES_CHANGED","SPELLS_CHANGED"}) do
    pcall(events.RegisterEvent,events,event)
end
