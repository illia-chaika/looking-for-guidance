local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
wow.payload = wow.Samples(here .. "/../samples/samples.json").payloads.ready
local T = wow.Load(here .. "/../looking-for-guidance")
local page = T.HubPage.Create(UIParent)
page:Show()
local tiles = {
    {icon="Route",label="Route",summary="Quests along the way",click=function() end},
    {icon="Bags",label="Bags",summary="Space and useful items",click=function() end},
    {icon="Dungeon",label="Dungeons",summary="Entrance and preparation",click=function() end},
}
local touched
local function draw(width, long)
    page:SetWidth(width)
    page:Begin()
    page:Lead({kicker="Quest log",title="Go to the Undercity",reason="Two quests share a destination."},
        "Route",{{label=long and string.rep("Route ",8) or "View route",primary=true,click=function() touched = "route" end},
          {label="To current chat",click=function() touched = "chat" end},
          {label="Later",trailing=true,click=function() touched = "later" end}},
        "The tracked quest changed.")
    page:Grid(tiles)
    page:Finish()
end
local function find(label)
    for _, b in ipairs(wow.frames) do
        if b:IsVisible() then
            local s = b.label or b.title
            if s and s:GetText() == label then return b end
        end
    end
    error("Missing fieldbook row: " .. label)
end
draw(520)
local first, second, third = find("Route"),find("Bags"),find("Dungeons")
local function coords(widget)
    local _,_,_,x,y = widget:GetPoint()
    return x,y
end
local x1,y1=coords(first)
local x2,y2=coords(second)
local x3,y3=coords(third)
assert(x2 > x1 and y1 == y2 and x3 == x1 and y3 < y1, "two-column overview grid with aligned rows")
local inspect,chat,later=find("View route"),find("To current chat"),find("Later")
local function press(b,key) b:Fire("OnClick"); assert(touched==key) end
press(inspect,"route");press(chat,"chat");press(later,"later")
local cx,cy=coords(chat);local lx,ly=coords(later)
assert(cx>0 and cy==ly and lx>cx and lx+later:GetWidth() < page:GetWidth(), "inline primary and secondary actions")
local frames=#wow.frames
draw(220,true)
x1,y1=coords(first);x2,y2=coords(second)
assert(x1==x2 and y2<y1, "narrow tiles stack")
cx,cy=coords(chat);local _,py=coords(inspect)
assert(cy<py, "long translated action captions wrap once, without overlap")
assert(#wow.frames==frames,"resize reuses native controls")
print("Native fieldbook card, responsive task grid, localized action wrapping and pooling passed")
