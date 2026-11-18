; Included by the installer that mygo build makes (windows.installerInclude).

; The uninstaller has closed Clipnest already. If Win+V opened Clipnest,
; give it back to Windows: take out the V that Clipnest added to Explorer's
; DisabledHotkeys and restart Explorer.
!macro customUnInstall
  ExecWait '"$INSTDIR\Clipnest.exe" --restore-win-v'
!macroend
