; ZEAM NSIS Installer Hooks
; Custom logic for install/uninstall

!macro NSIS_HOOK_PREINSTALL
    ; Kill any running ZEAM processes
    nsExec::ExecToLog 'taskkill /F /IM zeam.exe'
    nsExec::ExecToLog 'taskkill /F /IM zeam-daemon.exe'
    nsExec::ExecToLog 'taskkill /F /IM zeam-daemon-x86_64-pc-windows-gnu.exe'
    nsExec::ExecToLog 'taskkill /F /IM zeam-shell.exe'
    Sleep 1000

    ; Check for old app data and offer to delete
    IfFileExists "$PROFILE\.zeam\*.*" 0 NoOldData
        MessageBox MB_YESNO "Found existing ZEAM app data at:$\r$\n$PROFILE\.zeam$\r$\n$\r$\nDelete it for a clean install?" IDNO NoOldData
        RMDir /r "$PROFILE\.zeam"
    NoOldData:
!macroend

!macro NSIS_HOOK_POSTINSTALL
    ; Nothing extra needed after install
!macroend

!macro NSIS_HOOK_PREUNINSTALL
    ; Kill any running ZEAM processes
    nsExec::ExecToLog 'taskkill /F /IM zeam.exe'
    nsExec::ExecToLog 'taskkill /F /IM zeam-daemon.exe'
    nsExec::ExecToLog 'taskkill /F /IM zeam-daemon-x86_64-pc-windows-gnu.exe'
    Sleep 1000
!macroend

!macro NSIS_HOOK_POSTUNINSTALL
    ; Offer to remove app data
    IfFileExists "$PROFILE\.zeam\*.*" 0 NoAppData
        MessageBox MB_YESNO "Remove ZEAM app data?$\r$\n$\r$\nThis includes your identity, logs, and cache.$\r$\nLocation: $PROFILE\.zeam" IDNO NoAppData
        RMDir /r "$PROFILE\.zeam"
    NoAppData:
!macroend
