package types

import "errors"

var (
	// ErrDialogCancelled reports that the user dismissed a native chooser.
	ErrDialogCancelled  = errors.New("webview: dialog cancelled")
	ErrDialogMainThread = errors.New("webview: dialogs before Run require the main goroutine")
	ErrDialogReentrant  = errors.New("webview: dialog called on UI thread while another request is pending")
	// ErrDialogUnsupported reports that the native UI cannot host file dialogs.
	ErrDialogUnsupported = errors.New("webview: native dialogs unsupported")
)

// FileFilter restricts a file dialog to files of a given kind.
type FileFilter struct {
	// Name is the human-readable label for this filter (e.g. "Images").
	Name string

	// Extensions lists file extensions with or without a leading dot
	// (e.g. {"png", ".jpg"}). An empty list, or an entry "*", matches any file.
	Extensions []string

	// MIMETypes lists exact or wildcard MIME types (e.g. "image/png" or
	// "image/*").
	MIMETypes []string
}

// FileDialogOptions configures a native file dialog. The zero value is valid:
// it shows a default dialog with no title, filename or type filtering.
type FileDialogOptions struct {
	// Title overrides the dialog's title.
	Title string

	// Directory is the initial directory the dialog displays, as a filesystem
	// path. Empty uses the platform default (usually the last-used directory).
	Directory string

	// Filename is the suggested file name.
	Filename string

	// Filters limits the selectable file types. An empty list shows all files.
	Filters []FileFilter
}

// DialogKind identifies a process-owned native chooser.
type DialogKind int

const (
	DialogOpen DialogKind = iota
	DialogDirectory
	DialogSave
)
