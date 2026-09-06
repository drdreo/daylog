// Package durable implements private atomic files and process-death-safe OS locks.
package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gofrs/flock"
	"io"
	"os"
	"path/filepath"
)

func Decode(b []byte, v any) error {
	tokens := json.NewDecoder(bytes.NewReader(b))
	if err := uniqueKeys(tokens, 0); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON or prose")
	}
	return nil
}
func Read(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil {
		return err
	}
	if len(b) > 4*1024*1024 {
		return fmt.Errorf("%s exceeds JSON file limit", path)
	}
	if err := Decode(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
func JSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, append(b, '\n'))
}
func Write(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := Mkdir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = Private(tmp, false); err != nil {
		f.Close()
		return err
	}
	n, err := f.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = replace(tmp, path); err != nil {
		return err
	}
	return SyncDir(dir)
}
func Mkdir(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("not a directory: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if err := Mkdir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := Private(path, true); err != nil {
		return err
	}
	return SyncDir(parent)
}
func Lock(path string, wait bool) (func(), error) {
	if err := Mkdir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := Private(path, false); err != nil {
		return nil, err
	}
	l := flock.New(path)
	if wait {
		err = l.Lock()
	} else {
		var ok bool
		ok, err = l.TryLock()
		if err == nil && !ok {
			err = fmt.Errorf("lock busy: %s", path)
		}
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = l.Unlock(); _ = l.Close() }, nil
}
