module github.com/AnxiangLemon/lw-plugins/ws/go/group-manager

go 1.26.0

require (
	github.com/AnxiangLemon/lw-plugins/ws/go v0.0.0
	modernc.org/sqlite v1.62.0
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// 源码仓库内直接使用相邻 SDK。发布 SDK tag 后可替换成明确版本。
replace github.com/AnxiangLemon/lw-plugins/ws/go => ..
