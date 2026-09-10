package assets

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFiles embed.FS

func GetStaticFS() fs.FS {
	fsys, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}

	return fs.FS(fsys)
}
