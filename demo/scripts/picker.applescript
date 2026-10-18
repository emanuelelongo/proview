-- Locate a fixture in the native picker owned by the runner's GUI process.
-- Input is sent by cliclick: Standard Additions panels do not reliably activate
-- or accept synthetic keystrokes/AXOpen while hosted by a background osascript.
on run argv
    set targetPID to item 1 of argv as integer
    set filename to item 2 of argv
    tell application "System Events"
        set proc to a reference to (first application process whose unix id is targetPID)
        if (count windows of proc) is not 1 then error "Expected one owned file picker window"
        set win to front window of proc
        if not (exists static text "Import signature" of splitter group 1 of win) then error "Unexpected owned file picker"
        set position of win to {100, 100}
        perform action "AXRaise" of win
        delay 0.4
        set matches to {}
        set elements to entire contents of splitter group 1 of splitter group 1 of win
        repeat with element in elements
            try
                if role of element is "AXImage" then
                    set label to name of element
                    if label is filename or label starts with (filename & ",") then set end of matches to element
                end if
            end try
        end repeat
        if (count matches) is not 1 then error "Expected exactly one fixture in the native picker"
        set target to item 1 of matches
        set pos to position of target
        set dimensions to size of target
        set x to ((item 1 of pos) + (item 1 of dimensions) / 2) as integer
        set y to ((item 2 of pos) + (item 2 of dimensions) / 2) as integer
        return (x as text) & "," & (y as text)
    end tell
end run
