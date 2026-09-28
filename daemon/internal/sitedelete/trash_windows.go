//go:build windows

package sitedelete

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"
)

// SHFileOperationW (shell32) moves files to the Recycle Bin. Called in
// process rather than through PowerShell so no error dialog can open from
// the background daemon: FOF_NOERRORUI returns failures as a code instead.
var procSHFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

const (
	foDelete           = 0x3
	fofSilent          = 0x4
	fofNoConfirmation  = 0x10
	fofAllowUndo       = 0x40 // to the Recycle Bin, not gone for good
	fofNoErrorUI       = 0x400
	fofWantNukeWarning = 0x4000 // ask before deleting outright what the bin can't hold
)

// shFileOpStruct is SHFILEOPSTRUCTW with the platform's natural alignment
// (shellapi.h packs it to 1 byte only on 32-bit Windows).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// trash moves path to the Recycle Bin, so a deleted project can be
// restored. A folder too big for the bin is only deleted outright after
// Windows asks the user (the one dialog this can show, by design). The
// call can't be interrupted, so ctx is unused; the task isn't cancellable.
func trash(_ context.Context, path string) error {
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return fmt.Errorf("the path can't be passed to Windows: %w", err)
	}
	from = append(from, 0) // pFrom is a list ended by an empty string
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI | fofWantNukeWarning,
	}
	code, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	switch {
	case code != 0:
		return fmt.Errorf("the Recycle Bin refused it (shell error 0x%x); close programs using files in it and try again", code)
	case op.fAnyOperationsAborted != 0:
		return fmt.Errorf("moving it was cancelled, so the folder is kept")
	}
	return nil
}
