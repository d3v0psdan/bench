; Bench's NSIS hooks (bundle.windows.nsis.installerHooks in tauri.conf.json).
;
; Installing or updating replaces benchd.exe, so a running Bench is stopped
; first; the app starts it again. Uninstalling (not an update, which passes
; /UPDATE) also removes the machine setup Bench made: the .test DNS rule
; and the trusted local HTTPS authority. That asks for elevation once.
; Failures never block the (un)install: a Bench that isn't running, or a
; setup that was never made, is fine; a failed undo is logged.

!macro NSIS_HOOK_PREINSTALL
  nsExec::Exec '"$INSTDIR\bench.exe" stop'
  Pop $0
!macroend

; An interactive uninstall asks first (a manual reinstall runs it too, and
; the setup would then have to be made again); a silent one removes it.
!macro NSIS_HOOK_PREUNINSTALL
  ${If} $UpdateMode <> 1
    StrCpy $1 "yes"
    ${IfNot} ${Silent}
      MessageBox MB_YESNO|MB_ICONQUESTION "Also remove Bench's HTTPS authority and .test DNS setup from this computer? Windows asks for admin rights once." IDYES +2
      StrCpy $1 "no"
    ${EndIf}
    ${If} $1 == "yes"
      nsExec::ExecToLog '"$INSTDIR\bench.exe" setup --undo'
      Pop $0
      ${If} $0 != 0
        DetailPrint "Bench couldn't remove its HTTPS and DNS setup (exit $0); run bench setup --undo to retry."
      ${EndIf}
    ${EndIf}
  ${EndIf}
  nsExec::Exec '"$INSTDIR\bench.exe" stop'
  Pop $0
!macroend
