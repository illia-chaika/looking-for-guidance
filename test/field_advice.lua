-- Replay game API changes through the same collector that powers the journal.
local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
local hub = T.HubOverview
local guid = "Player-1-121A"
T.CharacterIdentity = function() return guid, "login" end
T.settings.locale = "ruRU"
local function read(quest, dungeon)
    return hub.Cards(hub.Model({}), quest, dungeon)
end

local log = {
    {questID=71,title="Silverpine delivery",level=8},
    {questID=72,title="Carry the orders",level=8},
    {questID=73,title="Speak to Halgar",level=10},
}
local objectives={"Carry the package to Silverpine Forest.","Present the orders in Undercity.","Speak to Halgar in Undercity."}
local watches={[72]=true}
C_QuestLog = {
    GetNumQuestLogEntries=function() return #log end,
    GetInfo=function(i) return log[i] end,
    GetSelectedQuest=function() return 0 end,
    GetQuestWatchType=function(id) return watches[id] and 0 or nil end,
    GetQuestObjectives=function() return {} end,
    IsComplete=function(id) return id==71 end,
}
function GetQuestLogQuestText(index) return nil,objectives[index] end
C_Map = {
    GetBestMapForUnit=function() return 3 end,
    GetMapInfo=function(id)
        return id==3 and {mapID=3,parentMapID=2,mapType=3,name="Tirisfal"} or {mapID=2,mapType=2,name="Eastern Kingdoms"}
    end,
    GetMapChildrenInfo=function() return {{name="Silverpine Forest"},{name="Undercity"}} end,
}
local bagFree=5
local trashNames={"Broken Fang","Stolen Seal","Spare Tooth"}
C_Container = {
    GetContainerNumSlots=function(b) return b==0 and 3 or 0 end,
    GetContainerNumFreeSlots=function(b) return b==0 and bagFree or 0,0 end,
    GetContainerItemInfo=function(_,i) return {
        hyperlink="|cffa1a1a1|Hitem:"..i.."|h["..trashNames[i].."]|h|r",stackCount=1,quality=0,hasNoValue=false
    } end,
    GetContainerItemQuestInfo=function(_,i) return {isQuestItem=i==2,questID=i==2 and 72 or nil} end,
}
T.Store.memory={character=guid,login="login",notes={{scope="character",topic="bags",text="Keep my Spare Tooth."}}}
local cards=read()
assert(not cards.route.nearby and cards.route.quest.id==72, "tracked nearby goal wins over an arbitrary ready delivery")
hub.Observe(cards);assert(hub.UnreadCount()==0)
watches[73]=true
cards=read()
assert(cards.route.nearby and cards.route.title:find("Undercity",1,true))
assert(cards.route.quest.coordinates==nil,"textual region is not a map pin")
hub.Observe(cards);assert(hub.TopUnread(cards)==cards.route)
hub.Snooze(cards.route);hub.Observe(read());assert(hub.UnreadCount()==0)
bagFree=0;cards=read();hub.Observe(cards)
assert(hub.Lead(cards)==cards.bags and hub.UnreadCount()==1)
assert(cards.bags.title:find("+1",1,true),"quest item and remembered item are excluded from vendor slots")
assert(cards.bags.reason=="Broken Fang")
assert(#cards.bags.steps==2 and cards.bags.steps[2].title=="Stolen Seal")
hub.Read(cards.bags);hub.Observe(read());assert(hub.UnreadCount()==0)
assert(cards.stuck.quest==nil and #cards.stuck.choices==3,"ambiguous tracked quests require selection")
assert(read(73).stuck.quest.id==73)
T.settings.context.location=false
assert(not read().route.nearby,"disabled location excludes place matching")
T.settings.context.location=true

local services={{"Demon Skin",300},{"Immolate",300}}
local trainer,kind,id="Rupert",0,21
function GetLocale() return "enUS" end
function UnitName(unit) return unit=="npc" and trainer or nil end
function UnitGUID(unit) return unit=="npc" and "Creature-0-1-0-1-"..id.."-0" or guid end
function GetNumTrainerServices() return #services end
function GetTrainerServiceInfo(i) return services[i][1],"available",1,10,"Rank 2" end
function GetTrainerServiceCost(i) return services[i][2] end
function GetMoney() return 450 end
function GetActionInfo(slot) if slot==1 then return "spell",10 end end
C_Spell={GetSpellName=function() return "Immolate" end}
C_Trainer={GetTrainerType=function() return kind end}
local page=hub.Create(UIParent,{memory=function() end,discuss=function() end})
page:Fire("OnEvent","TRAINER_SHOW")
cards=read()
assert(cards.trainer.title:find("Immolate",1,true) and cards.trainer.reason:find("1s 50c",1,true))
page:Fire("OnEvent","TRAINER_CLOSED")
assert(read().trainer.priority==0,"historical prices do not create a live offer")
trainer,kind,id="Tailoring",2,22
services={{"Bolt of Cloth",400}}
page:Fire("OnEvent","TRAINER_SHOW")
assert(read().trainer.title:find("Bolt of Cloth",1,true))
page:Fire("OnEvent","TRAINER_CLOSED")
assert(read().trainer.title:find("Immolate",1,true),"craft trainer did not erase class trainer's visit")

function UnitFactionGroup() return "Horde" end
function UnitLevel() return 10 end
function GetNumGroupMembers() return 2 end
function GetRealZoneText(instance) return instance and "Ragefire Chasm" or "Brill" end
log[#log+1]={title="Other dungeon",isHeader=true}
log[#log+1]={questID=74,title="Unrelated boss",level=10}
T.Store.memory.notes[2]={topic="dungeon",scope="day",text="Run RFC today."}
cards=read()
assert(cards.dungeon.dungeon=="Ragefire Chasm" and #cards.dungeon.matches==0 and cards.dungeon.signal)
assert(#cards.dungeon.steps==1 and cards.dungeon.steps[1].title==T.L("Согласуйте место встречи"),"nothing about a dungeon is hard-coded: its quests come from research")
print("ok native journal advice scenarios")
