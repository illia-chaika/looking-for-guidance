local base = (arg[0]:match('^(.*)/') or '.') .. '/../looking-for-guidance/'
local T={}; assert(loadfile(base..'Format.lua'))('test',T); assert(loadfile(base..'GameLinks.lua'))('test',T)
local function box() local b={visible=true,focus=true,text=''};function b:IsVisible() return self.visible end;function b:HasFocus() return self.focus end;function b:Insert(s) self.text=self.text..s end;return b end
local edit, mod, original = box(), true, 0
function IsModifiedClick(key) return key=='CHATLINK' and mod end
ChatFrameUtil={InsertLink=function(_,x) original=original+1;return x,'fallback' end}
T.GameLinks.Bind(edit)
local a='|cff1eff00|Hitem:35:2:0:-15|h[Ветхий посох]|h|r';local b='|cnINV_Flavor_Rare:|Hitem:35:7:0:-15|h[Ветхий посох]|h|r';local q='|cffffff00|Hquest:405:8|h[The Prodigal Lich]|h|r';local s='|Hspell:555|h[Новое умение]|h'
for _,v in ipairs({a,b,q,s}) do assert(ChatFrameUtil.InsertLink(v)==true) end
local text,refs=T.GameLinks.Prepare('Сравни '..edit.text);assert(not text:find('|',1,true) and text:find('[Ветхий посох](item:35)',1,true) and text:find('[The Prodigal Lich](quest:405)',1,true) and text:find('[Новое умение](spell:555)',1,true));assert(#refs==4 and refs[1].link~=refs[2].link and refs[2].link=='item:35:7:0:-15');assert(#select(2,T.GameLinks.Prepare(a..a))==2);assert(#select(2,T.GameLinks.Prepare('без вложений'))==0)
local variants = {'|Hachievement:8:Player-1:0:0|h[An achievement]|h','|Hplayer:Someone-Realm|h[Someone]|h','|Hcurrency:33|h[Currency]|h','|Htrade:Player-123-FFFF:2259:AA==|h[Alchemy]|h','|Hworldmap:123:4500:8000|h|A:Waypoint-MapPin-ChatIcon:0:0|a Pin|h'}
for i,link in ipairs(variants) do
 assert(ChatFrameUtil.InsertLink(link),'native link rejected')
 local msg,list=T.GameLinks.Prepare(link)
 assert(#list==1 and msg:find('wowref:1',1,true) and T.GameLinks.Display(msg,nil,list):find('|H'..list[1].link..'|h',1,true),'link lost')
end
assert(ChatFrameUtil.InsertLink('A plain chat insertion'))
local sent=edit.text
for _,gate in ipairs({'focus','visible','mod','unsupported','malformed'}) do
 edit.focus=gate~='focus';edit.visible=gate~='visible';mod=gate~='mod'
 local link=gate=='unsupported' and '|Hunclosed:123|hTest' or gate=='malformed' and '|Hitem:35evil|h[Test]|h' or a
 local n,tag=ChatFrameUtil.InsertLink(link,9001);assert(n==9001 and tag=='fallback' and edit.text==sent)
end
local rich=T.GameLinks.Display(text,'ffdedede',refs);assert(rich:find('|Hitem:35:2:0:-15|h',1,true) and rich:find('|Hitem:35:7:0:-15|h',1,true));assert(original==5);local display=T.GameLinks.Display('Проверь [Новое умение](spell:555) |Tbad|t');assert(display:find('|Hspell:555|h',1,true) and display:find('||Tbad||t',1,true))
print('Shift-click: focus, modifier, unchanged normal chat, independent item variants, Cyrillic, quest/spell, removal, safe rendering OK')

-- Exercise the actual window through offline, connected, and pending states.
local here = arg[0]:match('^(.*)/') or '.'
local wow = dofile(here .. '/wow.lua')
local frames = wow.Samples(here .. '/../samples/samples.json')
local keyboard, delegated = nil, 0
function GetCurrentKeyBoardFocus() return keyboard end
function IsModifiedClick(key) return key == 'CHATLINK' end
ChatFrameUtil = { InsertLink = function() delegated = delegated + 1; return false end }
wow.payload = frames.payloads.offline
local addon = wow.Load(base)
addon.Window.Show()
addon.Window.OpenPage("chat")
local composer, send
for _, frame in ipairs(wow.frames) do
    if frame.choice and frame.context and frame.edit then composer = frame end
    if frame.glyph and frame.glyph.name == 'send' then send = frame end
end
local editBox = assert(composer.edit)
function editBox:HasFocus() return keyboard == self end
function editBox:SetFocus() keyboard = self; self:Fire('OnEditFocusGained') end
function editBox:ClearFocus()
    if self:HasFocus() then keyboard = nil; self:Fire('OnEditFocusLost') end
end
function editBox:Insert(value) self:SetText(self:GetText() .. value) end
local function apply(payload) addon.Store.Apply(payload); addon.Window.Refresh() end
local function draftHas(link) return editBox:GetText():find(link, 1, true) ~= nil end
assert(editBox:IsVisible() and not composer.canSend)
editBox:SetFocus()
assert(ChatFrameUtil.InsertLink(a) and draftHas(a) and not send:IsEnabled())
editBox:Fire('OnEnterPressed')
assert(draftHas(a), 'sending before connection discarded the draft')
local ready = CopyTable(frames.payloads.ready)
ready.rev = wow.payload.rev + 1
local idle
for _,thread in ipairs(ready.threads) do
    if thread.state == 'idle' then idle = thread; break end
end
assert(idle, 'fixture has no idle chat')
ready.threads = {idle}
apply(ready)
addon.Window.Select(idle.id)
assert(composer.canSend and send:IsEnabled() and draftHas(a))

-- The unmodified mouse-down on Send clears focus before its click handler.
editBox:ClearFocus()
send:Fire('OnClick')
assert(editBox:HasFocus() and editBox:GetText() == '', 'Send did not restore focus')
assert(not composer.canSend, 'previous question is still being delivered')
assert(ChatFrameUtil.InsertLink(b) and draftHas(b), 'second link fell through while waiting')
editBox:Fire('OnEnterPressed')
assert(draftHas(b) and not send:IsEnabled(), 'a busy turn cleared or sent the next draft')
local active = CopyTable(ready); active.rev = ready.rev+1
for _,thread in ipairs(active.threads) do
    if thread.id == idle.id then
        thread.state = 'active'
        thread.turns = { {role='user',text='[Ветхий посох](item:35)', at=time()} }
    end
end
apply(active)
assert(ChatFrameUtil.InsertLink(q) and draftHas(q), 'link dropped during an active turn')

-- A refresh or an answer cannot reclaim focus from a different input.
editBox:ClearFocus()
local nativeChat = {}; keyboard = nativeChat
local completed = CopyTable(active); completed.rev = active.rev+1
for _,thread in ipairs(completed.threads) do
    if thread.id == idle.id then thread.state = 'idle' end
end
apply(completed)
assert(composer.canSend and keyboard == nativeChat and not ChatFrameUtil.InsertLink(a) and delegated == 1)
assert(draftHas(b) and draftHas(q), 'reply completion cleared the next draft')
send:Fire('OnClick')
assert(keyboard == nativeChat, 'submit stole an existing keyboard focus')
print('Shift-click: offline draft, second attachment during delivery, active turn, send focus and normal chat OK')

-- No helper revisions arrive after a long first question. The local clock must
-- make the Retry control visible even if every attempted slot read fails.
local loadSlot, replay = addon.Transport.Load, addon.Transport.Resend
local replays = 0
addon.Transport.Load = function() return false, 'unreadable' end
addon.Transport.Resend = function(...) replays = replays + 1; return replay(...) end
local firstQuestion = string.rep('long initial question ', 180)
local pending = assert(addon.Actions.Ask(firstQuestion))
local seq = assert(tonumber(pending.origin:match(':(%d+)$')))
addon.Window.Select(pending.id)
editBox:SetFocus()
assert(ChatFrameUtil.InsertLink(s))
local nextDraft = editBox:GetText()
wow.Run(3)
assert(addon.Store.Delivery(seq) == 'sending', 'timeout counted while frames were still being sent')
local function finishTransfer()
    while addon.Transport.Sending(seq) do wow.Run(0.04) end
end
finishTransfer()
assert(addon.Store.Delivery(seq) == 'sent')
local function retryRow()
    for _, f in ipairs(wow.frames) do
        if f.failed and f.resend and f.resend:IsVisible() then return f end
    end
end
wow.Run(3)
local row = assert(retryRow(), 'no visible retry when the helper never publishes another payload')
assert(addon.Store.Delivery(seq) == 'failed' and row.failed:GetText():find('Нет подтверждения',1,true))
assert(not addon.Store.Tracked(seq) and replays == 0 and not composer.canSend)
assert(not addon.Actions.Reply(pending, 'local chat id must never be sent'))
assert(editBox:GetText() == nextDraft, 'timeout erased the next draft')
row.resend:Fire('OnClick')
assert(replays == 1 and addon.Transport.Sending(seq) and not retryRow())
finishTransfer()
addon.Transport.Load = loadSlot
local confirmation = CopyTable(completed)
confirmation.rev = completed.rev + 1
confirmation.acks = { [addon.Transport.session] = { seq } }
local adopted = { id='real-created-chat', agent='codex', model='gpt-6.1-sol', state='idle',
    origin=pending.origin, updated=time(), title='Retry', cwd='/game', turns={{ role='user', text=firstQuestion, at=time() }} }
confirmation.threads={adopted}
apply(confirmation)
assert(addon.Window.Current() == adopted.id and composer.canSend and addon.Store.Delivery(seq) == 'delivered')
assert(not retryRow() and replays == 1 and editBox:GetText() == nextDraft)
print('Delivery: local ACK timeout without new helper data, visible manual retry, confirmed chat and retained draft OK')
