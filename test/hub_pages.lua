local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
wow.payload.runId = "test-run"
local T = wow.Load(here .. "/../looking-for-guidance")
local guid, login = "Player-1-123ABC", "journal-login"
T.CharacterIdentity = function() return guid, login end
T.settings.locale = "enUS"
local hero, second = {questID=41, title="Courier's Orders", level=8}, {questID=42, title="Meet the Enchanter", level=10}
C_QuestLog = {
    GetNumQuestLogEntries=function() return 2 end,
    GetInfo=function(i) return ({hero, second})[i] end,
    GetSelectedQuest=function() return 42 end, IsComplete=function(id) return id == 41 end,
    GetQuestWatchType=function() return 1 end,
    GetNextWaypoint=function(id) return 601, id == 41 and .51 or .53, .62 end,
    GetQuestObjectives=function() return {{text="Speak to the quartermaster."}} end,
}
C_Map = {GetMapInfo=function() return {name="The Citadel"} end}
local linen = "|cffaaaaaa|Hitem:4567|h[Cloth]|h|r"
local packed = false
C_Container = {GetContainerNumSlots=function(bag) return bag == 0 and (packed and 1 or 2) or 0 end,
    GetContainerNumFreeSlots=function(bag) return (bag == 0 and packed) and 1 or 0, 0 end,
    GetContainerItemInfo=function(bag,slot) return {hyperlink=linen, stackCount=packed and 10 or (slot==1 and 3 or 7), quality=1} end}
C_Item = {GetItemInfo=function() return "Cloth", linen, 1, 5, 1, "Trade Goods", "Cloth", 20 end}
function UnitLevel() return 10 end
function GetNumGroupMembers() return 2 end
function GetLFDChoiceOrder() return {-1, 4, 5} end
function GetLFGDungeonInfo(id) return id==4 and "The Cellars" or "High Dungeon", 1, 1, id==4 and 10 or 40, 60, 13, 10, 18, 0, 1, "", 1, 5 end
local function visible(target)
    for _, object in ipairs(wow.texts) do
        if object:IsVisible() and object:GetText() == target then return true end
    end
end
local function click(label)
    for _, object in ipairs(wow.frames) do
        if object:IsVisible() and object:IsEnabled() and object:GetScript("OnClick") then
            if (object.label and object.label:GetText()==label) or (object.title and object.title:GetText()==label) then
                object:Fire("OnClick")
                return
            end
        end
    end
    error("missing control: " .. label)
end
T.Store.memory = {character=guid, login=login, notes={
    {id="party", text="Keep tonight for the Cellars.", topic="dungeon", revision=2, scope="session", source={quote="I want a Cellars run tonight."}},
    {id="bags", text="Keep wool for my guild.", topic="bags",revision=3,scope="character",source={quote="My guild needs wool."}}}}
local discussed, links
local overview = T.HubOverview.Create(UIParent,{discuss=function(text,refs) discussed,links=text,refs end, memory=function() end})
overview:Show()
assert(visible(T.L("Освободи ячейку на месте")),
    "a full inventory can take priority over a route without the player asking")
click(T.L("Маршрут"))
assert(visible(T.L("Один поход: {1}", "The Citadel")))
click(T.L("Показать подробности"))
assert(visible(hero.title) and visible(second.title), "route connects both actual quest objectives")
click(T.L("Источники"))
assert(visible("The Citadel · 53.0, 62.0"))
click(T.L("Разобрать в текущем чате"))
local joined = table.concat(links, " ")
assert(#links == 2 and joined:find("quest:41",1,true) and joined:find("quest:42",1,true))
click(T.L("К обзору"));click(T.L("Подземелья"))
assert(visible("The Cellars") and not visible("High Dungeon"))
assert(visible(T.L("Мой план:") .. " Keep tonight for the Cellars."))
click(T.L("К обзору"));click(T.L("Сумки"))
assert(visible(T.L("Объедини две стопки: {1} ({2} + {3}).","Cloth",3,7)))
click(T.L("Разобрать в текущем чате"))
assert(links[1]==linen and discussed:find("Keep wool for my guild.",1,true))
packed=true
overview:Invalidate("BAG_UPDATE_DELAYED");wow.Run(.3)
assert(not visible("Combine two stacks of Cloth (3 + 7)."))
click(T.L("Источники"));assert(visible("Free bag slots: 1"))
T.settings.context.bags=false;overview:Refresh()
assert(not visible("Free bag slots: 1"))
overview:Hide()
-- Going from space available to full is useful even while the book is closed.
-- Nothing is sent to Codex until the player explicitly starts the discussion.
T.settings.context.bags=true
overview:Invalidate("BAG_UPDATE_DELAYED");wow.Run(.3)
local beforeEvent = #wow.sent
packed=false
overview:Invalidate("BAG_UPDATE_DELAYED");wow.Run(.3)
assert(not overview:IsVisible() and T.HubOverview.UnreadCount()==1)
for i=beforeEvent+1,#wow.sent do assert(wow.sent[i].type~="create" and wow.sent[i].type~="reply") end
overview:Show();overview:OpenAdvice()
assert(visible(T.L("Освободи ячейку на месте")))
click(T.L("Позже"));assert(T.HubOverview.UnreadCount()==0 and not visible(T.L("Освободи ячейку на месте")))
overview:Hide()
local memory=T.HubMemory.Create(UIParent);memory:Show()
assert(visible("Keep wool for my guild."));click(T.L("Источник"));assert(visible("«My guild needs wool.»"))
click(T.L("Изменить"))
local edit
for _,w in ipairs(wow.frames) do if w:IsObjectType("EditBox") and w:IsVisible() then edit=w end end
assert(edit and edit:GetText()=="Keep wool for my guild.")
edit:SetText("Keep 20 wool.");click(T.L("Сохранить"))
local packet = wow.sent[#wow.sent]
assert(packet.type=="memory" and packet.memoryAction=="edit" and packet.character==guid and packet.login==login)
assert(packet.revision==3 and packet.noteId=="bags" and packet.noteText=="Keep 20 wool.")
T.Store.memory.requestSeq=packet.seq;T.Store.memory.session=T.Transport.session
T.Store.memory.notes[2].text="Keep 20 wool.";T.Store.memory.notes[2].revision=4
memory:Refresh();assert(not edit:IsVisible() and visible("Keep 20 wool."))
-- A passive snapshot may arrive after the ACK. A recovered edit is accepted
-- only if its revision, text and lifetime all match this mutation.
click(T.L("Изменить"));edit:SetText("Save 30 wool.");click(T.L("Сохранить"))
local secondEdit = wow.sent[#wow.sent]
T.Store.memory.requestSeq = nil
T.Store.memory.notes[2].revision,T.Store.memory.notes[2].text = 5,"Save 30 wool."
local delivery = T.Store.Delivery
T.Store.Delivery = function(seq) if seq == secondEdit.seq then return "delivered" end return delivery(seq) end
memory:Refresh();assert(not edit:IsVisible() and visible("Save 30 wool."))
T.Store.Delivery = delivery
click(T.L("Изменить"));click(T.L("Удалить из памяти"))
assert(visible(T.L("Удалить эту запись?")))
click(T.L("Удалить"))
packet=wow.sent[#wow.sent];assert(packet.memoryAction=="forget" and packet.revision==5)
T.Store.memory.character="Player-1-DIFFERENT";memory:Refresh()
assert(not visible("Save 30 wool.") and not visible("Keep tonight for the Cellars."))
print("Live overview, event updates, topical notes and scoped memory edits passed")
