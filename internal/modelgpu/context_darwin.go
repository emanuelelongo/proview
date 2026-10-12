//go:build darwin && cgo

package modelgpu

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework OpenGL
#include <OpenGL/OpenGL.h>
#include <OpenGL/CGLCurrent.h>
static CGLError proviewCreateContext(CGLContextObj *ctx) {
 CGLPixelFormatAttribute attrs[] = {kCGLPFAOpenGLProfile, (CGLPixelFormatAttribute)kCGLOGLPVersion_3_2_Core,kCGLPFAAccelerated,0};
 CGLPixelFormatObj format = NULL;
 GLint count = 0;
 CGLError err = CGLChoosePixelFormat(attrs,&format,&count);
 if (err != kCGLNoError) return err;
 err=CGLCreateContext(format,NULL,ctx);
 CGLDestroyPixelFormat(format);
 if (err == kCGLNoError) err=CGLSetCurrentContext(*ctx);
 return err;
}
*/
import "C"

import (
	"context"
	"emanuelelongo/proview/internal/model3d"
	"fmt"
	"github.com/go-gl/gl/v3.2-core/gl"
	"image"
	"math"
	"strings"
	"sync"
)

type glDevice struct {
	context                                          C.CGLContextObj
	program, vao, vbo                                uint32
	framebuffer, color, depth, resolve, resolveColor uint32
	w, h                                             int
	mesh                                             *model3d.Mesh
	vertices                                         int32
	label                                            string
	samples                                          int32
}

var glInitOnce sync.Once
var glInitError error

