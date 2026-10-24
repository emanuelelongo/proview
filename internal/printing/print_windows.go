package printing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	printDialog    = windows.NewLazySystemDLL("comdlg32.dll").NewProc("PrintDlgExW")
	gdi            = windows.NewLazySystemDLL("gdi32.dll")
	deleteDC       = gdi.NewProc("DeleteDC")
	getDeviceCaps  = gdi.NewProc("GetDeviceCaps")
	startDoc       = gdi.NewProc("StartDocW")
	endDoc         = gdi.NewProc("EndDoc")
	abortDoc       = gdi.NewProc("AbortDoc")
	startPage      = gdi.NewProc("StartPage")
	endPage        = gdi.NewProc("EndPage")
	stretchDIBits  = gdi.NewProc("StretchDIBits")
	setStretchMode = gdi.NewProc("SetStretchBltMode")
	setBrushOrigin = gdi.NewProc("SetBrushOrgEx")
	globalFree     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
	com            = windows.NewLazySystemDLL("ole32.dll")
	coInitialize   = com.NewProc("CoInitializeEx")
	coUninitialize = com.NewProc("CoUninitialize")
)

type pageRange struct{ From, To uint32 }

// Match PRINTDLGEXW using pointer-sized handles and Go's native alignment.
type printDialogData struct {
	Size                                                 uint32
	Owner, DevMode, DevNames, DC                         windows.Handle
	Flags, Flags2, ExclusionFlags, RangeCount, MaxRanges uint32
	Ranges                                               *pageRange
	MinPage, MaxPage, Copies                             uint32
	Instance                                             windows.Handle
	Template                                             *uint16
	Callback                                             uintptr
	PropertyCount                                        uint32
	Properties                                           uintptr
	StartPage, Result                                    uint32
}

type documentInfo struct {
	Size                   int32
	Name, Output, DataType *uint16
	Type                   uint32
}

type bitmapInfo struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ColorsUsed, ColorsImportant  uint32
}

