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

Existing text annotations remain editable through their selection properties.
Pending creation must not steal focus from a newer draft or override a later
tool choice. Tests cover insertion, cancellation, repeated clicks, styles, zoom,
PDF coordinates, clipboard precedence and Save waiting for preparation.

## Highlight

Highlight starts on a word and follows text through the release position,
including multiple lines. Words and spaces are merged into a band per line,
following that line's orientation. A background drag creates no annotation.
The only property control is color; opacity is fixed at 30%, with no stroke-width
control. One completed gesture creates one editable vector object and one Undo.
Escape or switching tools cancels the gesture.
Completed highlights remain selectable, movable and recolorable, with no resize
handles. Resize gestures are ignored so bands keep their text-derived dimensions.

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
