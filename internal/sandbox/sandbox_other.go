//go:build !linux

package sandbox

import "runtime"

const platform = runtime.GOOS

func apply(Config) (Status, error) {
	st := Status{Platform: platform}
	st.note("no in-process sandbox on %s; use Linux for production", platform)
	return st, nil
}
