module github.com/Wirezat/fileshare

go 1.25.0

require (
	github.com/Wirezat/GoLog v0.0.0-20260403110615-1539104ddbb7
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/yuin/goldmark v1.8.6
	golang.org/x/crypto v0.49.0
	golang.org/x/image v0.44.0
)

require golang.org/x/sys v0.42.0 // indirect

replace github.com/Wirezat/GoLog => ../GoLog