func newDevice() (device, error) {
	d := &glDevice{}
	if err := C.proviewCreateContext(&d.context); err != C.kCGLNoError {
		d.close()
		return nil, fmt.Errorf("CGL: %s", C.GoString(C.CGLErrorString(err)))
	}
	glInitOnce.Do(func() { glInitError = gl.Init() })
	if glInitError != nil {
		d.close()
		return nil, glInitError
	}
	d.label = "GPU: " + gl.GoStr(gl.GetString(gl.RENDERER))
	var maxSamples int32
	gl.GetIntegerv(gl.MAX_SAMPLES, &maxSamples)
	d.samples = min(4, maxSamples)
	if d.samples < 1 {
		d.close()
		return nil, fmt.Errorf("multisample framebuffer unsupported")
	}
	var err error
	d.program, err = createProgram()
	if err != nil {
		d.close()
		return nil, err
	}
	gl.GenVertexArrays(1, &d.vao)
	gl.GenBuffers(1, &d.vbo)
	gl.BindVertexArray(d.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, d.vbo)
	for i := uint32(0); i < 2; i++ {
		gl.EnableVertexAttribArray(i)
		gl.VertexAttribPointerWithOffset(i, 3, gl.FLOAT, false, 24, uintptr(i)*12)
	}
	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.MULTISAMPLE)
	gl.Disable(gl.CULL_FACE)
	return d, nil
}
func (d *glDevice) name() string { return d.label }
func (d *glDevice) close() {
	if d.context == nil {
		return
	}
	C.CGLSetCurrentContext(d.context)
	// gl.Init may have failed before any resource was allocated.
	if d.program != 0 {
		gl.DeleteProgram(d.program)
	}
	if d.vbo != 0 {
		gl.DeleteBuffers(1, &d.vbo)
	}
	if d.vao != 0 {
		gl.DeleteVertexArrays(1, &d.vao)
	}
	d.releaseFramebuffers()
	d.mesh = nil
	C.CGLSetCurrentContext(nil)
	C.CGLDestroyContext(d.context)
	d.context = nil
}
func (d *glDevice) releaseFramebuffers() {
	for _, id := range []uint32{d.framebuffer, d.resolve} {
		if id != 0 {
			gl.DeleteFramebuffers(1, &id)
		}
	}
	for _, id := range []uint32{d.color, d.depth, d.resolveColor} {
		if id != 0 {
			gl.DeleteRenderbuffers(1, &id)
		}
	}
	d.framebuffer, d.resolve, d.color, d.depth, d.resolveColor = 0, 0, 0, 0, 0
}
func (d *glDevice) allocate(w, h int) error {
	if d.w == w && d.h == h && d.framebuffer != 0 {
		return nil
	}
	d.releaseFramebuffers()
	d.w, d.h = w, h
	gl.GenFramebuffers(1, &d.framebuffer)
	gl.BindFramebuffer(gl.FRAMEBUFFER, d.framebuffer)
	for _, target := range []struct {
		id                 *uint32
		format, attachment uint32
	}{{&d.color, gl.RGBA8, gl.COLOR_ATTACHMENT0}, {&d.depth, gl.DEPTH_COMPONENT24, gl.DEPTH_ATTACHMENT}} {
		gl.GenRenderbuffers(1, target.id)
		gl.BindRenderbuffer(gl.RENDERBUFFER, *target.id)
		gl.RenderbufferStorageMultisample(gl.RENDERBUFFER, d.samples, target.format, int32(w), int32(h))
		gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, target.attachment, gl.RENDERBUFFER, *target.id)
	}
	if gl.CheckFramebufferStatus(gl.FRAMEBUFFER) != gl.FRAMEBUFFER_COMPLETE {
		return fmt.Errorf("incomplete multisample framebuffer")
	}
	gl.GenFramebuffers(1, &d.resolve)
	gl.BindFramebuffer(gl.FRAMEBUFFER, d.resolve)
	gl.GenRenderbuffers(1, &d.resolveColor)
	gl.BindRenderbuffer(gl.RENDERBUFFER, d.resolveColor)
	gl.RenderbufferStorage(gl.RENDERBUFFER, gl.RGBA8, int32(w), int32(h))
	gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.RENDERBUFFER, d.resolveColor)
	if gl.CheckFramebufferStatus(gl.FRAMEBUFFER) != gl.FRAMEBUFFER_COMPLETE {
		return fmt.Errorf("incomplete resolve framebuffer")
	}
	return nil
}
func (d *glDevice) render(ctx context.Context, m *model3d.Mesh, c model3d.Camera, w, h int) (*image.RGBA, error) {
	if err := validate(m, c, w, h); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.mesh != m {
		data, err := m.GPUVertices(ctx)
		if err != nil {
			return nil, err
		}
		gl.BindBuffer(gl.ARRAY_BUFFER, d.vbo)
		gl.BufferData(gl.ARRAY_BUFFER, len(data)*4, gl.Ptr(data), gl.STATIC_DRAW)
		d.mesh = m
		d.vertices = int32(len(data) / 6)
	}
	if err := d.allocate(w, h); err != nil {
		return nil, err
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, d.framebuffer)
	gl.Viewport(0, 0, int32(w), int32(h))
	bg := model3d.BackgroundColor
	gl.ClearColor(float32(bg.R)/255, float32(bg.G)/255, float32(bg.B)/255, 1)
	gl.ClearDepth(1)
	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
	gl.UseProgram(d.program)
	gl.BindVertexArray(d.vao)
	sy, cy := math.Sincos(c.Yaw)
	sp, cp := math.Sincos(c.Pitch)
	rotation := []float32{float32(cy), float32(-sp * sy), float32(cp * sy), float32(-sy), float32(-sp * cy), float32(cp * cy), 0, float32(cp), float32(sp)}
	gl.UniformMatrix3fv(gl.GetUniformLocation(d.program, gl.Str("rotation\x00")), 1, false, &rotation[0])
	scale := float64(min(w, h)) * .29 * math.Max(.05, math.Min(c.Zoom, 30))
	gl.Uniform2f(gl.GetUniformLocation(d.program, gl.Str("scale\x00")), float32(2*scale/float64(w)), float32(2*scale/float64(h)))
	gl.Uniform2f(gl.GetUniformLocation(d.program, gl.Str("pan\x00")), float32(2*c.PanX), float32(-2*c.PanY))
	wire := gl.GetUniformLocation(d.program, gl.Str("wire\x00"))
	gl.Uniform1i(wire, 0)
	gl.DepthFunc(gl.LESS)
	if c.Wireframe {
		gl.ColorMask(false, false, false, false)
		gl.Enable(gl.POLYGON_OFFSET_FILL)
		gl.PolygonOffset(1, 1)
	}
	gl.DrawArrays(gl.TRIANGLES, 0, d.vertices)
	if c.Wireframe {
		gl.Disable(gl.POLYGON_OFFSET_FILL)
		gl.ColorMask(true, true, true, true)
		gl.Uniform1i(wire, 1)
		gl.DepthFunc(gl.LEQUAL)
		gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)
		gl.DrawArrays(gl.TRIANGLES, 0, d.vertices)
		gl.PolygonMode(gl.FRONT_AND_BACK, gl.FILL)
	}
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, d.framebuffer)
	gl.BindFramebuffer(gl.DRAW_FRAMEBUFFER, d.resolve)
	gl.BlitFramebuffer(0, 0, int32(w), int32(h), 0, 0, int32(w), int32(h), gl.COLOR_BUFFER_BIT, gl.NEAREST)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, d.resolve)
	frame := image.NewRGBA(image.Rect(0, 0, w, h))
	gl.ReadPixels(0, 0, int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(frame.Pix))
	if code := gl.GetError(); code != gl.NO_ERROR {
		return nil, fmt.Errorf("OpenGL error 0x%x", code)
	}
	// OpenGL readback starts at the bottom; Fyne/image coordinates start at the top.
	row := make([]byte, frame.Stride)
	for y := 0; y < h/2; y++ {
		a, b := y*frame.Stride, (h-1-y)*frame.Stride
		copy(row, frame.Pix[a:a+frame.Stride])
		copy(frame.Pix[a:a+frame.Stride], frame.Pix[b:b+frame.Stride])
		copy(frame.Pix[b:b+frame.Stride], row)
	}
	return frame, ctx.Err()
}
func compileShader(kind uint32, source string) (uint32, error) {
	shader := gl.CreateShader(kind)
	text, free := gl.Strs(source + "\x00")
	defer free()
	gl.ShaderSource(shader, 1, text, nil)
	gl.CompileShader(shader)
	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var length int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &length)
		info := strings.Repeat("\x00", int(length)+1)
		gl.GetShaderInfoLog(shader, length, nil, gl.Str(info))
		gl.DeleteShader(shader)
		return 0, fmt.Errorf("shader compile: %s", info)
	}
	return shader, nil
}
func createProgram() (uint32, error) {
	vertex, err := compileShader(gl.VERTEX_SHADER, vertexShader)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(vertex)
	fragment, err := compileShader(gl.FRAGMENT_SHADER, fragmentShader)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(fragment)
	program := gl.CreateProgram()
	gl.AttachShader(program, vertex)
	gl.AttachShader(program, fragment)
	gl.BindAttribLocation(program, 0, gl.Str("position\x00"))
	gl.BindAttribLocation(program, 1, gl.Str("normal\x00"))
	gl.LinkProgram(program)
	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		gl.DeleteProgram(program)
		return 0, fmt.Errorf("shader program link failed")
	}
	return program, nil
}

const vertexShader = `#version 150
in vec3 position;
in vec3 normal;
uniform mat3 rotation;
uniform vec2 scale;
uniform vec2 pan;
out vec3 surfaceNormal;
void main() {
 vec3 p=rotation*position;
 gl_Position=vec4(p.xy*scale+pan,-p.z/32.0,1.0);
 surfaceNormal=rotation*normal;
}`
const fragmentShader = `#version 150
in vec3 surfaceNormal;
uniform bool wire;
out vec4 outputColor;
void main() {
 vec3 n=normalize(surfaceNormal);
 if(n.z<0.0) n=-n;
 float shade=0.3+0.7*max(0.0,dot(n,normalize(vec3(-0.4,0.6,1.0))));
 outputColor=wire ? vec4(vec3(235.0/255.0),1.0) : vec4(vec3(shade),1.0);
}`
