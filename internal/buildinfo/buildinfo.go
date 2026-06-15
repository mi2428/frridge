// Package buildinfo carries build-time metadata injected by Makefile ldflags.
package buildinfo

// Version is replaced at build time. The default keeps local `go test` and
// ad-hoc `go run` invocations readable.
var Version = "dev"
