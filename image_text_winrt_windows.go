//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// The Windows Runtime from Go without cgo. Its objects are COM interfaces,
// called through their method tables; a method's index counts IUnknown's
// three and IInspectable's three methods first, then the interface's own in
// the order of the Windows SDK's headers.

var (
	combase                       = syscall.NewLazyDLL("combase.dll")
	procRoInitialize              = combase.NewProc("RoInitialize")
	procRoUninitialize            = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procCoIncrementMTAUsage       = combase.NewProc("CoIncrementMTAUsage")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
)

var (
	iidOcrEngineStatics           = syscall.GUID{Data1: 0x5bffa85a, Data2: 0x3384, Data3: 0x3540, Data4: [8]byte{0x99, 0x40, 0x69, 0x91, 0x20, 0xd4, 0x28, 0xa8}}
	iidLanguageFactory            = syscall.GUID{Data1: 0x9b0252ac, Data2: 0x0c27, Data3: 0x44f8, Data4: [8]byte{0xb7, 0x92, 0x97, 0x93, 0xfb, 0x66, 0xc6, 0x3e}}
	iidSoftwareBitmapStatics      = syscall.GUID{Data1: 0xdf0385db, Data2: 0x672f, Data3: 0x4a9d, Data4: [8]byte{0x80, 0x6e, 0xc2, 0x44, 0x2f, 0x34, 0x3e, 0x86}}
	iidCryptographicBufferStatics = syscall.GUID{Data1: 0x320b7e22, Data2: 0x3cb0, Data3: 0x4cdf, Data4: [8]byte{0x86, 0x63, 0x1d, 0x28, 0x91, 0x00, 0x65, 0xeb}}
	iidAsyncInfo                  = syscall.GUID{Data1: 0x00000036, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// comObject is an interface pointer: its first word points to the method
// table. The object lives outside Go's heap.
type comObject struct{ methods unsafe.Pointer }

type hresultError struct {
	what string
	code uint32
}

func (e hresultError) Error() string { return fmt.Sprintf("%s 失败（0x%08X）", e.what, e.code) }

func failed(hr uint32) bool { return int32(hr) < 0 }

func check(what string, hr uint32) error {
	if failed(hr) {
		return hresultError{what, hr}
	}
	return nil
}

// call invokes method i; the result is an HRESULT in the low 32 bits.
//
//go:uintptrescapes
func (o *comObject) call(i int, args ...uintptr) uint32 {
	fn := *(*uintptr)(unsafe.Add(o.methods, uintptr(i)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uint32(r)
}

func (o *comObject) release() {
	if o != nil {
		o.call(2)
	}
}

// object calls a getter that returns an interface.
func (o *comObject) object(what string, i int) (*comObject, error) {
	var out *comObject
	if err := check(what, o.call(i, uintptr(unsafe.Pointer(&out)))); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New(what + " 没有返回对象")
	}
	return out, nil
}

// hstring is a Windows Runtime string handle; 0 is the empty string.
type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, check("WindowsCreateString", uint32(r))
}

func (h hstring) delete() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}

func (h hstring) String() string {
	if h == 0 {
		return ""
	}
	var n uint32
	r, _, _ := procWindowsGetStringRawBuffer.Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
	if r == 0 || n == 0 {
		return ""
	}
	// The characters belong to the handle, outside Go's heap.
	p := *(**uint16)(unsafe.Pointer(&r))
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// text calls a getter that returns a string.
func (o *comObject) text(what string, i int) (string, error) {
	var h hstring
	if err := check(what, o.call(i, uintptr(unsafe.Pointer(&h)))); err != nil {
		return "", err
	}
	defer h.delete()
	return h.String(), nil
}

// IVectorView<T>: GetAt is method 6, Size method 7.
func (v *comObject) size() (uint32, error) {
	var n uint32
	return n, check("IVectorView.Size", v.call(7, uintptr(unsafe.Pointer(&n))))
}

func (v *comObject) at(i uint32) (*comObject, error) {
	var out *comObject
	if err := check("IVectorView.GetAt", v.call(6, uintptr(i), uintptr(unsafe.Pointer(&out)))); err != nil {
		return nil, err
	}
	return out, nil
}

func activationFactory(class string, iid *syscall.GUID) (*comObject, error) {
	h, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer h.delete()
	var f *comObject
	r, _, _ := procRoGetActivationFactory.Call(uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	return f, check(class, uint32(r))
}

// enterMTA puts the locked thread in the multithreaded apartment. The
// apartment outlives the call: CoIncrementMTAUsage keeps it, and the engines
// created in it, alive while no thread is in it.
func enterMTA() (leave func(), err error) {
	r, _, _ := procRoInitialize.Call(1) // RO_INIT_MULTITHREADED
	switch hr := uint32(r); hr {
	case 0, 1: // S_OK, S_FALSE
		return func() { procRoUninitialize.Call() }, nil
	case 0x80010106: // RPC_E_CHANGED_MODE
		return nil, errors.New("线程已用于单线程 COM，无法调用文字识别")
	default:
		return nil, hresultError{"RoInitialize", hr}
	}
}

// await waits for an IAsyncOperation by polling, which needs no COM callback
// object. Windows recognizes a tile in 10 ms to about a second.
func await(op *comObject, timeout time.Duration) error {
	var info *comObject
	if err := check("IAsyncInfo", op.call(0, uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info)))); err != nil {
		return err
	}
	defer info.release()
	deadline := time.Now().Add(timeout)
	for {
		var status int32
		if err := check("IAsyncInfo.Status", info.call(7, uintptr(unsafe.Pointer(&status)))); err != nil {
			return err
		}
		switch status {
		case 1: // Completed
			return nil
		case 2: // Canceled
			return errors.New("文字识别被取消")
		case 3: // Error
			var code int32
			info.call(8, uintptr(unsafe.Pointer(&code)))
			return hresultError{"文字识别", uint32(code)}
		}
		if time.Now().After(deadline) {
			info.call(9) // Cancel
			return errors.New("文字识别超时")
		}
		time.Sleep(time.Millisecond)
	}
}
