package signatures

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func exampleDrawing() Drawing {
	return Drawing{Version: 1, Width: 4, Smooth: 25, Strokes: [][]Point{{{100, 100}, {150, 145}, {200, 90}, {260, 150}}, {{300, 80}, {330, 150}}, {{350, 120}}}}
}

func TestDrawingSmoothIsReversibleAndPreservesStrokes(t *testing.T) {
	d := exampleDrawing()
	original := d.Clone()
	a, err := FromDrawing(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(a.Data, []byte(" C")) || bytes.Count(a.Data, []byte("<path")) != 2 || !bytes.Contains(a.Data, []byte("<circle")) {
		t.Fatalf("missing curves, separate strokes or dot: %s", a.Data)
	}
	raw, err := a.WithSettings(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw.Data, []byte(" C")) || !bytes.Contains(raw.Data, []byte(" L150.00000 145.00000")) {
		t.Fatal("zero smoothing did not restore original points")
	}
	again, err := raw.WithSettings(25, 4)
	if err != nil || !bytes.Equal(again.Data, a.Data) {
		t.Fatal("smoothing was not reversible")
	}
	if !reflect.DeepEqual(d, original) {
		t.Fatal("fitting mutated original points")
	}
	points := resample(d.Strokes[0], 1)
	curves := fitCurves(points, 1.25)
	if curves[0][0] != points[0] || curves[len(curves)-1][3] != points[len(points)-1] {
		t.Fatal("fitting moved stroke endpoints")
	}
	for _, curve := range curves {
		for _, p := range curve {
			if math.IsNaN(p.X+p.Y) || math.IsInf(p.X+p.Y, 0) {
				t.Fatal("fitting produced invalid coordinates")
			}
		}
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSVGImportTransparencyAndUnsupportedFeatures(t *testing.T) {
	a, err := Import("signature.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><g transform="translate(10,0)" fill="#ff0000" fill-opacity="0.5"><rect x="10" y="10" width="30" height="20"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := a.Render(100)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha := picture.At(30, 20).RGBA()
	if alpha < 30000 || alpha > 35000 {
		t.Fatalf("opacity = %d, want approximately 32768", alpha)
	}
	_, _, _, alpha = picture.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatal("signature background is not transparent")
	}
	for _, element := range []string{`<text>Hello</text>`, `<image href="file:///tmp/image.png"/>`, `<path d="M0 0L10 10" filter="url(#f)"/>`, `<foreignObject/>`, `<style>path{fill:red}</style>`} {
		if _, err := Import("bad.svg", []byte(`<svg viewBox="0 0 100 50">`+element+`</svg>`)); err == nil {
			t.Fatalf("accepted unsupported element %s", element)
		}
	}
	if _, err := Import("bad.svg", []byte(`<svg viewBox="0 0 0 0"/>`)); err == nil {
		t.Fatal("accepted empty SVG")
	}
}

func TestStorePersistenceRevisionDeletionAndCorruption(t *testing.T) {
	root := t.TempDir()
	store := &Store{Root: root}
	asset, err := FromDrawing(exampleDrawing())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.Save(Entry{Asset: asset})
	if err != nil {
		t.Fatal(err)
	}
	copy := entry.Asset.Clone()
	entry.Asset, err = entry.Asset.WithSettings(80, 6)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = store.Save(entry)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &Store{Root: root}
	entries, err := reopened.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %d, error %v", len(entries), err)
	}
	if entries[0].Asset.Drawing.Smooth != 80 {
		t.Fatal("settings did not survive reopening")
	}
	if copy.Drawing.Smooth != 25 {
		t.Fatal("archive edit changed an independent copy")
	}
	revisions, _ := os.ReadDir(filepath.Join(root, entry.ID))
	count := 0
	for _, revision := range revisions {
		if revision.IsDir() {
			count++
			if _, err := os.Stat(filepath.Join(root, entry.ID, revision.Name(), "signature.svg")); err != nil {
				t.Fatal("SVG was not saved")
			}
		}
	}
	if count != 1 {
		t.Fatalf("retained %d revisions", count)
	}
	uncommitted, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, entry.ID, uncommitted), 0700); err != nil {
		t.Fatal(err)
	}
	entries, err = store.List()
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatal("interrupted revision changed the committed entry")
	}
	bad, err := store.Save(Entry{Asset: asset})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bad.ID, "current.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err = store.List()
	if err == nil || len(entries) != 1 {
		t.Fatal("corrupt signature prevented reading valid entries")
	}
	if err := store.Delete(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := copy.Render(180); err != nil {
		t.Fatal("deleting archive entry invalidated its independent copy")
	}
	if err := store.Delete("../../escape"); err == nil {
		t.Fatal("accepted invalid ID")
	}
	if _, err := store.Save(Entry{Asset: Asset{}}); err == nil {
		t.Fatal("accepted empty asset")
	}
}

