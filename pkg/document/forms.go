package document

import (
	"errors"
	"fmt"
	"image"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"emanuelelongo/proview/internal/pdfengine"
)

type formKey struct {
	source *Source
	id     int
}
type FormField struct {
	ID                                 int
	Name, Value, OnValue               string
	Kind, Flags, MaxLen                int
	Rect                               NormalizedRect
	Options, ExportOptions             []string
	Values, DefaultValues, ResetFields []string
	Caption                            string
	ResetExclude                       bool
}

const (
	FormReset    = pdfengine.FormReset
	FormText     = pdfengine.FormText
	FormCheckbox = pdfengine.FormCheckbox
	FormRadio    = pdfengine.FormRadio
	FormCombo    = pdfengine.FormCombo
	FormList     = pdfengine.FormList
)

func (s *Source) formFields() ([]pdfengine.FormField, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.formsLoaded {
		s.forms, s.formsErr = pdfengine.ReadForms(s.original)
		s.formsLoaded = true
	}
	return s.forms, s.formsErr
}

func (d *Document) FormFields(page int) ([]FormField, error) {
	if d.Format() != FormatPDF {
		return nil, nil
	}
	source, index, err := d.pageSource(page)
	if err != nil {
		return nil, err
	}
	fields, err := source.formFields()
	if err != nil {
		return nil, err
	}
	var result []FormField
	for _, f := range fields {
		if f.Page != index {
			continue
		}
		rect := NormalizedRect{f.X, f.Y, f.Width, f.Height}
		for range d.pages[page].turns {
			rect = transformRect(rect, "right")
		}
		values := normalizedFormValues(f.Kind, f.Flags, f.Values)
		if v, ok := d.formValues[formKey{source, f.ID}]; ok {
			values = slices.Clone(v)
		}
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		result = append(result, FormField{ID: f.ID, Name: f.Name, Value: value, OnValue: f.OnValue, Kind: f.Kind, Flags: f.Flags, MaxLen: f.MaxLen, Rect: rect, Options: slices.Clone(f.Options), ExportOptions: slices.Clone(f.ExportOptions), Values: values, DefaultValues: slices.Clone(f.DefaultValues), ResetFields: slices.Clone(f.ResetFields), ResetExclude: f.ResetExclude, Caption: f.Caption})
	}
	return result, nil
}

func (d *Document) HasFormFields() bool {
	if d.Format() != FormatPDF {
		return false
	}
	for _, ref := range d.pages {
		fields, err := ref.source.formFields()
		if err != nil {
			continue
		}
		for _, f := range fields {
			if f.Page == ref.index {
				return true
			}
		}
	}
	return false
}

const (
	FormRequired       = 1 << 1
	FormMultiline      = 1 << 12
	FormPassword       = 1 << 13
	FormEditableChoice = 1 << 18
	FormMultiSelect    = 1 << 21
)

func normalizedFormValues(kind, flags int, values []string) []string {
	if kind == FormList && flags&FormMultiSelect != 0 {
		return slices.Clone(values)
	}
	if len(values) > 0 {
		return []string{values[0]}
	}
	if kind == FormCheckbox || kind == FormRadio {
		return []string{"Off"}
	}
	return []string{""}
}

func (d *Document) SetFormValue(page, id int, value string) error {
	return d.SetFormValues(page, id, []string{value})
}

// ValidateFormField checks declarative constraints without executing PDF scripts.
// Required is advisory while saving a draft; callers choose when to show it.
func ValidateFormField(field FormField, values []string, required bool) error {
	if field.Kind == FormReset {
		return nil
	}
	if required && field.Flags&FormRequired != 0 {
		empty := len(values) == 0
		if len(values) == 1 {
			empty = strings.TrimSpace(values[0]) == "" || (field.Kind == FormCheckbox || field.Kind == FormRadio) && values[0] == "Off"
		}
		if empty {
			return errors.New("Required field")
		}
	}
	multi := field.Kind == FormList && field.Flags&FormMultiSelect != 0
	if !multi && len(values) > 1 {
		return errors.New("This field accepts only one value")
	}
	for _, value := range values {
		if strings.IndexByte(value, 0) >= 0 {
			return errors.New("Field value contains an invalid character")
		}
		if field.MaxLen > 0 && utf8.RuneCountInString(value) > field.MaxLen {
			return fmt.Errorf("Maximum %d characters", field.MaxLen)
		}
		if field.Kind == FormText && field.Flags&FormMultiline == 0 && strings.ContainsAny(value, "\r\n") {
			return errors.New("This field accepts a single line")
		}
		if (field.Kind == FormList || field.Kind == FormCombo) && field.Flags&FormEditableChoice == 0 && value != "" && !slices.Contains(field.ExportOptions, value) {
			return errors.New("Invalid choice")
		}
	}
	return nil
}

