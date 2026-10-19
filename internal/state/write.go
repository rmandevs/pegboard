package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteAtomic escribe en un temporal de la misma carpeta y lo renombra: el
// archivo nunca queda a medias. Conserva los permisos si ya existía.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".pegboard-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // tras el rename ya no existe
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Backup copia path a dir/<fecha>/<ruta aplanada> antes de modificarlo y
// devuelve dónde quedó ("" si el archivo no existía).
func Backup(dir string, now time.Time, path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	flat := strings.NewReplacer(string(filepath.Separator), "%", ":", "%").Replace(filepath.Clean(path))
	dst := filepath.Join(dir, now.UTC().Format("20060102T150405Z"), strings.TrimLeft(flat, "%"))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	// Los backups pueden tener secretos (los del usuario): solo para él.
	return dst, os.WriteFile(dst, data, 0o600)
}
