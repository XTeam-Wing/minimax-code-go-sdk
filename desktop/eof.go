package desktop

import "io"

// errEOF is io.EOF without importing it from multiple files' init cycles.
var errEOF = io.EOF
