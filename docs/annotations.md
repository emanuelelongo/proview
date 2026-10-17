# Drawing, highlighting and shapes

## Highlight

Highlight starts on a word and follows text through the release position,
including multiple lines. Words and spaces are merged into a band per line,
following that line's orientation. A background drag creates no annotation.
The only property control is color; opacity is fixed at 30%, with no stroke-width
control. One completed gesture creates one editable vector object and one Undo.
Escape or switching tools cancels the gesture.

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
- Solid line: flat rectangular marker nib, fixed at 45 degrees, with a narrow
  dimension one fifth of the selected width. Apparent thickness varies with
  movement direction; this mode does not constrain the gesture to a segment.
- Dashed and dotted: the existing patterned strokes.

Width and opacity are independent properties; opacity ranges from 5% to 100%.
Live round/flat previews and committed artwork apply opacity once within each
gesture, including retraced areas. Separate objects composite normally.
Flat-tip outlines sweep the nib between consecutive pointer samples.

Both tools export vector artwork to PDF without rasterizing source pages or
removing searchable text. Tests cover multiline selection, cancellation,
recognition after mouse release, Undo/Redo, nib geometry, opacity, and PDF
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