func (d *Document) SetFormValues(page, id int, values []string) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	fields, err := d.FormFields(page)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(fields, func(f FormField) bool { return f.ID == id })
	if i < 0 || fields[i].Kind == FormReset {
		return fmt.Errorf("document: editable field not found")
	}
	field := fields[i]
	if err := ValidateFormField(field, values, false); err != nil {
		return err
	}
	if field.Kind == FormCheckbox || field.Kind == FormRadio {
		valid := len(values) == 0 || values[0] == "Off"
		for _, f := range fields {
			if f.Name == field.Name && len(values) == 1 && f.OnValue == values[0] {
				valid = true
			}
		}
		if !valid {
			return errors.New("document: invalid button value")
		}
	}
	values = normalizedFormValues(field.Kind, field.Flags, values)
	if field.Kind == FormList && field.Flags&FormMultiSelect != 0 {
		// Canonical PDF choice order avoids duplicate values and spurious undo edits.
		ordered := []string(nil)
		for _, option := range field.ExportOptions {
			if slices.Contains(values, option) {
				ordered = append(ordered, option)
			}
		}
		values = ordered
	}
	if slices.Equal(field.Values, values) {
		return nil
	}
	source, _, _ := d.pageSource(page)
	before := d.snapshot()
	d.storeFormValues(source, field.Name, id, values)
	d.record(before)
	return nil
}

func (d *Document) storeFormValues(source *Source, name string, id int, values []string) {
	if d.formValues == nil {
		d.formValues = map[formKey][]string{}
	}
	original, _ := source.formFields()
	for _, f := range original {
		if f.ID != id && (name == "" || f.Name != name) {
			continue
		}
		key := formKey{source, f.ID}
		initial := normalizedFormValues(f.Kind, f.Flags, f.Values)
		if slices.Equal(values, initial) {
			delete(d.formValues, key)
		} else {
			d.formValues[key] = slices.Clone(values)
		}
	}
}

// ResetForm implements a standalone ResetForm action as one reversible edit.
// Targets are names (possibly nonterminal groups) resolved by the PDF bridge.
func (d *Document) ResetForm(page, id int) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	fields, err := d.FormFields(page)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(fields, func(f FormField) bool { return f.ID == id && f.Kind == FormReset })
	if index < 0 {
		return errors.New("document: reset button not found")
	}
	button := fields[index]
	source, _, _ := d.pageSource(page)
	before := d.snapshot()
	seen := map[int]bool{}
	for page, ref := range d.pages {
		if ref.source != source {
			continue
		}
		candidates, err := d.FormFields(page)
		if err != nil {
			d.restore(before)
			return err
		}
		for _, f := range candidates {
			if f.Kind == FormReset || seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			targeted := len(button.ResetFields) == 0
			for _, name := range button.ResetFields {
				if f.Name == name || strings.HasPrefix(f.Name, name+".") {
					targeted = true
				}
			}
			if len(button.ResetFields) > 0 && button.ResetExclude {
				targeted = !targeted
			}
			if targeted {
				d.storeFormValues(source, f.Name, f.ID, normalizedFormValues(f.Kind, f.Flags, f.DefaultValues))
			}
		}
	}
	d.record(before)
	return nil
}

// FormValidationIssues reports each invalid visible logical field once. Saving
// an incomplete form is permitted: Required applies to submission, not drafts.
func (d *Document) FormValidationIssues() []string {
	var issues []string
	type logicalField struct {
		source *Source
		name   string
		id     int
	}
	seen := map[logicalField]bool{}
	if d.Format() != FormatPDF {
		return nil
	}
	for page, ref := range d.pages {
		fields, err := d.FormFields(page)
		if err != nil {
			continue
		}
		for _, f := range fields {
			key := logicalField{source: ref.source, name: f.Name}
			if f.Name == "" {
				key.id = f.ID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := ValidateFormField(f, f.Values, true); err != nil {
				issues = append(issues, fmt.Sprintf("Page %d, %s: %v", page+1, f.Name, err))
			}
		}
	}
	return issues
}

func (d *Document) sourcePDF(source *Source) ([]byte, error) {
	values := map[int][]string{}
	for key, value := range d.formValues {
		if key.source == source {
			values[key.id] = value
		}
	}
	return pdfengine.FillFormValues(source.original, values)
}

func (d *Document) formPreview(source *Source, index int, dpi float64) (image.Image, bool, error) {
	changed := false
	for key := range d.formValues {
		if key.source == source {
			changed = true
			break
		}
	}
	if !changed {
		return nil, false, nil
	}
	fields, err := source.formFields()
	if err != nil || len(fields) == 0 {
		return nil, false, nil
	}
	data, err := d.sourcePDF(source)
	if err != nil {
		return nil, true, err
	}
	preview, err := pdfengine.RenderFormPage(data, index, dpi)
	return preview, true, err
}

func cloneFormValues(values map[formKey][]string) map[formKey][]string {
	copy := maps.Clone(values)
	for key, value := range copy {
		copy[key] = slices.Clone(value)
	}
	return copy
}
