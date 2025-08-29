package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// MarshalToFile is a utility function for marshalling a data structore to JSON
// and write it to a fil. The JSON will be pretty printed.
func MarshalFile(path string, o any) (outErr error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		err := f.Close()
		if err != nil {
			outErr = errors.Join(outErr, fmt.Errorf(
				"failed to close file: %w", err))
		}
	}()

	dec := json.NewEncoder(f)
	dec.SetIndent("", "  ")

	err = dec.Encode(o)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return nil
}

// Close a resource and joins the error to the outError if the close fails. Will
// ignore os.ErrClosed so it's safe to use together with "manual" closing of
// files.
func Close(name string, c io.Closer, outErr *error) {
	err := c.Close()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		*outErr = errors.Join(*outErr, fmt.Errorf("close %s: %w", name, err))
	}
}
