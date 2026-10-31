//go:build cgo && !nocgo && !extlib

package pdfengine

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

type FormField struct {
	Page, ID, Kind, Flags, MaxLen      int
	X, Y, Width, Height                float64
	Name, Value, OnValue               string
	Options, ExportOptions             []string
	Values, DefaultValues, ResetFields []string
	Caption                            string
	ResetExclude                       bool
}

const (
	FormReset    = 1
	FormCheckbox = 2
	FormCombo    = 3
	FormList     = 4
	FormRadio    = 5
	FormText     = 7
)

func ReadForms(data []byte) ([]FormField, error) {
	if len(data) == 0 {
		return nil, errors.New("pdfengine: empty PDF")
	}
	input := cBytes(data)
	var result C.pv_form_result
	defer C.pv_free_forms(&result)
	C.pv_read_forms((*C.uchar)(input), C.size_t(len(data)), &result)
	if message := C.GoString(&result.error[0]); message != "" {
		return nil, fmt.Errorf("pdfengine: %s", message)
	}
	var fields []FormField
	for _, f := range unsafe.Slice(result.fields, int(result.count)) {
		field := FormField{Page: int(f.page), ID: int(f.id), Kind: int(f.kind), Flags: int(f.flags), MaxLen: int(f.max_len), X: float64(f.x), Y: float64(f.y), Width: float64(f.width), Height: float64(f.height), Name: C.GoString(f.name), Value: C.GoString(f.value), OnValue: C.GoString(f.on_value), Caption: C.GoString(f.caption), ResetExclude: f.reset_exclude != 0}
		for _, v := range unsafe.Slice(f.values, int(f.value_count)) {
			field.Values = append(field.Values, C.GoString(v))
		}
		for _, v := range unsafe.Slice(f.defaults, int(f.default_count)) {
			field.DefaultValues = append(field.DefaultValues, C.GoString(v))
		}
		for _, v := range unsafe.Slice(f.reset_fields, int(f.reset_count)) {
			field.ResetFields = append(field.ResetFields, C.GoString(v))
		}
		for _, option := range unsafe.Slice(f.options, int(f.option_count)) {
			field.Options = append(field.Options, C.GoString(option))
		}
		for _, option := range unsafe.Slice(f.export_options, int(f.option_count)) {
			field.ExportOptions = append(field.ExportOptions, C.GoString(option))
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// FillForms preserves interactive widgets and regenerates their appearances.
func FillForms(data []byte, values map[int]string) ([]byte, error) {
	edits := make(map[int][]string, len(values))
	for id, value := range values {
		edits[id] = []string{value}
	}
	return FillFormValues(data, edits)
}

// FillFormValues supports scalar values and arrays for multiple-selection lists.
func FillFormValues(data []byte, values map[int][]string) ([]byte, error) {
	if len(values) == 0 {
		return data, nil
	}
	if len(data) == 0 {
		return nil, errors.New("pdfengine: empty PDF")
	}
	keys := make([]int, 0, len(values))
	for id := range values {
		keys = append(keys, id)
	}
	sort.Ints(keys)
	array := C.calloc(C.size_t(len(keys)), C.size_t(unsafe.Sizeof(C.pv_form_edit{})))
	if array == nil {
		return nil, errors.New("pdfengine: allocate field edits")
	}
	defer C.free(array)
	edits := unsafe.Slice((*C.pv_form_edit)(array), len(keys))
	for i, id := range keys {
		if id <= 0 || id > math.MaxInt32 {
			return nil, errors.New("pdfengine: invalid field ID")
		}
		edits[i].id = C.int(id)
		edits[i].count = C.int(len(values[id]))
		if len(values[id]) == 0 {
			continue
		}
		stringsArray := C.calloc(C.size_t(len(values[id])), C.size_t(unsafe.Sizeof(uintptr(0))))
		if stringsArray == nil {
			return nil, errors.New("pdfengine: allocate field values")
		}
		defer C.free(stringsArray)
		edits[i].values = (**C.char)(stringsArray)
		for j, value := range values[id] {
			if strings.IndexByte(value, 0) >= 0 {
				return nil, errors.New("pdfengine: invalid field value")
			}
			chars := C.CString(value)
			defer C.free(unsafe.Pointer(chars))
			unsafe.Slice(edits[i].values, len(values[id]))[j] = chars
		}
	}
	input := cBytes(data)
	var result C.pv_result
	defer C.pv_free_result(&result)
	C.pv_fill_form((*C.uchar)(input), C.size_t(len(data)), (*C.pv_form_edit)(array), C.size_t(len(keys)), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	return C.GoBytes(unsafe.Pointer(result.bytes), C.int(result.length)), nil
}

// RenderFormPage includes widget appearances, which go-fitz's content-only
// renderer omits. The document remains interactive; this is only a preview.
func RenderFormPage(data []byte, page int, dpi float64) (image.Image, error) {
	if len(data) == 0 || page < 0 || page > math.MaxInt32 || math.IsNaN(dpi) || dpi <= 0 || dpi > 600 {
		return nil, errors.New("pdfengine: invalid form preview")
	}
	input := cBytes(data)
	var result C.pv_result
	defer C.pv_free_result(&result)
	C.pv_render_forms((*C.uchar)(input), C.size_t(len(data)), C.int(page), C.float(dpi), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	width, height := int(result.raster_width), int(result.raster_height)
	components, stride, length := int(result.raster_components), int(result.raster_stride), int(result.length)
	if width <= 0 || height <= 0 || components < 3 || stride < width*components || length < stride*height {
		return nil, errors.New("pdfengine: invalid form preview raster")
	}
	samples := unsafe.Slice((*byte)(unsafe.Pointer(result.bytes)), length)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		src := samples[y*stride : y*stride+width*components]
		dst := out.Pix[y*out.Stride : y*out.Stride+width*4]
		switch components {
		case 4:
			copy(dst, src)
		case 3:
			for x := range width {
				dst[x*4+0], dst[x*4+1], dst[x*4+2], dst[x*4+3] = src[x*3+0], src[x*3+1], src[x*3+2], 0xff
			}
		default:
			return nil, fmt.Errorf("pdfengine: unsupported form preview raster components: %d", components)
		}
	}
	return out, nil
}

// FormRenderer keeps a MuPDF document open for repeated page previews. Calls
// and Close are serialized because a MuPDF context is not concurrently safe.
type FormRenderer struct {
	mu     sync.Mutex
	handle *C.pv_form_renderer
}

func NewFormRenderer(data []byte) (*FormRenderer, error) {
	if len(data) == 0 {
		return nil, errors.New("pdfengine: empty PDF")
	}
	input := cBytes(data)
	var result C.pv_result
	handle := C.pv_form_renderer_new((*C.uchar)(input), C.size_t(len(data)), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	if handle == nil {
		return nil, errors.New("pdfengine: cannot create form renderer")
	}
	return &FormRenderer{handle: handle}, nil
}

// RenderPage renders directly into one Go-owned raster. The context mutex is
// held across native raster preparation and the copy into Go-owned memory.
func (r *FormRenderer) RenderPage(page int, dpi float64) (*image.RGBA, error) {
	if r == nil || page < 0 || page > math.MaxInt32 || math.IsNaN(dpi) || math.IsInf(dpi, 0) || dpi <= 0 || dpi > 600 {
		return nil, errors.New("pdfengine: invalid form preview")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handle == nil {
		return nil, errors.New("pdfengine: form renderer is closed")
	}
	var result C.pv_result
	C.pv_form_renderer_prepare(r.handle, C.int(page), C.float(dpi), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	width, height := int(result.raster_width), int(result.raster_height)
	maxInt := int(^uint(0) >> 1)
	if width <= 0 || height <= 0 || width > math.MaxInt32/4 || height > math.MaxInt32 ||
		height > maxInt/(width*4) {
		C.pv_form_renderer_discard(r.handle)
		return nil, errors.New("pdfengine: invalid form preview raster")
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	C.pv_form_renderer_copy(r.handle, (*C.uchar)(unsafe.Pointer(&out.Pix[0])),
		C.int(width), C.int(height), C.int(out.Stride), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FormRenderer) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handle != nil {
		C.pv_form_renderer_drop(r.handle)
		r.handle = nil
	}
}

type PDFInfo struct {
	AnnotPages    map[int]bool
	HasImportable bool
	HasForms      bool
}

func InspectPDF(data []byte) (PDFInfo, error) {
	if len(data) == 0 {
		return PDFInfo{}, errors.New("pdfengine: empty PDF")
	}
	input := cBytes(data)
	var result C.pv_pdf_info
	defer C.pv_free_pdf_info(&result)
	C.pv_inspect_pdf((*C.uchar)(input), C.size_t(len(data)), &result)
	if message := C.GoString(&result.error[0]); message != "" {
		return PDFInfo{}, fmt.Errorf("pdfengine: %s", message)
	}
	var pages map[int]bool
	if result.annot_page_count > 0 {
		pages = make(map[int]bool, int(result.annot_page_count))
		for _, page := range unsafe.Slice(result.annot_pages, int(result.annot_page_count)) {
			pages[int(page)] = true
		}
	}
	return PDFInfo{
		AnnotPages:    pages,
		HasImportable: result.has_importable != 0,
		HasForms:      result.has_forms != 0,
	}, nil
}
