package books

import (
	"io/fs"
	"os"
	"path/filepath"
)

// linkedDir is a directory entry for a link to a folder, which reports itself
// as the folder it points to.
type linkedDir struct{ fs.DirEntry }

func (linkedDir) IsDir() bool       { return true }
func (linkedDir) Type() fs.FileMode { return fs.ModeDir }

// walkFollowing is filepath.WalkDir that also goes into links to folders. A
// real folder is entered once, so a link back up the tree is not a loop.
// Paths are kept as they are under root, through the links. fn is called like
// WalkDir's: for each folder before its contents, and for each file.
func walkFollowing(root string, fn fs.WalkDirFunc) error {
	visited := map[string]bool{}
	var walk func(path string, d fs.DirEntry) error
	walk = func(path string, d fs.DirEntry) error {
		if err := fn(path, d, nil); err != nil {
			if err == filepath.SkipDir {
				return nil
			}
			return err
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			if visited[real] {
				return nil
			}
			visited[real] = true
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			if err := fn(path, d, err); err != nil && err != filepath.SkipDir {
				return err
			}
			return nil
		}
		for _, e := range entries {
			child := filepath.Join(path, e.Name())
			if e.Type()&fs.ModeSymlink != 0 {
				info, err := os.Stat(child)
				if err != nil {
					continue // a broken link
				}
				if info.IsDir() {
					e = linkedDir{e}
				}
			}
			if !e.IsDir() {
				if err := fn(child, e, nil); err != nil && err != filepath.SkipDir {
					return err
				}
				continue
			}
			if err := walk(child, e); err != nil {
				return err
			}
		}
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return fn(root, nil, err)
	}
	return walk(root, fs.FileInfoToDirEntry(info))
}
