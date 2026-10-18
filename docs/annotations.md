# Drawing, highlighting and shapes

## Text placement

Select Text, click the insertion point on a page, then type into the focused
inline editor. The property bar contains font, size, styles and color; it has no
text-entry field for new annotations. Property changes update the draft without
committing it when the entry loses focus. The editor scales with zoom while
retaining physical page coordinates. The editor has a transparent background,
a thin outline and a placeholder visible immediately after the click. Padding belongs to the outline
rather than the text input, so it does not shift the insertion point. A live
preview uses the same glyph outlines, decorations and color as the final
vector object. The click anchors the glyph origin horizontally and visible ink's
top vertically, excluding artwork padding, with a small upward pointer alignment
adjustment of 9 page units (half the nominal height of size-18 text), so the
visible displacement follows view zoom. Preview and confirmation share the
same page-edge clamp, so the text does not jump on confirmation. The keyboard
entry uses the exact bundled font face and baseline; outline/raster work runs in
the background with at most one preview job per draft and a bounded raster size.

Enter confirms the text. Another page click confirms it and opens a fresh empty
editor at the new point. Changing tools/pages, Save, Close or Undo also finish the
draft before proceeding. Escape cancels; empty/whitespace-only drafts create no
object. Each successful placement creates one vector object and one Undo step,
independent of keystrokes or style changes. Newline/tab pastes are normalized to
one line, with a 2000-character limit. Failed creation retains the input for
correction and stops deferred Save/Close actions.

Existing text annotations use the same page editor. A single click selects an
object for movement and properties; double-click its text, or use **Edit text on
page** in the property bar, to edit its contents inline. The bar has no text-entry
field, including after reopening a PDF. Font/style/color changes stay in the
inline draft until confirmation. Enter or a page/tool change replaces the same
object in one Undo step; Escape restores the original, and confirming an empty
value deletes it. Save waits for the final edit; preparation failures retain the
draft and stop deferred save/close actions. Transforms and the visible ink anchor are retained instead of applying the
new-text insertion offset. Text regenerates at its font size with natural physical
width/height; a PDF FreeText layout box must never stretch glyphs on either axis.
The same geometry is used by inline preview, confirmation and property changes.
Pending creation must not steal focus from a newer draft or override a later
tool choice. Tests cover insertion, cancellation, repeated clicks, styles, zoom,
PDF coordinates, clipboard precedence and Save waiting for preparation.

## Highlight

Highlight starts on a word and follows text through the release position,
including multiple lines. Words and spaces are merged into a band per line,
following that line's orientation. A background drag creates no annotation.
The only property control is color. Full color uses Multiply blending to preserve
dark text, with no opacity or stroke-width control. Imported opacity is retained. One completed gesture creates one editable vector object and one Undo.
Escape or switching tools cancels the gesture.
Completed highlights remain selectable and recolorable, anchored to their text.
Move and resize gestures are ignored, and resize handles stay hidden, so bands
keep their text-derived positions and dimensions.

Geometry uses native PDF text first, then the configured OCR engine for image
content. Existing text-selection recognition is reused when available. Loading
runs in the background, checks the document content key before publication, and
can finish a released gesture once geometry is ready. Without native or recognized
text, no highlight is created. Recognition failures go to diagnostic logs.

Highlight quads retain physical page coordinates independently of zoom and are
deep-copied/scaled with the object. Legacy freehand highlight geometry remains
readable, rendered at the same fixed opacity.

## Draw

Draw keeps freehand gestures in every mode. The style button cycles through:

- Filled circle: round tip, retaining the original freehand drawing behavior.
- Dashed and dotted: the existing patterned strokes.

Width and opacity are independent properties; opacity ranges from 5% to 100%.
Live previews and committed artwork apply opacity once within each
gesture, including retraced areas. Separate objects composite normally.
The flat marker mode has been removed. Legacy Drawing `solid` values regenerate
as round strokes; shapes retain their independent solid-line style. Freehand
geometry is a single nonzero-filled path of simple rounded segment outlines,
without resolving intersections through `Path.Settle`. This preserves uniform
opacity and vector export without the intersection solver's degeneracy failures.

Both tools export vector artwork to PDF without rasterizing source pages or
removing searchable text. Tests cover multiline selection, cancellation,
recognition after mouse release, Undo/Redo, vertical retraces, opacity, and PDF
placement/export at all four page rotations.

## Shapes

The Shapes tool includes a straight Line and a simple Arrow, alongside Rectangle,
Oval, Callout, Star and Magnifier. Line and Arrow always use a stroked path, with
no fill/outline toggle. Arrows have a single shaft and an open chevron head whose
length is fixed at 12 physical units (PDF points or original image pixels), with
a total span of 12 units. Shaft length never changes the head size.

Resizing regenerates each shape from control points at its new physical size;
it preserves stroke width, padding and arrowhead size. Free resizing changes
both axes independently; Ctrl retains proportions, and Magnifier stays circular.
Rotated/flipped objects keep their transforms. Shape preparation runs in the
annotation worker, coalesces pointer updates, and commits the last geometry as
one Undo step. Save and Undo wait for the final artwork; Escape discards pending
results. Model APIs also regenerate shapes for direct or transactional resizing.

A newly inserted Magnifier is selected immediately while Shapes stays active, so
its zoom slider edits that lens without requiring a move. Beginning another
shape clears that selection. Property edits preserve the lens frame and one Undo
step. Lens previews retain an active slider preview until its edit completes.
PDF content is rendered at a magnification-dependent sampling resolution before
cropping into the lens, including vector annotation appearances. Sampling is
bounded to 600 DPI, 16 million source pixels and an 8192-pixel long side. Preview
output accounts for screen density; image sources retain their original detail.

## Saving editable PDF objects

PDF Save/Save As writes added text, drawing, highlights and shapes as standard
annotations, so they can be selected and edited after reopening. Text Contents
and styling accompany vector appearances and embedded fonts. No private editor
payload is stored. Signatures reopen unselectable; magnifiers reopen as static
artwork, and stars/callouts as generic polygons. Unsupported foreign annotations
remain in the source PDF. Applied redactions bake and sanitize all markup instead.
See [Standard editable PDF annotations](pdf-annotations.md) for representations,
compatibility, implementation and verification. Image exports remain flattened.

Imported Preview arrows with a closed head retain that ending. New arrows still
use the simple open head. Both variants regenerate at resize with a fixed-size
head. A standard Line annotation with missing/empty appearance can regenerate
from its standard endpoints and style. A simple, single stroked vector path
stored as a Stamp can import as Drawing without using Apple's private data;
its original appearance stays cached until its properties change. Filled artwork,
images, text, cropped/multipath strokes and unsupported operators stay locked.
This distinction keeps signature outlines out of drawing editing.
