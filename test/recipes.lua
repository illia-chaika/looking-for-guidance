local here=arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow=dofile(here.."/wow.lua")
local T=wow.Load(here.."/../looking-for-guidance")
local guid="Player-1-A1"
T.CharacterIdentity=function() return guid,"login" end
local own, lists, full, fire, bags = true,0,false,false,2
Professions={InLocalCraftingMode=function() return own end}
-- Synthetic IDs. Required fire, current materials and book rank are distinct.
local form=CreateFrame("Frame",nil,UIParent)
ProfessionsFrame=CreateFrame("Frame",nil,UIParent)
ProfessionsFrame.CraftingPage={SchematicForm=form}
form.GetRecipeInfo=function() return {recipeID=7001} end
C_TradeSkillUI={
    GetBaseProfessionInfo=function() return {professionID=185,professionName="Cooking",skillLevel=1,maxSkillLevel=75} end,
    IsTradeSkillReady=function() return true end, IsDataSourceChanging=function() return false end,
    IsRecipeInSkillLine=function(id,profession) return profession==185 and id~=999 end,
    GetFilteredRecipeIDs=function() lists=lists+1;return {7001,999} end,
    GetAllRecipeIDs=function() if full then return {7001,7002,999} end end,
    GetRecipeInfo=function(id)
        return {recipeID=id,name=id==7001 and "Charred Wolf Meat" or "Crispy Bat Wing",learned=id==7001}
    end,
    GetRecipeRequirements=function() return {{name="Cooking Fire",met=fire,type=0}} end,
    GetRecipeSchematic=function(id) return {recipeID=id,outputItemID=8001,reagentSlotSchematics={
        {required=true,quantityRequired=1,reagents={{itemID=8002}}}}} end
}
C_Item.GetItemCount=function(id) if id==8002 then return bags end end
local function capture() local c={};T.Recipes.Append(c);return c[1],c end
local function validate(book)
    assert(book and book.id==185 and book.rank==1 and book.maxRank==75)
    assert(book.recipes[1][1]==7001 and book.recipes[1][2]=="Charred Wolf Meat" and book.recipes[1][3]==1)
end
local b=capture();validate(b);assert(b.freshness=="open" and b.coverage=="filtered" and #b.recipes==1)
T.Store.runId="run-fixture"
T.GameDetails.Receive({id="exact-recipe",session=T.Transport.session,character=guid,login="login",
    refs={{kind="recipe",section="skills",id=7002}}})
local packet=wow.sent[#wow.sent]
assert(packet.cards[1].id==7002 and packet.cards[1].learned==false and packet.cards[1].status=="ready",
    "valid exact-ID lookup finds an explicit unlearned recipe hidden by the player's filter")
assert(packet.cards[1].reagentSlots[1].alternatives[1].bagCount==nil,
    "a live search must also respect the original question's bag permission")
T.GameDetails.Receive({id="other-login",session=T.Transport.session,character=guid,login="old",
    refs={{kind="recipe",section="skills",id=7002}}})
assert(wow.sent[#wow.sent].cards[1].status=="unavailable")
assert(b.selected.outputItemID==8001 and b.selected.recipeID==7001 and b.selected.requirements[1].met==false)
assert(b.selected.reagentSlots[1].alternatives[1].bagCount==2 and b.selected.reagentSlots[1].required)
local n=lists;fire=true;bags=1;b=capture()
assert(lists==n and b.selected.requirements[1].met and b.selected.reagentSlots[1].alternatives[1].bagCount==1)
T.settings.context.bags=false;b=capture();assert(b.selected.reagentSlots[1].alternatives[1].bagCount==nil)
ProfessionsFrame:Hide();b=capture();validate(b);assert(b.freshness=="last_seen" and b.selected==nil)
wow.Event("NEW_RECIPE_LEARNED",7002);wow.Run(.3);b=capture();assert(b.freshness=="stale" and not b.selected)
own=false;ProfessionsFrame:Show();wow.Event("TRADE_SKILL_SHOW");wow.Run(.3);b=capture();assert(b.freshness=="stale" and lists==n)
guid="Player-1-B2";assert(capture()==nil,"another player's books leaked")
own=true;full=true;wow.Event("TRADE_SKILL_LIST_UPDATE");wow.Run(.3)
b=capture();validate(b);assert(#b.recipes==2 and b.recipes[2][3]==0 and b.coverage=="reported_catalog")
T.settings.context.skills=false;assert(capture()==nil)
print("Own Cooking book, selected recipe requirements, filtered catalog, explicit unlearned flag, staleness and character isolation passed")
