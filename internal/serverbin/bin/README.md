Compiled copies of `cmd/website-server` go here, one per platform, as
`website-server-<os>-<arch>.gz`. They're produced by `go run ./tools/build`
and are not checked in.

This file keeps the folder in git, so `//go:embed bin` always has something
to embed and plain `go build` keeps working without the servers.
