module github.com/toaster/fritz_sync

go 1.14

//replace github.com/huin/goupnp => github.com/toaster/goupnp v1.0.3-wip
replace github.com/huin/goupnp => ../goupnp

replace github.com/toaster/digest => ../digest

require (
	github.com/emersion/go-vcard v0.0.0-20190105225839-8856043f13c5
	github.com/huin/goupnp v0.0.0-00010101000000-000000000000
	github.com/studio-b12/gowebdav v0.0.0-20190103184047-38f79aeaf1ac
	github.com/toaster/digest v0.0.0-20190401193356-8bd17d7ddb36
	github.com/urfave/cli v1.20.0
)
