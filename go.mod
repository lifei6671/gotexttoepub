module github.com/lifei6671/gotexttoepub

go 1.26

// Pin the Windows nested-TOC fix pending https://github.com/go-shiori/go-epub/pull/62.
replace github.com/go-shiori/go-epub => github.com/ystyle/go-epub v0.0.0-20250425133851-dba4e6a949ec

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/go-shiori/go-epub v1.2.1
	github.com/urfave/cli/v2 v2.27.7
	golang.org/x/image v0.45.0
	golang.org/x/text v0.41.0
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/gofrs/uuid/v5 v5.5.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/vincent-petithory/dataurl v1.0.0 // indirect
	github.com/xrash/smetrics v0.0.0-20250705151800-55b8f293f342 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
