package api

// FolderLabelsForTest lets the external tests reach folderLabels.
func FolderLabelsForTest(paths []string) []string { return folderLabels(paths) }
