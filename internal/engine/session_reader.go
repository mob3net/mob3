package engine

// readerRules is the author stylesheet used for the article view. The engine
// owns extraction and the stylesheet; these aliases keep the session call sites
// stable during the boundary migration.
var readerRules = ReaderRules

func extractReaderPage(root *Node, title string) (*Node, bool) {
	return ExtractReaderPage(root, title)
}
