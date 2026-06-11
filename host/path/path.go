package path

import (
	"path/filepath"
)

func Basename(path string) string {
	return filepath.Base(path)
}

func Join(path ...string) string {
	return filepath.Join(path...)
}

func Resolve(path ...string) string {
	return filepath.Clean(filepath.Join(path...))
}

func Normalize(path string) string {
	return filepath.Clean(path)
}

func Dirname(path string) string {
	return filepath.Dir(path)
}

func Extname(path string) string {
	return filepath.Ext(path)
}
