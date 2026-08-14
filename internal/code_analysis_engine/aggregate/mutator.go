package aggregate

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"path"
)

// GraftFileNode traverses or creates the physical directory hierarchy and grafts a normalized GAST root onto the target folder.
func GraftFileNode(root *DirectoryNode, relPath string, gastRoot *normalize.GASTNode, imports []string, lang string) {
	if root == nil || relPath == "" {
		return
	}

	dirs, fileName := SplitPathToDirectories(relPath)
	curr := root

	currentPath := ""
	for _, d := range dirs {
		if currentPath == "" {
			currentPath = d
		} else {
			currentPath = path.Join(currentPath, d)
		}

		curr.mu.Lock()
		child, exists := curr.SubFolders[d]
		if !exists {
			child = NewDirectoryNode(d, currentPath)
			curr.SubFolders[d] = child
		}
		curr.mu.Unlock()
		curr = child
	}

	fileNode := &FileBoundaryNode{
		FileName:     fileName,
		RelativePath: NormalizeRelativePath(relPath),
		Language:     lang,
		GASTRoot:     gastRoot,
		LocalImports: imports,
	}

	curr.mu.Lock()
	curr.Files[fileName] = fileNode
	curr.mu.Unlock()
}

// PruneFileNode removes a file from the directory tree, and recursively deletes empty parent folders.
func PruneFileNode(root *DirectoryNode, relPath string) bool {
	if root == nil || relPath == "" {
		return false
	}

	dirs, fileName := SplitPathToDirectories(relPath)
	return pruneRecursive(root, dirs, fileName)
}

func pruneRecursive(curr *DirectoryNode, dirs []string, fileName string) bool {
	if curr == nil {
		return false
	}

	if len(dirs) == 0 {
		// Target folder reached
		curr.mu.Lock()
		if _, exists := curr.Files[fileName]; exists {
			delete(curr.Files, fileName)
			curr.mu.Unlock()
			return true
		}
		curr.mu.Unlock()
		return false
	}

	nextDir := dirs[0]
	curr.mu.Lock()
	child, exists := curr.SubFolders[nextDir]
	curr.mu.Unlock()

	if !exists {
		return false
	}

	removed := pruneRecursive(child, dirs[1:], fileName)
	if removed {
		// Prune empty subfolder if it has no files and no subfolders left
		child.mu.RLock()
		empty := len(child.Files) == 0 && len(child.SubFolders) == 0
		child.mu.RUnlock()

		if empty {
			curr.mu.Lock()
			delete(curr.SubFolders, nextDir)
			curr.mu.Unlock()
		}
	}

	return removed
}

// pruneEmptyDirectories recursively removes empty DirectoryNodes from the hierarchy.
func pruneEmptyDirectories(dir *DirectoryNode) bool {
	if dir == nil {
		return true
	}
	dir.mu.RLock()
	names := make([]string, 0, len(dir.SubFolders))
	for name := range dir.SubFolders {
		names = append(names, name)
	}
	dir.mu.RUnlock()

	for _, name := range names {
		dir.mu.RLock()
		sub := dir.SubFolders[name]
		dir.mu.RUnlock()

		if pruneEmptyDirectories(sub) {
			dir.mu.Lock()
			delete(dir.SubFolders, name)
			dir.mu.Unlock()
		}
	}

	dir.mu.RLock()
	empty := len(dir.Files) == 0 && len(dir.SubFolders) == 0
	dir.mu.RUnlock()

	return empty
}
