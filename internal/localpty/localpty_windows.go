//go:build windows

package localpty

// ConPTY, by hand.
//
// os/exec cannot start a process on a pseudo console: the console has to be
// passed as a PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE in an extended STARTUPINFO,
// and exec.Cmd has no way to add one. So the process is made with
// CreateProcess directly:
//
//  1. Two anonymous pipes, one each way.
//  2. CreatePseudoConsole on their far ends. The console keeps its own copies
//     of those ends, so ours are closed straight away -- otherwise the output
//     pipe would never report end of file.
//  3. A STARTUPINFOEX carrying the console as an attribute, and CreateProcess
//     with EXTENDED_STARTUPINFO_PRESENT, suspended.
//  4. The process put in a job object that kills everything in it when the job
//     is closed, then resumed. Started suspended so that nothing it starts can
//     escape the job by being quicker than AssignProcessToJobObject.
//
// Two things must NOT be done here:
//
//   - CREATE_NO_WINDOW (what internal/procattr.Hide sets on the app's
//     background processes). It tells Windows to give the process a hidden
//     console of its own, and a process with its own console is not attached
//     to the pseudo console: its output never reaches the pipe, and the dock
//     stays blank. ConPTY is already headless -- its conhost has no window --
//     so there is nothing to hide anyway.
//   - Inheriting this process's standard handles. STARTF_USESTDHANDLES with no
//     handles makes sure the child talks to the pseudo console rather than to
//     whatever stdout this GUI process may have been given.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const supported = true

// consoleCloseGrace bounds how long the output pipe is kept open waiting for
// the pseudo console to flush once it has been told to close.
const consoleCloseGrace = 3 * time.Second

type conPTY struct {
	hpc     windows.Handle
	process windows.Handle
	job     windows.Handle
	pidv    int

	in  *os.File // what we write: the console's input
	out *os.File // what we read: the console's output

	done     chan struct{}
	exitCode int
	exitErr  error

	mu            sync.Mutex
	consoleClosed bool
	closeOnce     sync.Once
}

// available reports whether this Windows has ConPTY at all: Windows 10 1809
// and later. Asked first because x/sys panics, rather than returning an error,
// when a lazily loaded function is missing.
func available() error {
	if err := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find(); err != nil {
		return errors.New("a local terminal needs ConPTY, which is in Windows 10 version 1809 and later")
	}
	return nil
}

func coord(cols, rows uint16) windows.Coord {
	clamp := func(v uint16) int16 {
		if v > 0x7fff {
			return 0x7fff
		}
		return int16(v) // #nosec G115 -- clamped to int16 just above
	}
	return windows.Coord{X: clamp(cols), Y: clamp(rows)}
}

func start(opts Options) (impl, error) {
	if err := available(); err != nil {
		return nil, err
	}

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("could not make the terminal's input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
		return nil, fmt.Errorf("could not make the terminal's output pipe: %w", err)
	}

	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(coord(opts.Cols, opts.Rows), inRead, outWrite, 0, &hpc); err != nil {
		closeHandles(inRead, inWrite, outRead, outWrite)
		return nil, fmt.Errorf("could not create a pseudo console: %w", err)
	}
	// The console has its own copies of these.
	closeHandles(inRead, outWrite)

	c := &conPTY{
		hpc:  hpc,
		in:   os.NewFile(uintptr(inWrite), "conpty-in"),
		out:  os.NewFile(uintptr(outRead), "conpty-out"),
		done: make(chan struct{}),
	}
	if err := c.spawn(opts); err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = c.in.Close()
		_ = c.out.Close()
		return nil, err
	}
	go c.reap()
	return c, nil
}

func (c *conPTY) spawn(opts Options) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("could not make the process attributes: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON itself, not a pointer to it -- which
	// is what UpdateProcThreadAttribute is documented to take for
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE. The handle's bits are reinterpreted
	// as a pointer rather than converted, which go vet would rightly flag.
	// #nosec G103 -- required by the Win32 API; the value is never dereferenced by Go.
	value := *(*unsafe.Pointer)(unsafe.Pointer(&c.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, value, unsafe.Sizeof(c.hpc)); err != nil {
		return fmt.Errorf("could not attach the pseudo console: %w", err)
	}

	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si)) // #nosec G103 -- the Win32 struct size
	si.Flags = windows.STARTF_USESTDHANDLES

	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{opts.Path}, opts.Args...)))
	if err != nil {
		return err
	}
	appName, err := windows.UTF16PtrFromString(opts.Path)
	if err != nil {
		return err
	}
	var dir *uint16
	if opts.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(opts.Dir); err != nil {
			return err
		}
	}
	var env *uint16
	if opts.Env != nil {
		block := envBlock(opts.Env)
		env = &block[0]
	}

	// Deliberately no CREATE_NO_WINDOW: see the top of this file.
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(appName, cmdLine, nil, nil, false, flags, env, dir, &si.StartupInfo, &pi); err != nil {
		return fmt.Errorf("could not start %s: %w", opts.Path, err)
	}

	c.process = pi.Process
	c.pidv = int(pi.ProcessId)
	c.job = killOnCloseJob(pi.Process)
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		closeHandles(pi.Thread, pi.Process)
		if c.job != 0 {
			closeHandles(c.job)
		}
		return fmt.Errorf("could not resume %s: %w", opts.Path, err)
	}
	closeHandles(pi.Thread)
	return nil
}

