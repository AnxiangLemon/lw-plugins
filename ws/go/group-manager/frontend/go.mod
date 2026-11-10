// 隔离 npm 依赖附带的 Go 示例，避免父模块 go test ./... 扫描 node_modules。
module github.com/AnxiangLemon/lw-plugins/ws/go/group-manager/frontend

go 1.26.0
