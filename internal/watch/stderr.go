package watch

import (
	"io"
	"os"
)

var defaultStderr io.Writer = os.Stderr