func TestDataDirectories(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")
	for _, test := range []struct{ os, env, want string }{
		{"darwin", "", filepath.Join(home, "Library", "Application Support", "Proview", "signatures")},
		{"linux", "", filepath.Join(home, ".local", "share", "proview", "signatures")},
		{"linux", "/data", filepath.Join("/data", "proview", "signatures")},
		{"windows", "", filepath.Join(home, "AppData", "Local", "Proview", "signatures")},
	} {
		got, err := DataDir(test.os, home, func(string) string { return test.env })
		if err != nil || got != test.want {
			t.Fatalf("%s: %q, %v; want %q", test.os, got, err, test.want)
		}
	}
}

func TestPNGArchivePreservesOriginalAndTransparency(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	picture.SetNRGBA(3, 2, color.NRGBA{R: 20, G: 30, B: 40, A: 128})
	var source bytes.Buffer
	if err := png.Encode(&source, picture); err != nil {
		t.Fatal(err)
	}
	data := source.Bytes()
	asset, err := Import("signature.PNG", data)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 0
	prepared, err := asset.Prepare()
	if err != nil {
		t.Fatal("import did not own its source bytes:", err)
	}
	if prepared.Asset.Kind != PNG || prepared.Asset.Drawing != nil || prepared.Preview.Bounds() != picture.Bounds() {
		t.Fatal("PNG format or dimensions changed")
	}
	if got := color.NRGBAModel.Convert(prepared.Preview.At(3, 2)).(color.NRGBA); got.A != 128 {
		t.Fatalf("PNG opacity = %d", got.A)
	}
	store := &Store{Root: t.TempDir()}
	if _, err := store.Save(Entry{Asset: asset}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 || !bytes.Equal(entries[0].Asset.Data, asset.Data) {
		t.Fatal("PNG original did not survive persistence")
	}
	if _, err := asset.WithSettings(50, 4); err == nil {
		t.Fatal("PNG incorrectly offered vector smoothing")
	}
}

func TestSVGViewBoxOffsetAndEvenOddHoles(t *testing.T) {
	asset, err := Import("offset.svg", []byte(`<svg viewBox="10,20,100,50"><rect x="10" y="20" width="20" height="20" fill="blue"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(100)
	if err != nil {
		t.Fatal(err)
	}
	_, _, blue, alpha := picture.At(5, 5).RGBA()
	if blue == 0 || alpha == 0 {
		t.Fatal("nonzero viewBox origin moved the signature")
	}
	asset, err = Import("hole.svg", []byte(`<svg viewBox="0 0 100 100"><path fill-rule="evenodd" d="M10 10L90 10L90 90L10 90Z M30 30L70 30L70 70L30 70Z"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	picture, err = asset.Render(100)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha = picture.At(50, 50).RGBA()
	if alpha != 0 {
		t.Fatal("evenodd hole was filled")
	}
	_, _, _, alpha = picture.At(20, 20).RGBA()
	if alpha == 0 {
		t.Fatal("evenodd outer shape was lost")
	}
	if _, err := Import("bad.svg", []byte(`<svg viewBox="0 0 100 50"><rect width="20" height="20" fill="unrecognized-color"/></svg>`)); err == nil {
		t.Fatal("unknown color was silently substituted")
	}
	asset, err = Import("viewport.svg", []byte(`<svg width="100" height="100" viewBox="0 0 100 50"><rect width="100" height="50" fill="red"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	picture, err = asset.Render(100)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, alpha = picture.At(50, 5).RGBA()
	if alpha != 0 {
		t.Fatal("SVG viewport did not preserve the aspect ratio")
	}
	_, _, _, alpha = picture.At(50, 50).RGBA()
	if alpha == 0 {
		t.Fatal("SVG viewport did not center the shape")
	}
}

func TestSVGNamedColorsAndInheritedStrokeWidth(t *testing.T) {
	asset, err := Import("colors.svg", []byte(`<svg viewBox="0 0 100 50"><rect width="40" height="50" fill="transparent"/><g color="rebeccapurple"><rect x="50" width="50" height="50" fill="currentColor"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := asset.Render(100)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(picture.At(20, 25)).(color.NRGBA); got.A != 0 {
		t.Fatal("transparent paint was replaced with an opaque color")
	}
	if got := color.NRGBAModel.Convert(picture.At(75, 25)).(color.NRGBA); got != (color.NRGBA{R: 102, G: 51, B: 153, A: 255}) {
		t.Fatalf("named currentColor = %v", got)
	}
	if _, err := Import("width.svg", []byte(`<svg viewBox="0 0 100 50"><g stroke-width="10%"><path d="M10 10 L90 40" stroke="black"/></g></svg>`)); err == nil {
		t.Fatal("unsupported inherited percentage stroke width was accepted")
	}
}

func BenchmarkSmoothTenThousandPoints(b *testing.B) {
	d := Drawing{Version: 1, Width: 4, Smooth: 25, Strokes: make([][]Point, 1)}
	for i := 0; i < 10_000; i++ {
		d.Strokes[0] = append(d.Strokes[0], Point{float64(i) / 10, 225 + 80*math.Sin(float64(i)/100)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		asset, err := FromDrawing(d)
		if err != nil || !strings.Contains(string(asset.Data), " C") {
			b.Fatal(err)
		}
	}
}