// killOnCloseJob puts a process in a job that kills everything in it when the
// job's last handle closes, so that ending the shell also ends whatever it
// started. Zero when that could not be arranged, in which case only the
// process itself is terminated on close.
func killOnCloseJob(process windows.Handle) windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	// #nosec G103 -- the Win32 API takes the struct by address.
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		closeHandles(job)
		return 0
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		closeHandles(job)
		return 0
	}
	return job
}

// envBlock is an environment as CreateProcess wants it with
// CREATE_UNICODE_ENVIRONMENT: UTF-16 KEY=value strings, each ended by a NUL,
// sorted by name without regard to case, with one more NUL at the end.
func envBlock(env []string) []uint16 {
	sorted := slices.Clone(env)
	slices.SortStableFunc(sorted, func(a, b string) int {
		return strings.Compare(strings.ToUpper(envName(a)), strings.ToUpper(envName(b)))
	})
	var block []uint16
	for _, kv := range sorted {
		if strings.ContainsRune(kv, 0) {
			continue
		}
		block = append(block, utf16.Encode([]rune(kv))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0)
}

func envName(kv string) string {
	// Windows keeps a few variables named "=C:" and the like; the name is
	// everything up to the first '=' after the first character.
	if i := strings.IndexByte(kv[min(1, len(kv)):], '='); i >= 0 {
		return kv[:i+min(1, len(kv))]
	}
	return kv
}

func (c *conPTY) reap() {
	_, _ = windows.WaitForSingleObject(c.process, windows.INFINITE)
	var code uint32
	if err := windows.GetExitCodeProcess(c.process, &code); err != nil {
		c.exitCode, c.exitErr = -1, err
	} else {
		c.exitCode = int(code)
	}
	close(c.done)
	// The console holds the output pipe open for as long as it exists, so
	// the reader only sees the end once it is closed. Closing it also flushes
	// whatever the process wrote last.
	c.closeConsole()
}

// closeConsole closes the pseudo console once, then the output pipe -- after
// the console has flushed, or after a grace period if it will not.
func (c *conPTY) closeConsole() {
	c.mu.Lock()
	if c.consoleClosed {
		c.mu.Unlock()
		return
	}
	c.consoleClosed = true
	c.mu.Unlock()

	flushed := make(chan struct{})
	go func() {
		// On Windows before 11 24H2 this blocks until the output pipe has
		// been drained, which is why it runs apart and is given a deadline.
		windows.ClosePseudoConsole(c.hpc)
		close(flushed)
	}()
	go func() {
		select {
		case <-flushed:
		case <-time.After(consoleCloseGrace):
		}
		_ = c.out.Close()
	}()
	_ = c.in.Close()
}

func (c *conPTY) Read(b []byte) (int, error) {
	n, err := c.out.Read(b)
	if err != nil && (errors.Is(err, os.ErrClosed) || errors.Is(err, windows.ERROR_BROKEN_PIPE)) {
		err = io.EOF
	}
	return n, err
}

func (c *conPTY) Write(b []byte) (int, error) { return c.in.Write(b) }

func (c *conPTY) resize(cols, rows uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consoleClosed {
		return nil
	}
	return windows.ResizePseudoConsole(c.hpc, coord(cols, rows))
}

func (c *conPTY) close() error {
	c.closeOnce.Do(func() {
		// The whole tree when there is a job, the shell alone when there is
		// not. Closing the job handle would do the same through
		// KILL_ON_JOB_CLOSE; terminating first makes it happen now.
		if c.job != 0 {
			_ = windows.TerminateJobObject(c.job, 1)
		} else {
			_ = windows.TerminateProcess(c.process, 1)
		}
		c.closeConsole()
		go func() {
			<-c.done
			if c.job != 0 {
				closeHandles(c.job)
			}
			closeHandles(c.process)
		}()
	})
	return nil
}

func (c *conPTY) wait() (int, error) {
	<-c.done
	return c.exitCode, c.exitErr
}

func (c *conPTY) pid() int { return c.pidv }

func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		_ = windows.CloseHandle(h)
	}
}
