-- Control only the uniquely named window created by the demo runner.
on run argv
    set operation to item 1 of argv
    set marker to item 2 of argv
    set targetPID to (item 3 of argv) as integer
    tell application "System Events"
        set matches to {}
        if targetPID > 0 then
            set candidates to (application processes whose unix id is targetPID)
        else
            set candidates to (application processes whose background only is false)
        end if
        repeat with proc in candidates
            repeat with win in windows of proc
                if name of win contains marker then set end of matches to {proc, win}
            end repeat
        end repeat
        if (count matches) is not 1 then error "Expected exactly one demo window; found " & (count matches)
        set target to item 1 of matches
        set proc to item 1 of target
        set win to item 2 of target
        if operation is "place" then
            set position of win to {80, 60}
            set size of win to {1000, 740}
            set frontmost of proc to true
            perform action "AXRaise" of win
        else if operation is "assert" or operation is "key" or operation is "close" then
            if not frontmost of proc then error "Demo application lost focus; stopping input"
            if name of front window of proc does not contain marker then error "Demo window lost focus; stopping input"
        end if
        if operation is "key" then
            set keyName to item 4 of argv
            set keyModifiers to {}
            if item 5 of argv contains "command" then set end of keyModifiers to command down
            if item 5 of argv contains "shift" then set end of keyModifiers to shift down
            if item 5 of argv contains "option" then set end of keyModifiers to option down
            if item 5 of argv contains "control" then set end of keyModifiers to control down
            if keyName is "escape" then
                key code 53 using keyModifiers
            else if keyName is "return" then
                key code 36 using keyModifiers
            else if keyName is "pagedown" then
                key code 121 using keyModifiers
            else if keyName is "pageup" then
                key code 116 using keyModifiers
            else if keyName is "tab" then
                key code 48 using keyModifiers
            else
                keystroke keyName using keyModifiers
            end if
        else if operation is "close" then
            click (first button of win whose subrole is "AXCloseButton")
            return "closed"
        end if
        set pos to position of win
        set dimensions to size of win
        return (unix id of proc as text) & "," & (item 1 of pos) & "," & (item 2 of pos) & "," & (item 1 of dimensions) & "," & (item 2 of dimensions)
    end tell
end run