func printNative(ctx context.Context, job Job, _ string) error {
	if job.ParentWindow == 0 || job.RenderPage == nil {
		return fmt.Errorf("native printing requires a document window and page renderer")
	}
	// COM, the print dialog and its printer DC share one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initialized, _, _ := coInitialize.Call(0, 2) // COINIT_APARTMENTTHREADED
	if int32(initialized) < 0 {
		return fmt.Errorf("initialize Windows printing: HRESULT %#x", uint32(initialized))
	}
	defer coUninitialize.Call()
	ranges := make([]pageRange, 32)
	pd := printDialogData{
		Owner: windows.Handle(job.ParentWindow),
		// The driver owns copies/collation. Virtual PDF printers remain available.
		Flags:     0x100 | 0x40000 | 0x100000 | 0x4 | 0x800000,
		MaxRanges: uint32(len(ranges)), Ranges: &ranges[0],
		MinPage: 1, MaxPage: uint32(len(job.Pages)), Copies: 1,
		StartPage: 0xffffffff,
	}
	pd.Size = uint32(unsafe.Sizeof(pd))
	defer func() {
		if pd.DC != 0 {
			deleteDC.Call(uintptr(pd.DC))
		}
		if pd.DevMode != 0 {
			globalFree.Call(uintptr(pd.DevMode))
		}
		if pd.DevNames != 0 {
			globalFree.Call(uintptr(pd.DevNames))
		}
		runtime.KeepAlive(ranges)
	}()
	result, _, _ := printDialog.Call(uintptr(unsafe.Pointer(&pd)))
	if int32(result) < 0 {
		return fmt.Errorf("Windows print dialog: HRESULT %#x", uint32(result))
	}
	if pd.Result != 1 { // PD_RESULT_PRINT; Apply also means no print job.
		return ErrCanceled
	}
	pages := make([]int, 0, len(job.Pages))
	if pd.Flags&0x2 != 0 { // PD_PAGENUMS
		if pd.RangeCount > uint32(len(ranges)) {
			return fmt.Errorf("invalid print page ranges")
		}
		seen := make(map[int]bool)
		for _, r := range ranges[:pd.RangeCount] {
			if r.From < 1 || r.To < r.From || r.To > uint32(len(job.Pages)) {
				return fmt.Errorf("invalid print page range")
			}
			for p := int(r.From) - 1; p < int(r.To); p++ {
				if !seen[p] {
					pages = append(pages, p)
					seen[p] = true
				}
			}
		}
	} else {
		for p := range job.Pages {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return fmt.Errorf("no pages selected for printing")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString(job.Title)
	if err != nil {
		return err
	}
	info := documentInfo{Name: title}
	info.Size = int32(unsafe.Sizeof(info))
	dc := uintptr(pd.DC)
	started, _, callErr := startDoc.Call(dc, uintptr(unsafe.Pointer(&info)))
	if int32(started) <= 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return ErrCanceled
		}
		return fmt.Errorf("start printer job: %w", callErr)
	}
	finished := false
	defer func() {
		if !finished {
			abortDoc.Call(dc)
		}
	}()
	caps := func(index uintptr) int { v, _, _ := getDeviceCaps.Call(dc, index); return int(int32(v)) }
	width, height := caps(8), caps(10) // HORZRES, VERTRES: printable area
	dpiX, dpiY := caps(88), caps(90)
	if width <= 0 || height <= 0 || dpiX <= 0 || dpiY <= 0 {
		return fmt.Errorf("printer returned invalid paper dimensions")
	}
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := job.Pages[page]
		dpi := min(600., float64(min(dpiX, dpiY)))
		// Bound each page allocation, including unusually large paper sizes.
		dpi = min(dpi, 72*math.Sqrt(32_000_000/(size.Width*size.Height)))
		pixels, err := job.RenderPage(ctx, page, dpi)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		b := pixels.Bounds()
		if b.Empty() || int64(b.Dx())*int64(b.Dy()) > 33_000_000 {
			return fmt.Errorf("print page exceeds rendering limits")
		}
		bitmap := make([]byte, b.Dx()*b.Dy()*4)
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				r, g, blue, a := pixels.At(b.Min.X+x, b.Min.Y+y).RGBA()
				i := (y*b.Dx() + x) * 4
				bitmap[i], bitmap[i+1], bitmap[i+2], bitmap[i+3] = byte((blue+65535-a)>>8), byte((g+65535-a)>>8), byte((r+65535-a)>>8), 255
			}
		}
		header := bitmapInfo{Width: int32(b.Dx()), Height: -int32(b.Dy()), Planes: 1, BitCount: 32}
		header.Size = uint32(unsafe.Sizeof(header))
		scale := min(float64(width)/float64(dpiX)/size.Width, float64(height)/float64(dpiY)/size.Height)
		w, h := max(1, int(math.Round(size.Width*scale*float64(dpiX)))), max(1, int(math.Round(size.Height*scale*float64(dpiY))))
		x, y := (width-w)/2, (height-h)/2
		if ok, _, _ := startPage.Call(dc); int32(ok) <= 0 {
			return fmt.Errorf("start printer page")
		}
		setStretchMode.Call(dc, 4) // HALFTONE
		setBrushOrigin.Call(dc, 0, 0, 0)
		copied, _, _ := stretchDIBits.Call(dc, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0,
			uintptr(b.Dx()), uintptr(b.Dy()), uintptr(unsafe.Pointer(&bitmap[0])), uintptr(unsafe.Pointer(&header)), 0, 0xcc0020)
		runtime.KeepAlive(bitmap)
		if int32(copied) <= 0 {
			return fmt.Errorf("draw printer page")
		}
		if ok, _, _ := endPage.Call(dc); int32(ok) <= 0 {
			return fmt.Errorf("finish printer page")
		}
	}
	if ok, _, _ := endDoc.Call(dc); int32(ok) <= 0 {
		return fmt.Errorf("finish printer job")
	}
	finished = true
	return nil
}
