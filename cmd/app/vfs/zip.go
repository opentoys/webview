package vfs

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"time"
)

func ZipReadFile(name string) (r *zip.Reader, e error) {
	buf, e := os.ReadFile(name)
	if e != nil {
		return
	}
	return zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
}

type ZipFS struct {
	data *zip.Reader
}

func NewZip(name string) (fs fs.FS, e error) {
	data, e := ZipReadFile(name)
	if e != nil {
		return
	}
	return &ZipFS{data: data}, nil
}

func (s *ZipFS) Open(file string) (f fs.File, e error) {
	if file == "." {
		return &fsDirFile{fstat: &fstat{name: "vfs", mode: fs.ModeDir | 0555, idir: true}}, nil
	}
	return s.data.Open(file)
}

func (s *ZipFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	return fs.ReadDir(s, dir)
}

type fsDirEntry struct {
	stat *fstat
}

func (d *fsDirEntry) Name() string               { return d.stat.name }
func (d *fsDirEntry) IsDir() bool                { return d.stat.idir }
func (d *fsDirEntry) Type() fs.FileMode          { return d.stat.mode.Type() }
func (d *fsDirEntry) Info() (fs.FileInfo, error) { return d.stat, nil }

type fsDirFile struct {
	*fstat
}

func (d *fsDirFile) Stat() (fs.FileInfo, error) { return d.fstat, nil }
func (d *fsDirFile) Read([]byte) (int, error)   { return 0, nil }
func (d *fsDirFile) Close() error               { return nil }

type fstat struct {
	name string
	size int64
	mode fs.FileMode
	mod  time.Time
	idir bool
	sys  any
}

func (s *fstat) Name() string       { return s.name }
func (s *fstat) Size() int64        { return s.size }
func (s *fstat) Mode() fs.FileMode  { return s.mode }
func (s *fstat) ModTime() time.Time { return s.mod }
func (s *fstat) IsDir() bool        { return s.idir }
func (s *fstat) Sys() any           { return s.sys }
