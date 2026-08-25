package types

// FileFilter restricts a file dialog to files of a given kind.
type FileFilter struct {
	// Name is the human-readable label for this filter (e.g. "Images").
	Name string

	// Extensions lists the file extensions WITHOUT the leading dot
	// (e.g. {"png", "jpg"}). An empty list, or an entry "*", matches any file.
	Extensions []string
}

// FileDialogOptions configures a native file dialog. The zero value is valid:
// it shows a default dialog with no title, filename or type filtering.
type FileDialogOptions struct {
	// Title overrides the dialog's title.
	Title string

	// Directory is the initial directory the dialog displays, as a filesystem
	// path. Empty uses the platform default (usually the last-used directory).
	Directory string

	// Filename is the suggested file name. Used by SaveFile.
	Filename string

	// Filters limits the selectable file types. An empty list shows all files.
	Filters []FileFilter
}
