local _, T = ...

local Skin = {}
T.JournalSkin = Skin
local path = T.media .. "Journal\\"

function Skin.Surface(frame, name, layer)
    local texture = frame:CreateTexture(nil, layer or "BACKGROUND", nil, 1)
    texture:SetAllPoints()
    texture:SetTexture(path .. name)
    return texture
end

function Skin.Icon(frame, name, size)
    local texture = frame:CreateTexture(nil, "ARTWORK")
    texture:SetSize(size, size)
    texture:SetTexture(path .. name)
    return texture
end

-- Keep the painted caps of the quest journal buttons at their original width.
function Skin.Tab(parent, name, onClick)
    local button = CreateFrame("Button", nil, parent)
    local left, center, right = button:CreateTexture(nil, "BORDER"), button:CreateTexture(nil, "BORDER"), button:CreateTexture(nil, "BORDER")
    left:SetPoint("TOPLEFT")
    left:SetPoint("BOTTOMLEFT")
    left:SetWidth(8)
    left:SetTexCoord(0, 0.28, 0, 1)
    right:SetPoint("TOPRIGHT")
    right:SetPoint("BOTTOMRIGHT")
    right:SetWidth(8)
    right:SetTexCoord(0.72, 1, 0, 1)
    center:SetPoint("TOPLEFT", left, "TOPRIGHT")
    center:SetPoint("BOTTOMRIGHT", right, "BOTTOMLEFT")
    center:SetTexCoord(0.28, 0.72, 0, 1)
    button.label = T.Kit.Text(button, "t-ui-strong", "gold", "OVERLAY")
    button.label:SetPoint("LEFT", 8, 0)
    button.label:SetPoint("RIGHT", -8, 0)
    button.label:SetJustifyH("CENTER")
    button.label:SetJustifyV("MIDDLE")
    button.label:SetWordWrap(false)
    T.BindLabel(button.label, name)
    function button:SetActive(active)
        active = not not active
        if self.active == active then return end
        self.active = active
        local file = path .. (active and "RedTab" or "Tab")
        left:SetTexture(file)
        center:SetTexture(file)
        right:SetTexture(file)
        self.label:SetTextColor(active and 1 or .94, active and .84 or .77, active and .24 or .49)
    end
    local glow = button:CreateTexture(nil, "HIGHLIGHT")
    glow:SetAllPoints()
    glow:SetColorTexture(1, .79, .27, .13)
    button:SetActive(false)
    button:SetScript("OnClick", onClick)
    return button
end
