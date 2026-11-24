//go:build windows && amd64

package kiosk

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	pEnumWindows              = user32.NewProc("EnumWindows")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pGetWindowTextW           = user32.NewProc("GetWindowTextW")
	pIsIconic                 = user32.NewProc("IsIconic")
	pShowWindow               = user32.NewProc("ShowWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pBringWindowToTop         = user32.NewProc("BringWindowToTop")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pSendInput                = user32.NewProc("SendInput")
)

const didukung = true

const (
	inputKeyboard    = 1
	keyeventfKeyup   = 0x0002
	keyeventfUnicode = 0x0004
	vkTab            = 0x09
	vkReturn         = 0x0D
	vkMenu           = 0x12
	swRestore        = 9
)

// keyInput = struct INPUT (type KEYBOARD) untuk amd64, ukuran 40 byte.
type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

// Gagal compile bila ukuran keyInput bukan 40 byte (sizeof(INPUT) Windows x64).
var _ = [1]struct{}{}[unsafe.Sizeof(keyInput{})-40]

// ---- pencarian jendela (satu callback global; jumlah callback Windows terbatas) ----

var (
	cariMu    sync.Mutex
	cariPID   uint32
	cariJudul string
	cariHasil uintptr
	enumCB    = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == cariPID {
			cariHasil = hwnd
			return 0
		}
		if cariJudul != "" && strings.Contains(strings.ToLower(judulJendela(hwnd)), cariJudul) {
			cariHasil = hwnd
			return 0
		}
		return 1
	})
)

func judulJendela(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func cariJendela(pid int, judul string, batas time.Duration) (uintptr, error) {
	if batas <= 0 {
		batas = 15 * time.Second
	}
	akhir := time.Now().Add(batas)
	for time.Now().Before(akhir) {
		cariMu.Lock()
		cariPID, cariJudul, cariHasil = uint32(pid), strings.ToLower(judul), 0
		pEnumWindows.Call(enumCB, 0)
		h := cariHasil
		cariMu.Unlock()
		if h != 0 {
			return h, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return 0, fmt.Errorf("jendela aplikasi tidak muncul dalam %s (isi *_JUDUL bila exe adalah launcher)", batas)
}

func fokusJendela(hwnd uintptr) error {
	for i := 0; i < 5; i++ {
		if v, _, _ := pIsIconic.Call(hwnd); v != 0 {
			pShowWindow.Call(hwnd, swRestore)
		}
		// Tekan-lepas ALT agar Windows mengizinkan SetForegroundWindow dari proses latar.
		_ = kirim([]keyInput{{typ: inputKeyboard, vk: vkMenu}, {typ: inputKeyboard, vk: vkMenu, flags: keyeventfKeyup}})
		pSetForegroundWindow.Call(hwnd)
		pBringWindowToTop.Call(hwnd)
		time.Sleep(150 * time.Millisecond)
		if fg, _, _ := pGetForegroundWindow.Call(); fg == hwnd {
			return nil
		}
	}
	return fmt.Errorf("jendela aplikasi tidak bisa dibawa ke depan; pengetikan dibatalkan")
}

func kirim(in []keyInput) error {
	n, _, err := pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(n) != len(in) {
		return fmt.Errorf("SendInput gagal: %v", err)
	}
	return nil
}

func ketikTeks(s string) error {
	for _, u := range utf16.Encode([]rune(s)) {
		if err := kirim([]keyInput{
			{typ: inputKeyboard, scan: u, flags: keyeventfUnicode},
			{typ: inputKeyboard, scan: u, flags: keyeventfUnicode | keyeventfKeyup},
		}); err != nil {
			return err
		}
		time.Sleep(15 * time.Millisecond)
	}
	return nil
}

func tekan(vk uint16) error {
	return kirim([]keyInput{{typ: inputKeyboard, vk: vk}, {typ: inputKeyboard, vk: vk, flags: keyeventfKeyup}})
}

func tekanTab() error   { return tekan(vkTab) }
func tekanEnter() error { return tekan(vkReturn) }
