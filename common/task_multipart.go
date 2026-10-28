package common

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
)

// ReadForm couples its text allowance to the file memory budget. Task inputs can
// contain large JSON text fields, so read those separately and let ReadForm own
// file spooling and FileHeader lifetimes with one shared file memory budget.
func readTaskMultipartForm(reader *multipart.Reader, fieldBytes, fileMemory int64) (_ *multipart.Form, err error) {
	form := &multipart.Form{
		Value: make(map[string][]string),
		File:  make(map[string][]*multipart.FileHeader),
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, form.RemoveAll())
		}
	}()
	for count := 0; ; count++ {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			return form, nil
		}
		if nextErr != nil {
			return nil, nextErr
		}
		// Preserve ReadForm's default part cap; task routing applies its stricter
		// field/file/part limits before the request reaches the adaptor.
		if count >= 1000 {
			return nil, multipart.ErrMessageTooLarge
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if part.FileName() == "" {
			value, readErr := io.ReadAll(io.LimitReader(part, fieldBytes+1))
			if readErr != nil {
				return nil, readErr
			}
			fieldBytes -= int64(len(value))
			if fieldBytes < 0 {
				return nil, ErrRequestBodyTooLarge
			}
			form.Value[name] = append(form.Value[name], string(value))
			continue
		}
		fileForm, readErr := readMultipartFile(part, fileMemory)
		if readErr != nil {
			return nil, readErr
		}
		for field, headers := range fileForm.File {
			form.File[field] = append(form.File[field], headers...)
			for _, header := range headers {
				fileMemory -= min(header.Size, fileMemory)
			}
		}
	}
}

func readMultipartFile(part *multipart.Part, fileMemory int64) (*multipart.Form, error) {
	// Frame just this file without buffering its payload. ReadForm then creates
	// its normal FileHeader, including temporary storage removed by RemoveAll.
	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	if _, err := writer.CreatePart(part.Header); err != nil {
		return nil, err
	}
	headerSize := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, err
	}
	encoded := framing.Bytes()
	body := io.MultiReader(bytes.NewReader(encoded[:headerSize]), part, bytes.NewReader(encoded[headerSize:]))
	return multipart.NewReader(body, writer.Boundary()).ReadForm(fileMemory)
}
