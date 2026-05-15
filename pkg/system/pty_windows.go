//go:build windows

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func spawnPTY(ctx context.Context, cols, rows uint16, argv []string) (*PTYSession, error) {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 25
	}
	if len(argv) == 0 {
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		argv = []string{comspec}
	}

	var inR, inW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("system: create input pipe: %w", err)
	}
	var outR, outW windows.Handle
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		_ = windows.CloseHandle(inW)
		return nil, fmt.Errorf("system: create output pipe: %w", err)
	}

	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	var hPC windows.Handle
	if err := windows.CreatePseudoConsole(size, inR, outW, 0, &hPC); err != nil {
		close4(inR, inW, outR, outW)
		return nil, fmt.Errorf("system: CreatePseudoConsole: %w", err)
	}
	// ConPTY duplicates internally; close host copies passed in.
	_ = windows.CloseHandle(inR)
	_ = windows.CloseHandle(outW)

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, fmt.Errorf("system: proc thread attr list: %w", err)
	}
	defer attrList.Delete()

	pcVal := hPC
	if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(&pcVal), unsafe.Sizeof(pcVal)); err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, fmt.Errorf("system: UpdateProcThreadAttribute: %w", err)
	}

	si := &windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESHOWWINDOW
	si.ShowWindow = windows.SW_HIDE
	si.ProcThreadAttributeList = attrList.List()

	exePath, err := exec.LookPath(argv[0])
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, fmt.Errorf("system: look path: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, fmt.Errorf("system: abs exe: %w", err)
	}
	appName, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, err
	}
	cmdLine := windows.ComposeCommandLine(argv)
	cmdLineU, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, err
	}

	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(
		appName,
		cmdLineU,
		nil,
		nil,
		false,
		flags,
		nil,
		nil,
		(*windows.StartupInfo)(unsafe.Pointer(si)),
		&pi,
	); err != nil {
		windows.ClosePseudoConsole(hPC)
		close2(inW, outR)
		return nil, fmt.Errorf("system: CreateProcess: %w", err)
	}

	_ = windows.CloseHandle(pi.Thread)

	proc := pi.Process
	_ = ctx // cancellation is handled by the caller closing the session.

	inFile := os.NewFile(uintptr(inW), "ptystdin")
	outFile := os.NewFile(uintptr(outR), "ptystdout")

	return &PTYSession{
		Stdin:  inFile,
		Stdout: outFile,
		Resize: func(cols, rows uint16) error {
			if cols < 2 {
				cols = 80
			}
			if rows < 2 {
				rows = 25
			}
			sz := windows.Coord{X: int16(cols), Y: int16(rows)}
			return windows.ResizePseudoConsole(hPC, sz)
		},
		Close: func() error {
			_ = windows.TerminateProcess(proc, 1)
			_, _ = windows.WaitForSingleObject(proc, 8000)
			_ = windows.CloseHandle(proc)
			_ = inFile.Close()
			_ = outFile.Close()
			windows.ClosePseudoConsole(hPC)
			return nil
		},
		Wait: func() error {
			_, err := windows.WaitForSingleObject(proc, windows.INFINITE)
			return err
		},
	}, nil
}

func close2(a, b windows.Handle) {
	_ = windows.CloseHandle(a)
	_ = windows.CloseHandle(b)
}

func close4(a, b, c, d windows.Handle) {
	close2(a, b)
	close2(c, d)
}
