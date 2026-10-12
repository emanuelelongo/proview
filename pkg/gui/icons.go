package gui

import (
	"embed"
	"fmt"
	vector "github.com/tdewolff/canvas"
	"image"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed icons/tabler/*.svg icons/tabler/LICENSE
var tablerAssets embed.FS
var tablerCache sync.Map

func tablerIcon(name string) fyne.Resource {
	if resource, ok := tablerCache.Load(name); ok {
		return resource.(fyne.Resource)
	}
	data, err := tablerAssets.ReadFile("icons/tabler/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	// Convert strokes to filled outlines because Fyne themes recolor fills only.
	data = []byte(strings.ReplaceAll(string(data), "currentColor", "#000000"))
	parsed, err := vector.ParseSVG(strings.NewReader(string(data)))
	if err != nil {
		panic(err)
	}
	outlines := &iconOutlines{}
	parsed.RenderTo(outlines)
	data = []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="#000000" d="%s"/></svg>`, outlines.path.ToSVG()))
	resource := theme.NewThemedResource(fyne.NewStaticResource("tabler-"+name+".svg", data))
	tablerCache.Store(name, resource)
	return resource
}

func (t largerIconTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	names := map[fyne.ThemeIconName]string{
		theme.IconNameDocument: "file", theme.IconNameDocumentCreate: "file-plus",
		theme.IconNameDocumentSave: "device-floppy", theme.IconNameFile: "file",
		theme.IconNameFileText: "file", theme.IconNameFileImage: "photo",
		theme.IconNameFileApplication: "file", theme.IconNameFileAudio: "file", theme.IconNameFileVideo: "file",
		theme.IconNameDownload: "download", theme.IconNameUpload: "arrow-up",
		theme.IconNameError: "alert-triangle", theme.IconNameBrokenImage: "photo",
		theme.IconNameCheckButton: "square", theme.IconNameCheckButtonChecked: "square-check",
		theme.IconNameFolder: "folder", theme.IconNameFolderOpen: "folder-open",
		theme.IconNameContentCut: "crop", theme.IconNameViewZoomIn: "zoom-in", theme.IconNameViewZoomOut: "zoom-out",
		theme.IconNameCancel: "x", theme.IconNameConfirm: "check", theme.IconNameDelete: "trash",
		theme.IconNameContentAdd: "plus", theme.IconNameContentRemove: "minus",
		theme.IconNameContentUndo: "arrow-back-up", theme.IconNameContentRedo: "arrow-forward-up",
		theme.IconNameContentCopy: "copy", theme.IconNameContentPaste: "clipboard", theme.IconNameContentClear: "eraser",
		theme.IconNameColorPalette: "palette", theme.IconNameSearch: "search", theme.IconNameSettings: "settings",
		theme.IconNameInfo: "info-circle", theme.IconNameWarning: "alert-triangle", theme.IconNameHelp: "question-mark",
		theme.IconNameMenu: "menu", theme.IconNameMoreHorizontal: "dots", theme.IconNameViewRefresh: "refresh",
		theme.IconNameNavigateBack: "chevron-left", theme.IconNameNavigateNext: "chevron-right",
		theme.IconNameArrowDropDown: "chevron-down", theme.IconNameArrowDropUp: "chevron-up",
		theme.IconNameMenuExpand: "chevron-right", theme.IconNameWindowClose: "x",
		theme.IconNameMoveUp: "arrow-up", theme.IconNameMoveDown: "arrow-down",
	}
	if icon := names[name]; icon != "" {
		return tablerIcon(icon)
	}
	return t.Theme.Icon(name)
}

// Tabler outline assets contain only geometry. Keep their original curves and
// rounded caps while adapting their paint to Fyne's light/dark icon resources.
type iconOutlines struct{ path *vector.Path }

func (*iconOutlines) Size() (float64, float64) { return 24, 24 }
func (r *iconOutlines) RenderPath(path *vector.Path, style vector.Style, m vector.Matrix) {
	if style.HasFill() {
		r.path = r.path.Append(path.Copy().Transform(m).Transform(vector.Identity.ReflectYAbout(12)))
	}
	if style.HasStroke() {
		r.path = r.path.Append(path.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, .01).Transform(m).Transform(vector.Identity.ReflectYAbout(12)))
	}
}
func (*iconOutlines) RenderText(*vector.Text, vector.Matrix) { panic("unexpected text in Tabler icon") }
func (*iconOutlines) RenderImage(image.Image, vector.Matrix) {
	panic("unexpected image in Tabler icon")
}
