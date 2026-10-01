package remote

import (
	"fmt"
	"strings"

	"github.com/Higangssh/homebutler/internal/util"
)

// Every string that goes into a remote command passes through one of these two,
// and the difference between them is the bug #303 was.
//
// v0.10.0 quoted every remote path against shell injection, which was right,
// and in doing so quoted "$HOME/.local/bin/homebutler" so that the remote
// shell never expanded it. scp was then told to write to a directory called
// $HOME, could not, and deploy failed on every account that could not write
// /usr/local/bin — with the reason thrown away. Quoting a path and quoting a
// piece of text are different jobs, so they are different functions, and a
// test in this package fails when util.ShellQuote is called around them.

// quotePath quotes a path for the remote shell. A leading ~ or $HOME is left
// for the remote shell to expand, since that is what someone writing it meant;
// any other $ is refused rather than quoted into a path that does not exist.
func quotePath(p string) (string, error) {
	for _, home := range []string{"$HOME", "${HOME}", "~"} {
		if p == home {
			return `"$HOME"`, nil
		}
		if rest, ok := strings.CutPrefix(p, home+"/"); ok {
			if strings.Contains(rest, "$") {
				break
			}
			return `"$HOME"/` + util.ShellQuote(rest), nil
		}
	}
	if strings.Contains(p, "$") {
		return "", fmt.Errorf("remote path %q uses a variable; only a leading ~ or $HOME is expanded", p)
	}
	return util.ShellQuote(p), nil
}

// quoteLiteral quotes text that must reach the remote side exactly as written,
// $ included: the line deploy appends to a shell rc file is
// `export PATH="$PATH:/home/me/.local/bin"`, and $PATH there is for the rc
// file to expand later, not for the command writing it.
func quoteLiteral(s string) string {
	return util.ShellQuote(s)
}
