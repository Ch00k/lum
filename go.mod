module github.com/ay/lum

go 1.25.4

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/yuin/goldmark v1.7.13
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc
)

require (
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

replace github.com/yuin/goldmark-highlighting/v2 => github.com/Ch00k/goldmark-highlighting/v2 v2.0.0-20251113164446-2f96e480cf40
